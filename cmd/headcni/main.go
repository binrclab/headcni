package main

import (
	"runtime"

	"github.com/binrclab/headcni/pkg/cni"
	"github.com/binrclab/headcni/pkg/ipam"
	"github.com/binrclab/headcni/pkg/networking"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"github.com/containernetworking/cni/pkg/version"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
)

// IPAMRange 表示 IPAM 范围配置
type IPAMRange struct {
	Subnet string `json:"subnet,omitempty"`
}

// IPAMConfig 表示 IPAM 配置
type IPAMConfig struct {
	Type               string        `json:"type"`
	Ranges             [][]IPAMRange `json:"ranges,omitempty"`
	DataDir            string        `json:"dataDir,omitempty"`
	AllocationStrategy string        `json:"allocation_strategy,omitempty"`
}

// CNI 配置结构
type NetConf struct {
	types.NetConf

	// IPAM 配置
	IPAM IPAMConfig `json:"ipam"`

	// 网络配置
	PodCIDR     string `json:"pod_cidr"`
	ServiceCIDR string `json:"service_cidr,omitempty"`

	// MagicDNS 配置
	MagicDNS struct {
		Enable        bool     `json:"enable"`
		BaseDomain    string   `json:"base_domain,omitempty"`
		Nameservers   []string `json:"nameservers,omitempty"`
		SearchDomains []string `json:"search_domains,omitempty"`
	} `json:"magic_dns,omitempty"`

	// 高级选项
	MTU                 int    `json:"mtu,omitempty"`
	EnableIPv6          bool   `json:"enable_ipv6,omitempty"`
	EnableNetworkPolicy bool   `json:"enable_network_policy,omitempty"`
	TailscaleNic        string `json:"tailscale_nic,omitempty"`

	// 插件链支持
	Plugins []*NetConf `json:"plugins,omitempty"`
}

type CNIPlugin struct {
	config      *NetConf
	ipamManager *ipam.IPAMManager
	networkMgr  *networking.NetworkManager
	cniClient   *cni.Client
	hostLocal   *HostLocalIPAM
}

func main() {
	// 设置运行时
	runtime.GOMAXPROCS(1)

	skel.PluginMainFuncs(skel.CNIFuncs{
		Add:   cmdAdd,
		Check: cmdCheck,
		Del:   cmdDel,
	}, version.All, bv.BuildString("headcni"))
}
