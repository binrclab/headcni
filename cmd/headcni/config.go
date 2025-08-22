package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/binrclab/headcni/pkg/cni"
	"github.com/binrclab/headcni/pkg/ipam"
	"github.com/binrclab/headcni/pkg/networking"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
)

// loadConfig 加载CNI配置
func loadConfig(stdinData []byte) (*CNIPlugin, error) {
	conf := &NetConf{}
	if err := json.Unmarshal(stdinData, conf); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %v", err)
	}

	// 检查是否是插件链配置
	if len(conf.Plugins) > 0 {
		// 找到 headcni 插件
		for _, plugin := range conf.Plugins {
			if plugin.Type == "headcni" {
				conf = plugin
				break
			}
		}
	}

	// 设置默认值
	if conf.MTU == 0 {
		conf.MTU = 1280 // 考虑 Tailscale 封装开销
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

	// 从 IPAM 配置中获取 Pod CIDR
	podCIDRStr := ""
	if len(conf.IPAM.Ranges) > 0 && len(conf.IPAM.Ranges[0]) > 0 {
		podCIDRStr = conf.IPAM.Ranges[0][0].Subnet
	}

	// 如果没有在 IPAM ranges 中找到，尝试从 PodCIDR 字段获取
	if podCIDRStr == "" && conf.PodCIDR != "" {
		podCIDRStr = conf.PodCIDR
	}

	if podCIDRStr == "" {
		return nil, fmt.Errorf("no pod CIDR specified in IPAM ranges or pod_cidr field")
	}

	_, podCIDR, err := net.ParseCIDR(podCIDRStr)
	if err != nil {
		return nil, fmt.Errorf("invalid pod CIDR %s: %v", podCIDRStr, err)
	}

	// 创建 IPAM Manager（仅用于 headcni-ipam 类型）
	var ipamManager *ipam.IPAMManager
	if conf.IPAM.Type == "headcni-ipam" {
		ipamManager, err = ipam.NewIPAMManager(nodeName, podCIDR)
		if err != nil {
			return nil, fmt.Errorf("failed to create IPAM manager: %v", err)
		}
	}

	// 创建 host-local IPAM（用于 host-local 类型）
	var hostLocal *HostLocalIPAM
	if conf.IPAM.Type == "host-local" {
		// 从 ranges 配置中获取子网
		var subnetStr string
		if len(conf.IPAM.Ranges) > 0 && len(conf.IPAM.Ranges[0]) > 0 {
			subnetStr = conf.IPAM.Ranges[0][0].Subnet
		}

		if subnetStr == "" {
			return nil, fmt.Errorf("no subnet specified in IPAM ranges configuration")
		}

		hostLocal, err = NewHostLocalIPAM(subnetStr, conf.IPAM.AllocationStrategy, conf.IPAM.DataDir)
		if err != nil {
			return nil, fmt.Errorf("failed to create host-local IPAM: %v", err)
		}
	}

	// 创建网络管理器
	networkMgr, err := networking.NewNetworkManager(&networking.Config{
		MTU: conf.MTU,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create network manager: %v", err)
	}

	// 创建 CNI 客户端（用于与 Daemon 通信）
	var cniClient *cni.Client
	// Daemon 模式需要与 Daemon 通信，无论使用哪种 IPAM
	daemonSocket := os.Getenv("HEADCNI_DAEMON_SOCKET")
	if daemonSocket == "" {
		daemonSocket = "/var/run/headcni/daemon.sock"
	}
	cniClient = cni.NewClient(daemonSocket)

	return &CNIPlugin{
		config:      conf,
		ipamManager: ipamManager,
		networkMgr:  networkMgr,
		cniClient:   cniClient,
		hostLocal:   hostLocal,
	}, nil
}

// getPodCIDRFromIPAM 从IPAM配置中获取Pod CIDR
func (p *CNIPlugin) getPodCIDRFromIPAM() string {
	// 从 IPAM 配置的 ranges 中获取子网
	if len(p.config.IPAM.Ranges) > 0 && len(p.config.IPAM.Ranges[0]) > 0 {
		return p.config.IPAM.Ranges[0][0].Subnet
	}

	// 如果没有在 ranges 中找到，尝试从 PodCIDR 字段获取
	if p.config.PodCIDR != "" {
		return p.config.PodCIDR
	}

	return ""
}

// getSubnetMask 获取子网掩码
func (p *CNIPlugin) getSubnetMask() net.IPMask {
	// 获取 Pod 子网掩码
	podCIDRStr := p.getPodCIDRFromIPAM()
	if podCIDRStr == "" {
		klog.Warningf("No Pod CIDR configured, cannot determine subnet mask")
		return nil
	}
	_, podCIDR, err := net.ParseCIDR(podCIDRStr)
	if err != nil {
		klog.Warningf("Invalid Pod CIDR %s: %v", podCIDRStr, err)
		return nil
	}
	return podCIDR.Mask
}

// getTailscaleGateway 获取Tailscale网关
func (p *CNIPlugin) getTailscaleGateway() net.IP {
	// 获取本地 Tailscale IP 作为网关
	// 优先从 Tailscale daemon 获取
	tailscaleIP := p.getTailscaleIP()
	if tailscaleIP != nil {
		return tailscaleIP
	}

	// 备用方案：使用本地 Pod CIDR 的第一个 IP（.1）
	podCIDRStr := p.getPodCIDRFromIPAM()
	if podCIDRStr == "" {
		klog.Warningf("No Pod CIDR configured, cannot determine gateway")
		return nil
	}
	_, podCIDR, err := net.ParseCIDR(podCIDRStr)
	if err != nil {
		klog.Warningf("Invalid Pod CIDR %s: %v", podCIDRStr, err)
		return nil
	}

	gateway := make(net.IP, len(podCIDR.IP))
	copy(gateway, podCIDR.IP)
	gateway[len(gateway)-1] = 1

	return gateway
}

// getTailscaleIP 获取Tailscale IP地址
func (p *CNIPlugin) getTailscaleIP() net.IP {
	// 从 Tailscale daemon 获取 IP 地址
	// 这里应该实现从 Tailscale 获取 IP 的逻辑
	// 暂时返回 nil，表示需要实现
	return nil
}

// PodInfo Pod信息结构
type PodInfo struct {
	namespace   string
	podName     string
	containerID string
	podCIDR     string // 添加 PodCIDR 字段
}

// parsePodInfo 解析Pod信息
func parsePodInfo(cniArgs string) (*PodInfo, error) {
	// 解析 CNI_ARGS 格式：IgnoreUnknown=1;K8S_POD_NAMESPACE=default;K8S_POD_NAME=pod-name;...
	args := make(map[string]string)

	for _, arg := range strings.Split(cniArgs, ";") {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) == 2 {
			args[parts[0]] = parts[1]
		}
	}

	return &PodInfo{
		namespace:   args["K8S_POD_NAMESPACE"],
		podName:     args["K8S_POD_NAME"],
		containerID: args["K8S_POD_INFRA_CONTAINER_ID"],
	}, nil
}

// notifyDaemonPodReady 通知 Daemon Pod 网络已配置完成
func (p *CNIPlugin) notifyDaemonPodReady(podInfo *PodInfo, allocation *ipam.IPAllocation) error {
	// 通过 CNI 客户端通知 Daemon
	req := &cni.CNIRequest{
		Type:        "pod_ready",
		Namespace:   podInfo.namespace,
		PodName:     podInfo.podName,
		ContainerID: allocation.ContainerID,
		PodIP:       allocation.IP.String(),
		LocalPool:   p.getPodCIDRFromIPAM(),
	}

	resp, err := p.cniClient.SendRequest(req)
	if err != nil {
		return fmt.Errorf("failed to notify daemon: %v", err)
	}

	if !resp.Success {
		return fmt.Errorf("daemon notification failed: %s", resp.Error)
	}

	return nil
}

// cleanupHostRoute 清理宿主机路由
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

// checkPodNetwork 检查Pod网络配置
func (p *CNIPlugin) checkPodNetwork(netns string, podInfo *PodInfo) error {
	klog.V(4).Infof("Checking pod network for %s/%s", podInfo.namespace, podInfo.podName)

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

		klog.V(4).Infof("Found %d IPv4 addresses on eth0", len(addrs))
		for _, addr := range addrs {
			klog.V(4).Infof("  - %s", addr.IPNet.String())
		}

		// 如果启用了 Tailscale，跳过默认路由检查
		// 因为 Tailscale 会自己处理路由
		if p.config.TailscaleNic != "" {
			klog.V(4).Infof("Tailscale mode enabled, skipping default route check")
			return nil
		}

		// 检查默认路由（仅在非 Tailscale 模式下）
		routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
		if err != nil {
			return fmt.Errorf("failed to get routes: %v", err)
		}

		hasDefaultRoute := false
		for _, route := range routes {
			if route.Dst == nil || route.Dst.String() == "0.0.0.0/0" {
				hasDefaultRoute = true
				klog.V(4).Infof("Found default route: %s via %s", route.Dst, route.Gw)
				break
			}
		}

		if !hasDefaultRoute {
			return fmt.Errorf("no default route configured")
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("pod network check failed: %v", err)
	}

	klog.V(4).Infof("Pod network check passed for %s/%s", podInfo.namespace, podInfo.podName)
	return nil
}
