package main

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/version"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
)

// IPAMRange 表示 IPAM 范围配置
type IPAMRange struct {
	Subnet  string `json:"subnet,omitempty"`
	Gateway string `json:"gateway,omitempty"`
}

// IPAMConfig 定义IPAM配置
type IPAMConfig struct {
	Type               string        `json:"type"`
	Ranges             [][]IPAMRange `json:"ranges,omitempty"`
	DataDir            string        `json:"dataDir,omitempty"`
	AllocationStrategy string        `json:"allocation_strategy,omitempty"`
}

// NetConf 定义网络配置
type NetConf struct {
	types.NetConf
	IPAM *IPAMConfig `json:"ipam"`
}

func main() {
	skel.PluginMainFuncs(skel.CNIFuncs{
		Add:   cmdAdd,
		Check: cmdCheck,
		Del:   cmdDel,
	}, version.All, bv.BuildString("headcni-ipam"))
}

// PodInfo 表示 Pod 信息
type PodInfo struct {
	Namespace string
	Name      string
	UID       string
	// 添加与主插件一致的字段
	containerID string
}

// parsePodInfo 解析 Pod 信息
func parsePodInfo(cniArgs string) (*PodInfo, error) {
	// 解析 CNI_ARGS 格式：IgnoreUnknown=1;K8S_POD_NAMESPACE=default;K8S_POD_NAME=pod-name;...
	args := make(map[string]string)

	for _, arg := range strings.Split(cniArgs, ";") {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) == 2 {
			args[parts[0]] = parts[1]
		}
	}

	namespace := args["K8S_POD_NAMESPACE"]
	name := args["K8S_POD_NAME"]
	uid := args["K8S_POD_UID"]
	containerID := args["K8S_POD_INFRA_CONTAINER_ID"]

	if namespace == "" || name == "" {
		return nil, fmt.Errorf("missing required pod info in CNI args")
	}

	return &PodInfo{
		Namespace:   namespace,
		Name:        name,
		UID:         uid,
		containerID: containerID,
	}, nil
}

// getSubnetConfig 从 IPAM 配置中获取子网和网关信息
func getSubnetConfig(ipamConfig *IPAMConfig) (*net.IPNet, net.IP, error) {
	var subnetStr string
	var gatewayStr string

	// 从 ranges 配置中获取
	if len(ipamConfig.Ranges) > 0 && len(ipamConfig.Ranges[0]) > 0 {
		rangeConfig := ipamConfig.Ranges[0][0]
		subnetStr = rangeConfig.Subnet
		gatewayStr = rangeConfig.Gateway
	}

	if subnetStr == "" {
		return nil, nil, fmt.Errorf("no subnet specified in IPAM configuration")
	}

	// 解析子网
	_, subnet, err := net.ParseCIDR(subnetStr)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid subnet %s: %v", subnetStr, err)
	}

	// 解析网关
	var gateway net.IP
	if gatewayStr != "" {
		gateway = net.ParseIP(gatewayStr)
		if gateway == nil {
			return nil, nil, fmt.Errorf("invalid gateway %s", gatewayStr)
		}
	} else {
		// 如果没有指定网关，使用子网的第一个 IP
		gateway = make(net.IP, len(subnet.IP))
		copy(gateway, subnet.IP)
		gateway[len(gateway)-1] = 1
	}

	return subnet, gateway, nil
}

func loadNetConf(bytes []byte) (*NetConf, error) {
	conf := &NetConf{}
	if err := json.Unmarshal(bytes, conf); err != nil {
		return nil, fmt.Errorf("failed to unmarshal netconf: %v", err)
	}

	if conf.IPAM == nil {
		return nil, fmt.Errorf("IPAM configuration is required")
	}

	return conf, nil
}
