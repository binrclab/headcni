package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"

	"github.com/binrclab/headcni/pkg/ipam"
	"github.com/binrclab/headcni/pkg/networking"
	bv "github.com/containernetworking/plugins/pkg/utils/buildversion"
)

// CNI 配置结构
type NetConf struct {
	types.NetConf

	// Tailscale 配置
	HeadscaleURL    string `json:"headscale_url"`
	TailscaleSocket string `json:"tailscale_socket,omitempty"`

	// IPAM 配置
	IPAM struct {
		Type               string `json:"type"`
		AllocationStrategy string `json:"allocation_strategy,omitempty"`
	} `json:"ipam"`

	// 网络配置
	PodCIDR     string `json:"pod_cidr"`
	ServiceCIDR string `json:"service_cidr,omitempty"`

	// DNS 配置
	DNS types.DNS `json:"dns,omitempty"`

	// 高级选项
	MTU                 int  `json:"mtu,omitempty"`
	EnableIPv6          bool `json:"enable_ipv6,omitempty"`
	EnableNetworkPolicy bool `json:"enable_network_policy,omitempty"`
}

type CNIPlugin struct {
	config      *NetConf
	ipamManager *ipam.IPAMManager
	networkMgr  *networking.NetworkManager
}

func main() {
	// 设置运行时
	runtime.GOMAXPROCS(1)

	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, bv.BuildString("headcni"))
}

func cmdAdd(args *skel.CmdArgs) error {
	plugin, err := loadConfig(args.StdinData, args.Args)
	if err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	klog.Infof("CNI ADD called for container %s", args.ContainerID)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 解析容器和 Pod 信息
	podInfo, err := parsePodInfo(args.Args)
	if err != nil {
		return fmt.Errorf("failed to parse pod info: %v", err)
	}

	// 分配 IP 地址
	allocation, err := plugin.ipamManager.AllocateIP(
		ctx,
		podInfo.Namespace,
		podInfo.Name,
		args.ContainerID,
	)
	if err != nil {
		return fmt.Errorf("IPAM allocation failed: %v", err)
	}

	// 配置 Pod 网络
	result, err := plugin.setupPodNetwork(args, allocation)
	if err != nil {
		// 出错时清理分配的 IP
		plugin.ipamManager.ReleaseIP(ctx, podInfo.Namespace, podInfo.Name)
		return fmt.Errorf("failed to setup pod network: %v", err)
	}

	klog.Infof("Successfully configured network for pod %s/%s with IP %s",
		podInfo.Namespace, podInfo.Name, allocation.IP.String())

	return types.PrintResult(result, plugin.config.CNIVersion)
}

func (p *CNIPlugin) setupPodNetwork(args *skel.CmdArgs, allocation *ipam.IPAllocation) (*current.Result, error) {
	// 1. 创建 veth pair
	hostVethName := vethName(args.ContainerID)

	err := p.networkMgr.CreateVethPair(args.Netns, "eth0", hostVethName)
	if err != nil {
		return nil, fmt.Errorf("failed to create veth pair: %v", err)
	}

	// 2. 配置 Pod 网络命名空间
	var result *current.Result

	err = ns.WithNetNSPath(args.Netns, func(hostNS ns.NetNS) error {
		// 配置 Pod 内的网络
		err := p.setupPodNetworkNS(allocation)
		if err != nil {
			return err
		}

		// 构造返回结果
		result = &current.Result{
			CNIVersion: p.config.CNIVersion,
			IPs: []*current.IPConfig{
				{
					Address: net.IPNet{
						IP:   allocation.IP,
						Mask: p.getSubnetMask(),
					},
					Gateway: p.getTailscaleGateway(),
				},
			},
			Routes: []*types.Route{
				{
					Dst: net.IPNet{
						IP:   net.IPv4zero,
						Mask: net.CIDRMask(0, 32),
					},
					GW: p.getTailscaleGateway(),
				},
			},
		}

		// 可选：配置 DNS
		if p.config.DNS.Nameservers != nil {
			result.DNS = p.config.DNS
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// 3. 配置宿主机路由
	err = p.setupHostRouting(allocation.IP, hostVethName)
	if err != nil {
		return nil, fmt.Errorf("failed to setup host routing: %v", err)
	}

	return result, nil
}

func (p *CNIPlugin) setupPodNetworkNS(allocation *ipam.IPAllocation) error {
	// 获取 eth0 接口
	eth0, err := netlink.LinkByName("eth0")
	if err != nil {
		return fmt.Errorf("failed to find eth0: %v", err)
	}

	// 设置 MTU
	mtu := p.config.MTU
	if mtu == 0 {
		mtu = 1500 // 默认 MTU，考虑 Tailscale 封装开销可能需要调整
	}

	if err := netlink.LinkSetMTU(eth0, mtu); err != nil {
		return fmt.Errorf("failed to set MTU: %v", err)
	}

	// 配置 IP 地址
	subnetMask := p.getSubnetMask()
	addr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   allocation.IP,
			Mask: subnetMask,
		},
	}

	if err := netlink.AddrAdd(eth0, addr); err != nil {
		return fmt.Errorf("failed to add IP address: %v", err)
	}

	// 启用接口
	if err := netlink.LinkSetUp(eth0); err != nil {
		return fmt.Errorf("failed to set eth0 up: %v", err)
	}

	// 配置路由
	err = p.setupPodRoutes(eth0)
	if err != nil {
		return fmt.Errorf("failed to setup pod routes: %v", err)
	}

	return nil
}

func (p *CNIPlugin) setupPodRoutes(eth0 netlink.Link) error {
	tailscaleGW := p.getTailscaleGateway()

	// 添加网关路由（确保网关可达）
	gwRoute := &netlink.Route{
		LinkIndex: eth0.Attrs().Index,
		Scope:     netlink.SCOPE_LINK,
		Dst: &net.IPNet{
			IP:   tailscaleGW,
			Mask: net.CIDRMask(32, 32),
		},
	}

	if err := netlink.RouteAdd(gwRoute); err != nil && !os.IsExist(err) {
		return fmt.Errorf("failed to add gateway route: %v", err)
	}

	// 添加默认路由
	defaultRoute := &netlink.Route{
		LinkIndex: eth0.Attrs().Index,
		Scope:     netlink.SCOPE_UNIVERSE,
		Dst: &net.IPNet{
			IP:   net.IPv4zero,
			Mask: net.CIDRMask(0, 32),
		},
		Gw: tailscaleGW,
	}

	if err := netlink.RouteAdd(defaultRoute); err != nil && !os.IsExist(err) {
		return fmt.Errorf("failed to add default route: %v", err)
	}

	// 可选：添加服务网段直连路由（优化 Service 访问）
	if p.config.ServiceCIDR != "" {
		if err := p.addServiceRoute(eth0, tailscaleGW); err != nil {
			klog.Warningf("Failed to add service route: %v", err)
			// 服务路由失败不应该阻塞 Pod 启动
		}
	}

	return nil
}

func (p *CNIPlugin) addServiceRoute(eth0 netlink.Link, gateway net.IP) error {
	_, serviceCIDR, err := net.ParseCIDR(p.config.ServiceCIDR)
	if err != nil {
		return err
	}

	serviceRoute := &netlink.Route{
		LinkIndex: eth0.Attrs().Index,
		Scope:     netlink.SCOPE_UNIVERSE,
		Dst:       serviceCIDR,
		Gw:        gateway,
	}

	return netlink.RouteAdd(serviceRoute)
}

func (p *CNIPlugin) setupHostRouting(podIP net.IP, hostVethName string) error {
	// 获取宿主机上的 veth 接口
	hostVeth, err := netlink.LinkByName(hostVethName)
	if err != nil {
		return fmt.Errorf("failed to find host veth %s: %v", hostVethName, err)
	}

	// 启用宿主机 veth 接口
	if err := netlink.LinkSetUp(hostVeth); err != nil {
		return fmt.Errorf("failed to set host veth up: %v", err)
	}

	// 添加路由：Pod IP -> host veth
	route := &netlink.Route{
		LinkIndex: hostVeth.Attrs().Index,
		Scope:     netlink.SCOPE_LINK,
		Dst: &net.IPNet{
			IP:   podIP,
			Mask: net.CIDRMask(32, 32), // /32 主机路由
		},
	}

	if err := netlink.RouteAdd(route); err != nil && !os.IsExist(err) {
		return fmt.Errorf("failed to add host route for pod IP: %v", err)
	}

	klog.V(4).Infof("Added host route: %s -> %s", podIP.String(), hostVethName)
	return nil
}

func cmdDel(args *skel.CmdArgs) error {
	plugin, err := loadConfig(args.StdinData, args.Args)
	if err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	klog.Infof("CNI DEL called for container %s", args.ContainerID)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 解析 Pod 信息
	podInfo, err := parsePodInfo(args.Args)
	if err != nil {
		klog.Warningf("Failed to parse pod info during deletion: %v", err)
		// 删除时解析失败不应阻塞清理过程
	} else {
		// 释放 IP 地址
		if err := plugin.ipamManager.ReleaseIP(ctx, podInfo.Namespace, podInfo.Name); err != nil {
			klog.Warningf("Failed to release IP: %v", err)
		}
	}

	// 清理网络配置（veth 接口会在容器删除时自动清理）
	hostVethName := vethName(args.ContainerID)
	if err := plugin.cleanupHostRoute(hostVethName); err != nil {
		klog.Warningf("Failed to cleanup host route: %v", err)
	}

	klog.Infof("Successfully cleaned up network for container %s", args.ContainerID)
	return nil
}

func (p *CNIPlugin) cleanupHostRoute(hostVethName string) error {
	// 尝试删除相关路由（如果接口还存在的话）
	hostVeth, err := netlink.LinkByName(hostVethName)
	if err != nil {
		// 接口可能已经被删除，这是正常的
		return nil
	}

	// 获取与该接口相关的路由并删除
	routes, err := netlink.RouteList(hostVeth, netlink.FAMILY_V4)
	if err != nil {
		return err
	}

	for _, route := range routes {
		if err := netlink.RouteDel(&route); err != nil {
			klog.V(4).Infof("Failed to delete route %v: %v", route, err)
		}
	}

	return nil
}

func cmdCheck(args *skel.CmdArgs) error {
	plugin, err := loadConfig(args.StdinData, args.Args)
	if err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	klog.V(4).Infof("CNI CHECK called for container %s", args.ContainerID)

	// 解析 Pod 信息
	podInfo, err := parsePodInfo(args.Args)
	if err != nil {
		return fmt.Errorf("failed to parse pod info: %v", err)
	}

	// 检查网络配置是否正确
	return plugin.checkPodNetwork(args.Netns, podInfo)
}

func (p *CNIPlugin) checkPodNetwork(netns string, podInfo *PodInfo) error {
	var checkErr error

	err := ns.WithNetNSPath(netns, func(hostNS ns.NetNS) error {
		// 检查接口是否存在
		eth0, err := netlink.LinkByName("eth0")
		if err != nil {
			return fmt.Errorf("eth0 interface not found: %v", err)
		}

		// 检查接口状态
		if eth0.Attrs().Flags&net.FlagUp == 0 {
			return fmt.Errorf("eth0 interface is down")
		}

		// 检查 IP 地址配置
		addrs, err := netlink.AddrList(eth0, netlink.FAMILY_V4)
		if err != nil {
			return fmt.Errorf("failed to get IP addresses: %v", err)
		}

		if len(addrs) == 0 {
			return fmt.Errorf("no IP address configured on eth0")
		}

		// 检查默认路由
		routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
		if err != nil {
			return fmt.Errorf("failed to get routes: %v", err)
		}

		hasDefaultRoute := false
		for _, route := range routes {
			if route.Dst == nil || route.Dst.String() == "0.0.0.0/0" {
				hasDefaultRoute = true
				break
			}
		}

		if !hasDefaultRoute {
			return fmt.Errorf("no default route configured")
		}

		return nil
	})

	if err != nil {
		checkErr = err
	}

	return checkErr
}

// 辅助函数和结构体

type PodInfo struct {
	Namespace string
	Name      string
	UID       string
}

func parsePodInfo(cniArgs string) (*PodInfo, error) {
	// 解析 CNI_ARGS 格式：IgnoreUnknown=1;K8S_POD_NAMESPACE=default;K8S_POD_NAME=test-pod;...
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

	if namespace == "" || name == "" {
		return nil, fmt.Errorf("missing required pod info in CNI args")
	}

	return &PodInfo{
		Namespace: namespace,
		Name:      name,
		UID:       uid,
	}, nil
}

func vethName(containerID string) string {
	// 生成 veth 接口名，限制在 15 字符内（Linux 接口名限制）
	if len(containerID) > 12 {
		containerID = containerID[:12]
	}
	return "veth" + containerID
}

func loadConfig(stdinData []byte, cniArgs string) (*CNIPlugin, error) {
	conf := &NetConf{}
	if err := json.Unmarshal(stdinData, conf); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %v", err)
	}

	// 设置默认值
	if conf.MTU == 0 {
		conf.MTU = 1420 // 考虑 Tailscale 封装开销
	}

	// 获取节点名
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, fmt.Errorf("failed to get hostname: %v", err)
		}
		nodeName = hostname
	}

	// 解析 Pod CIDR
	_, podCIDR, err := net.ParseCIDR(conf.PodCIDR)
	if err != nil {
		return nil, fmt.Errorf("invalid pod CIDR %s: %v", conf.PodCIDR, err)
	}

	// 创建 IPAM Manager
	ipamManager, err := ipam.NewIPAMManager(nodeName, podCIDR)
	if err != nil {
		return nil, fmt.Errorf("failed to create IPAM manager: %v", err)
	}

	// 创建网络管理器
	networkMgr, err := networking.NewNetworkManager(&networking.Config{
		TailscaleSocket: conf.TailscaleSocket,
		MTU:             conf.MTU,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create network manager: %v", err)
	}

	return &CNIPlugin{
		config:      conf,
		ipamManager: ipamManager,
		networkMgr:  networkMgr,
	}, nil
}

func (p *CNIPlugin) getSubnetMask() net.IPMask {
	// 获取 Pod 子网掩码（通常是 /24）
	_, podCIDR, _ := net.ParseCIDR(p.config.PodCIDR)
	return podCIDR.Mask
}

func (p *CNIPlugin) getTailscaleGateway() net.IP {
	// 获取本地 Tailscale IP 作为网关
	// 这里应该从 Tailscale daemon 获取，简化示例使用固定逻辑

	// 方法1：从 tailscale status 获取
	if ip, err := p.networkMgr.GetTailscaleIP(); err == nil {
		return ip
	}

	// 方法2：使用本地 Pod CIDR 的第一个 IP（.1）
	_, podCIDR, _ := net.ParseCIDR(p.config.PodCIDR)
	gateway := make(net.IP, len(podCIDR.IP))
	copy(gateway, podCIDR.IP)
	gateway[len(gateway)-1] = 1

	return gateway
}
