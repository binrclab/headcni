package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"

	"github.com/binrclab/headcni/pkg/ipam"
)

// IPAMConfig 定义IPAM配置
type IPAMConfig struct {
	Type               string         `json:"type"`
	Subnet             string         `json:"subnet"`
	RangeStart         string         `json:"rangeStart,omitempty"`
	RangeEnd           string         `json:"rangeEnd,omitempty"`
	Gateway            string         `json:"gateway,omitempty"`
	AllocationStrategy string         `json:"allocation_strategy,omitempty"`
	Routes             []*types.Route `json:"routes,omitempty"`
}

// NetConf 定义网络配置
type NetConf struct {
	types.NetConf
	IPAM *IPAMConfig `json:"ipam"`
}

func main() {
	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, "headcni-ipam")
}

func cmdAdd(args *skel.CmdArgs) error {
	conf, err := loadNetConf(args.StdinData)
	if err != nil {
		return fmt.Errorf("failed to load netconf: %v", err)
	}

	// 解析子网
	_, subnet, err := net.ParseCIDR(conf.IPAM.Subnet)
	if err != nil {
		return fmt.Errorf("invalid subnet %s: %v", conf.IPAM.Subnet, err)
	}

	// 创建IPAM管理器
	manager, err := ipam.NewIPAMManager("headcni-node", subnet)
	if err != nil {
		return fmt.Errorf("failed to create IPAM manager: %v", err)
	}

	// 分配IP
	allocation, err := manager.AllocateIP(context.Background(), "default", args.ContainerID, args.ContainerID)
	if err != nil {
		return fmt.Errorf("failed to allocate IP: %v", err)
	}

	// 构造结果
	result := &types100.Result{
		CNIVersion: types100.ImplementedSpecVersion,
		IPs: []*types100.IPConfig{
			{
				Address: net.IPNet{
					IP:   allocation.IP,
					Mask: subnet.Mask,
				},
				Gateway: net.ParseIP(conf.IPAM.Gateway),
			},
		},
		Routes: conf.IPAM.Routes,
	}

	return types.PrintResult(result, conf.CNIVersion)
}

func cmdDel(args *skel.CmdArgs) error {
	conf, err := loadNetConf(args.StdinData)
	if err != nil {
		return fmt.Errorf("failed to load netconf: %v", err)
	}

	// 解析子网
	_, subnet, err := net.ParseCIDR(conf.IPAM.Subnet)
	if err != nil {
		return fmt.Errorf("invalid subnet %s: %v", conf.IPAM.Subnet, err)
	}

	// 创建IPAM管理器
	manager, err := ipam.NewIPAMManager("headcni-node", subnet)
	if err != nil {
		return fmt.Errorf("failed to create IPAM manager: %v", err)
	}

	// 释放IP
	err = manager.ReleaseIP(context.Background(), "default", args.ContainerID)
	if err != nil {
		return fmt.Errorf("failed to release IP: %v", err)
	}

	return nil
}

func cmdCheck(args *skel.CmdArgs) error {
	// 检查IP是否仍然分配
	conf, err := loadNetConf(args.StdinData)
	if err != nil {
		return fmt.Errorf("failed to load netconf: %v", err)
	}

	// 解析子网
	_, subnet, err := net.ParseCIDR(conf.IPAM.Subnet)
	if err != nil {
		return fmt.Errorf("invalid subnet %s: %v", conf.IPAM.Subnet, err)
	}

	// 创建IPAM管理器
	manager, err := ipam.NewIPAMManager("headcni-node", subnet)
	if err != nil {
		return fmt.Errorf("failed to create IPAM manager: %v", err)
	}

	// 检查IP分配状态
	allocation, err := manager.AllocateIP(context.Background(), "default", args.ContainerID, args.ContainerID)
	if err != nil {
		return fmt.Errorf("IP allocation check failed: %v", err)
	}

	// 验证IP是否在子网范围内
	if !subnet.Contains(allocation.IP) {
		return fmt.Errorf("allocated IP %s is not in subnet %s", allocation.IP.String(), subnet.String())
	}

	return nil
}

func loadNetConf(bytes []byte) (*NetConf, error) {
	conf := &NetConf{}
	if err := json.Unmarshal(bytes, conf); err != nil {
		return nil, fmt.Errorf("failed to unmarshal netconf: %v", err)
	}

	if conf.IPAM == nil {
		return nil, fmt.Errorf("IPAM configuration is required")
	}

	if conf.IPAM.Type == "" {
		conf.IPAM.Type = "headcni-ipam"
	}

	return conf, nil
}
