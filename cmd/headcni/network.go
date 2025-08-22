package main

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"

	"github.com/binrclab/headcni/pkg/ipam"
)

// setupPodNetwork 配置Pod网络
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

		// 配置 DNS
		if p.config.MagicDNS.Enable {
			result.DNS = p.buildDNSConfig()
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

	// 4. 如果启用了 Tailscale NIC，配置 Tailscale 网络
	if p.config.TailscaleNic != "" {
		if err := p.setupTailscaleNetwork(allocation); err != nil {
			klog.Warningf("Failed to setup Tailscale network: %v", err)
		}
	}

	return result, nil
}

// buildDNSConfig 构建DNS配置
func (p *CNIPlugin) buildDNSConfig() types.DNS {
	// 重新排列 nameservers 优先级
	nameservers := p.reorderNameservers(p.config.MagicDNS.Nameservers)

	return types.DNS{
		Nameservers: nameservers,
		Search:      p.config.MagicDNS.SearchDomains,
		Domain:      p.config.MagicDNS.BaseDomain,
	}
}

// reorderNameservers 重新排列 nameservers 优先级
func (p *CNIPlugin) reorderNameservers(nameservers []string) []string {
	if len(nameservers) == 0 {
		return nameservers
	}

	// 分类 nameservers
	var clusterDNS, tailscaleDNS, externalDNS []string

	for _, ns := range nameservers {
		ip := net.ParseIP(ns)
		if ip == nil {
			// 如果不是有效 IP，当作外部 DNS
			externalDNS = append(externalDNS, ns)
			continue
		}

		// 判断 DNS 类型
		switch {
		case p.isClusterDNS(ip):
			clusterDNS = append(clusterDNS, ns)
		case p.isTailscaleDNS(ip):
			tailscaleDNS = append(tailscaleDNS, ns)
		default:
			externalDNS = append(externalDNS, ns)
		}
	}

	// 按优先级重新排列：集群 DNS -> Tailscale DNS -> 外部 DNS
	result := make([]string, 0, len(nameservers))
	result = append(result, clusterDNS...)
	result = append(result, tailscaleDNS...)
	result = append(result, externalDNS...)

	return result
}

// isClusterDNS 判断是否为集群内 DNS
func (p *CNIPlugin) isClusterDNS(ip net.IP) bool {
	// 检查是否在服务 CIDR 范围内
	if p.config.ServiceCIDR != "" {
		_, serviceCIDR, err := net.ParseCIDR(p.config.ServiceCIDR)
		if err == nil && serviceCIDR.Contains(ip) {
			return true
		}
	}

	// 检查常见的集群 DNS IP
	clusterDNSIPs := []string{
		"10.43.0.10", // 常见的 CoreDNS IP
		"10.96.0.10", // 另一个常见的 CoreDNS IP
		"10.0.0.10",  // 一些集群使用的 IP
	}

	for _, dnsIP := range clusterDNSIPs {
		if ip.String() == dnsIP {
			return true
		}
	}

	return false
}

// isTailscaleDNS 判断是否为 Tailscale DNS
func (p *CNIPlugin) isTailscaleDNS(ip net.IP) bool {
	// Tailscale 使用 100.64.0.0/10 地址空间
	tailscaleCIDR := &net.IPNet{
		IP:   net.ParseIP("100.64.0.0"),
		Mask: net.CIDRMask(10, 32),
	}

	return tailscaleCIDR.Contains(ip)
}

// setupPodNetworkNS 配置Pod网络命名空间
func (p *CNIPlugin) setupPodNetworkNS(allocation *ipam.IPAllocation) error {
	// 获取 eth0 接口
	eth0, err := netlink.LinkByName("eth0")
	if err != nil {
		return fmt.Errorf("failed to find eth0: %v", err)
	}

	// 设置 MTU
	mtu := p.config.MTU
	if mtu == 0 {
		mtu = 1280 // 默认 MTU，考虑 Tailscale 封装开销
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

	// 如果启用了 IPv6，配置 IPv6 地址
	if p.config.EnableIPv6 {
		if err := p.setupIPv6Address(eth0, allocation); err != nil {
			klog.Warningf("Failed to setup IPv6 address: %v", err)
		}
	}

	return nil
}

// setupPodRoutes 配置Pod路由
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

	// 添加服务网段直连路由（优化 Service 访问）
	if p.config.ServiceCIDR != "" {
		if err := p.addServiceRoute(eth0, tailscaleGW); err != nil {
			klog.Warningf("Failed to add service route: %v", err)
		}
	}

	// 如果启用了 IPv6，添加 IPv6 路由
	if p.config.EnableIPv6 {
		if err := p.setupIPv6Routes(eth0); err != nil {
			klog.Warningf("Failed to setup IPv6 routes: %v", err)
		}
	}

	return nil
}

// setupHostRouting 配置宿主机路由
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

// setupTailscaleNetwork 配置Tailscale网络
func (p *CNIPlugin) setupTailscaleNetwork(allocation *ipam.IPAllocation) error {
	klog.Infof("Setting up Tailscale network for pod %s/%s", allocation.PodNamespace, allocation.PodName)

	// 自动检测 Tailscale NIC
	tailscaleNic := p.detectTailscaleNic()
	if tailscaleNic == "" {
		return fmt.Errorf("no Tailscale NIC auto-detected")
	}

	// 检查 Tailscale NIC 是否存在
	tailscaleLink, err := netlink.LinkByName(tailscaleNic)
	if err != nil {
		return fmt.Errorf("Tailscale NIC %s not found: %v", tailscaleNic, err)
	}

	klog.Infof("Using Tailscale NIC: %s (index: %d)", tailscaleNic, tailscaleLink.Attrs().Index)

	// 在宿主机上添加到 Pod IP 的路由，通过 Tailscale NIC
	// 这样 Tailscale 就可以处理到 Pod 的路由
	hostRoute := &netlink.Route{
		LinkIndex: tailscaleLink.Attrs().Index,
		Scope:     netlink.SCOPE_LINK,
		Dst: &net.IPNet{
			IP:   allocation.IP,
			Mask: net.CIDRMask(32, 32), // /32 主机路由
		},
	}

	if err := netlink.RouteAdd(hostRoute); err != nil && !os.IsExist(err) {
		return fmt.Errorf("failed to add host route via Tailscale: %v", err)
	}

	klog.Infof("Added host route via Tailscale: %s -> %s", allocation.IP.String(), tailscaleNic)

	// 让 Tailscale 自己处理路由，不需要在容器内配置默认路由
	klog.Infof("Tailscale will handle routing automatically for pod %s/%s",
		allocation.PodNamespace, allocation.PodName)

	return nil
}

// detectTailscaleNic 自动检测 Tailscale NIC
func (p *CNIPlugin) detectTailscaleNic() string {
	if p.config.TailscaleNic != "" {
		return p.config.TailscaleNic
	}

	// 常见的 Tailscale 接口名模式
	tailscalePatterns := []string{
		"tailscale0",
		"ts0",
		"headcni*", // 支持通配符模式
	}

	links, err := netlink.LinkList()
	if err != nil {
		klog.Warningf("Failed to list network interfaces: %v", err)
		return ""
	}

	for _, link := range links {
		linkName := link.Attrs().Name

		// 检查是否匹配任何模式
		for _, pattern := range tailscalePatterns {
			if pattern == linkName || (strings.Contains(pattern, "*") &&
				strings.HasPrefix(linkName, strings.TrimSuffix(pattern, "*"))) {
				klog.Infof("Auto-detected Tailscale NIC: %s", linkName)
				return linkName
			}
		}
	}

	klog.Warningf("No Tailscale NIC auto-detected")
	return ""
}

// setupIPv6Routes 配置IPv6路由
func (p *CNIPlugin) setupIPv6Routes(eth0 netlink.Link) error {
	// 添加 IPv6 默认路由
	ipv6DefaultRoute := &netlink.Route{
		LinkIndex: eth0.Attrs().Index,
		Scope:     netlink.SCOPE_UNIVERSE,
		Dst: &net.IPNet{
			IP:   net.IPv6zero,
			Mask: net.CIDRMask(0, 128),
		},
	}

	return netlink.RouteAdd(ipv6DefaultRoute)
}

// addServiceRoute 添加服务路由
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

// setupIPv6Address 配置IPv6地址
func (p *CNIPlugin) setupIPv6Address(eth0 netlink.Link, allocation *ipam.IPAllocation) error {
	// 生成 IPv6 地址（基于 Pod 的 IPv4 地址）
	ipv6Addr := p.generateIPv6Address(allocation.IP)

	addr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   ipv6Addr,
			Mask: net.CIDRMask(64, 128), // /64 子网
		},
	}

	if err := netlink.AddrAdd(eth0, addr); err != nil {
		return fmt.Errorf("failed to add IPv6 address: %v", err)
	}

	return nil
}

// generateIPv6Address 生成IPv6地址
func (p *CNIPlugin) generateIPv6Address(ipv4 net.IP) net.IP {
	// 生成基于 IPv4 的 IPv6 地址
	// 使用 fd00::/8 私有地址空间
	ipv6 := net.ParseIP("fd00::")
	ipv6[8] = ipv4[0]  // 使用 IPv4 的第一个字节
	ipv6[9] = ipv4[1]  // 使用 IPv4 的第二个字节
	ipv6[10] = ipv4[2] // 使用 IPv4 的第三个字节
	ipv6[11] = ipv4[3] // 使用 IPv4 的第四个字节

	return ipv6
}

// vethName 生成veth接口名
func vethName(containerID string) string {
	// 生成 veth 接口名，限制在 15 字符内（Linux 接口名限制）
	if len(containerID) > 12 {
		containerID = containerID[:12]
	}
	return "veth" + containerID
}

// cleanupVethPair 清理veth对
func (p *CNIPlugin) cleanupVethPair(hostVethName, netns string) error {
	klog.V(4).Infof("Cleaning up veth pair: host=%s, netns=%s", hostVethName, netns)

	// 删除宿主机端的 veth 接口
	hostVeth, err := netlink.LinkByName(hostVethName)
	if err != nil {
		// 接口可能已经被删除，这是正常的
		klog.V(4).Infof("Host veth %s not found (may already be deleted)", hostVethName)
		return nil
	}

	// 删除宿主机端的 veth 接口
	if err := netlink.LinkDel(hostVeth); err != nil {
		return fmt.Errorf("failed to delete host veth %s: %v", hostVethName, err)
	}

	klog.V(4).Infof("Deleted host veth: %s", hostVethName)

	// 注意：容器端的 veth 接口会在容器删除时自动清理
	// 这里不需要手动删除，因为网络命名空间会被销毁

	return nil
}

// cleanupTailscaleRoutes 清理Tailscale相关路由
func (p *CNIPlugin) cleanupTailscaleRoutes(podInfo *PodInfo) error {
	klog.V(4).Infof("Cleaning up Tailscale routes for pod %s/%s", podInfo.namespace, podInfo.podName)

	// 获取 Pod IP（需要从 IPAM 或其他地方获取）
	// 这里简化处理，实际应该从 IPAM 获取已分配的 IP
	podIP := p.getPodIPFromContainerID(podInfo.containerID)
	if podIP == nil {
		klog.Warningf("Could not determine Pod IP for cleanup, skipping Tailscale route cleanup")
		return nil
	}

	// 查找并删除通过 Tailscale NIC 的路由
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("failed to list routes: %v", err)
	}

	for _, route := range routes {
		// 检查是否是到 Pod IP 的路由
		if route.Dst != nil && route.Dst.IP.Equal(podIP) {
			klog.V(4).Infof("Found route to Pod IP %s, deleting", podIP.String())
			if err := netlink.RouteDel(&route); err != nil {
				klog.Warningf("Failed to delete route %v: %v", route, err)
			} else {
				klog.V(4).Infof("Deleted route to Pod IP %s", podIP.String())
			}
		}
	}

	return nil
}

// getPodIPFromContainerID 从容器ID获取Pod IP
func (p *CNIPlugin) getPodIPFromContainerID(containerID string) net.IP {
	// 从 host-local IPAM 获取已分配的 IP
	if p.hostLocal != nil {
		// 遍历已分配的 IP，查找匹配的容器 ID
		p.hostLocal.mu.RLock()
		defer p.hostLocal.mu.RUnlock()

		for ip, cid := range p.hostLocal.allocated {
			if cid == containerID {
				return net.ParseIP(ip)
			}
		}
	}

	// 从自定义 IPAM 获取已分配的 IP
	if p.ipamManager != nil {
		// 使用 ipamManager 的公共方法获取 Pod IP
		return p.ipamManager.GetIPByContainerID(containerID)
	}

	klog.V(4).Infof("Could not find IP for container ID: %s", containerID)
	return nil
}
