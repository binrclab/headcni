// pkg/networking/manager.go
package networking

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/containernetworking/plugins/pkg/ns"
	"github.com/vishvananda/netlink"
	"k8s.io/klog/v2"
)

// Config 定义网络管理器配置
type Config struct {
	MTU        int
	EnableIPv6 bool
}

// NetworkManager 是网络管理器
type NetworkManager struct {
	config *Config
}

// NewNetworkManager 创建新的网络管理器
func NewNetworkManager(config *Config) (*NetworkManager, error) {
	if config.MTU == 0 {
		config.MTU = 1420
	}

	return &NetworkManager{
		config: config,
	}, nil
}

// CheckSystemResources 检查系统资源状态
func (nm *NetworkManager) CheckSystemResources() error {
	// 检查可用的网络接口索引
	links, err := netlink.LinkList()
	if err != nil {
		return fmt.Errorf("failed to list network links: %v", err)
	}

	// 检查是否有过多的网络接口
	if len(links) > 1000 {
		klog.Warningf("Large number of network interfaces detected: %d", len(links))
	}

	// 检查是否有重复的接口名称
	interfaceNames := make(map[string]bool)
	for _, link := range links {
		name := link.Attrs().Name
		if interfaceNames[name] {
			klog.Warningf("Duplicate interface name detected: %s", name)
		}
		interfaceNames[name] = true
	}

	// 检查系统限制
	if err := nm.checkSystemLimits(); err != nil {
		return fmt.Errorf("system limits check failed: %v", err)
	}

	return nil
}

// checkSystemLimits 检查系统限制
func (nm *NetworkManager) checkSystemLimits() error {
	// 检查 /proc/sys/net/core/dev_weight 值
	devWeightPath := "/proc/sys/net/core/dev_weight"
	if data, err := os.ReadFile(devWeightPath); err == nil {
		klog.V(4).Infof("Network device weight: %s", string(data))
	}

	// 检查 /proc/sys/net/core/netdev_budget 值
	budgetPath := "/proc/sys/net/core/netdev_budget"
	if data, err := os.ReadFile(budgetPath); err == nil {
		klog.V(4).Infof("Network device budget: %s", string(data))
	}

	return nil
}

// CreateVethPair 创建 veth pair
func (nm *NetworkManager) CreateVethPair(netnsPath, containerIfName, hostIfName string) error {
	// 验证接口名称
	if containerIfName == "" || hostIfName == "" {
		return fmt.Errorf("invalid interface names: container=%s, host=%s", containerIfName, hostIfName)
	}

	// 验证网络命名空间路径
	if netnsPath == "" {
		return fmt.Errorf("invalid network namespace path")
	}

	// 检查网络命名空间是否存在
	if _, err := os.Stat(netnsPath); os.IsNotExist(err) {
		return fmt.Errorf("network namespace %s does not exist", netnsPath)
	}

	// 检查系统资源状态
	if err := nm.CheckSystemResources(); err != nil {
		klog.Warningf("System resources check failed: %v", err)
		// 继续执行，但记录警告
	}

	// 记录创建前的系统状态
	klog.V(4).Infof("Creating veth pair: container=%s, host=%s, netns=%s",
		containerIfName, hostIfName, netnsPath)

	// 检查当前网络接口数量
	links, err := netlink.LinkList()
	if err == nil {
		klog.V(4).Infof("Current network interfaces count: %d", len(links))
	}

	// 如果同名的veth已经存在了，删除
	if oldHostVeth, err := netlink.LinkByName(hostIfName); err == nil {
		klog.V(4).Infof("Found existing host veth %s, deleting...", hostIfName)
		if err = netlink.LinkDel(oldHostVeth); err != nil {
			return fmt.Errorf("failed to delete old hostVeth %s: %v", hostIfName, err)
		}
		// 等待一下确保接口完全删除
		time.Sleep(100 * time.Millisecond)
	}

	// 在容器网络命名空间中创建veth pair
	return ns.WithNetNSPath(netnsPath, func(hostNS ns.NetNS) error {
		// 创建 veth pair 配置
		veth := &netlink.Veth{
			LinkAttrs: netlink.LinkAttrs{
				Name: containerIfName,
				MTU:  nm.config.MTU,
			},
			PeerName: hostIfName,
		}

		klog.V(4).Infof("Attempting to create veth pair with MTU: %d", nm.config.MTU)

		// 创建veth pair，添加重试逻辑
		var err error
		for retries := 0; retries < 3; retries++ {
			err = netlink.LinkAdd(veth)
			if err == nil {
				break
			}

			klog.V(4).Infof("Veth creation attempt %d failed: %v", retries+1, err)

			// 如果是接口已存在的错误，尝试删除后重试
			if strings.Contains(err.Error(), "file exists") || strings.Contains(err.Error(), "already exists") {
				klog.V(4).Infof("Interface already exists, retrying... (attempt %d/3)", retries+1)
				// 尝试删除已存在的接口
				if oldLink, lookupErr := netlink.LinkByName(containerIfName); lookupErr == nil {
					netlink.LinkDel(oldLink)
				}
				if oldLink, lookupErr := netlink.LinkByName(hostIfName); lookupErr == nil {
					netlink.LinkDel(oldLink)
				}
				time.Sleep(200 * time.Millisecond)
				continue
			}

			// 如果是数值超出范围的错误，提供更多调试信息
			if strings.Contains(err.Error(), "numerical result out of range") {
				klog.Errorf("Numerical result out of range error detected!")
				klog.Errorf("This usually indicates a system resource exhaustion issue")
				klog.Errorf("Container interface: %s", containerIfName)
				klog.Errorf("Host interface: %s", hostIfName)
				klog.Errorf("Network namespace: %s", netnsPath)
				klog.Errorf("MTU: %d", nm.config.MTU)

				// 记录当前系统状态
				if links, listErr := netlink.LinkList(); listErr == nil {
					klog.Errorf("Current network interfaces: %d", len(links))
					for i, link := range links {
						if i < 5 { // 只记录前5个
							klog.Errorf("  Interface %d: %s (index: %d)", i, link.Attrs().Name, link.Attrs().Index)
						}
					}
				}
			}

			// 其他错误直接返回
			break
		}

		if err != nil {
			return fmt.Errorf("failed to create veth pair after retries: %v", err)
		}

		klog.V(4).Infof("Veth pair created successfully, configuring interfaces...")

		// 获取宿主机端的veth
		hostVeth, err := netlink.LinkByName(hostIfName)
		if err != nil {
			return fmt.Errorf("failed to lookup %s: %v", hostIfName, err)
		}

		// 设置宿主机端veth的MAC地址
		defaultHostVethMac, _ := net.ParseMAC("EE:EE:EE:EE:EE:EE")
		if err = netlink.LinkSetHardwareAddr(hostVeth, defaultHostVethMac); err != nil {
			klog.V(4).Infof("failed to Set MAC of %s: %v. Using kernel generated MAC.", hostIfName, err)
		}

		// 启用宿主机端的veth
		if err = netlink.LinkSetUp(hostVeth); err != nil {
			return fmt.Errorf("failed to set %s up: %v", hostIfName, err)
		}

		// 获取容器端的veth
		contVeth, err := netlink.LinkByName(containerIfName)
		if err != nil {
			return fmt.Errorf("failed to lookup %s: %v", containerIfName, err)
		}

		// 启用容器端的veth
		if err = netlink.LinkSetUp(contVeth); err != nil {
			return fmt.Errorf("failed to set %s up: %v", containerIfName, err)
		}

		// 将宿主机端veth移动到宿主机网络命名空间
		if err = netlink.LinkSetNsFd(hostVeth, int(hostNS.Fd())); err != nil {
			return fmt.Errorf("failed to move veth to host netns: %v", err)
		}

		klog.V(4).Infof("Created veth pair: %s (host) <-> %s (container)",
			hostIfName, containerIfName)

		return nil
	})
}

// SetupVethProxyARP 设置veth的ARP代理
func (nm *NetworkManager) SetupVethProxyARP(hostVethName string) error {
	// 设置ARP代理
	if err := nm.writeProcSys(fmt.Sprintf("/proc/sys/net/ipv4/conf/%s/proxy_arp", hostVethName), "1"); err != nil {
		return fmt.Errorf("failed to set net.ipv4.conf.%s.proxy_arp=1: %v", hostVethName, err)
	}

	klog.V(4).Infof("Set proxy_arp=1 for %s", hostVethName)
	return nil
}

// SetupPodNetwork 配置Pod网络
func (nm *NetworkManager) SetupPodNetwork(netnsPath, containerIfName string, podIP net.IP, gateway net.IP) error {
	return ns.WithNetNSPath(netnsPath, func(hostNS ns.NetNS) error {
		// 获取容器端veth
		contVeth, err := netlink.LinkByName(containerIfName)
		if err != nil {
			return fmt.Errorf("failed to lookup %s: %v", containerIfName, err)
		}

		// 配置IP地址
		addr := &netlink.Addr{
			IPNet: &net.IPNet{
				IP:   podIP,
				Mask: net.CIDRMask(32, 32), // /32 主机路由
			},
		}

		if err = netlink.AddrAdd(contVeth, addr); err != nil {
			return fmt.Errorf("failed to add IP addr to %s: %v", containerIfName, err)
		}

		// 添加网关路由（确保网关可达）
		defaultGwIPNet := &net.IPNet{IP: gateway, Mask: net.CIDRMask(32, 32)}
		if err := netlink.RouteAdd(
			&netlink.Route{
				LinkIndex: contVeth.Attrs().Index,
				Scope:     netlink.SCOPE_LINK,
				Dst:       defaultGwIPNet,
			},
		); err != nil {
			return fmt.Errorf("failed to add gateway route: %v", err)
		}

		// 添加默认路由
		_, IPv4AllNet, _ := net.ParseCIDR("0.0.0.0/0")
		defaultRoute := &netlink.Route{
			LinkIndex: contVeth.Attrs().Index,
			Scope:     netlink.SCOPE_UNIVERSE,
			Dst:       IPv4AllNet,
			Gw:        gateway,
		}

		if err = netlink.RouteAdd(defaultRoute); err != nil {
			return fmt.Errorf("failed to add default route: %v", err)
		}

		klog.V(4).Infof("Configured pod network: IP=%s, Gateway=%s", podIP.String(), gateway.String())
		return nil
	})
}

// SetupHostRoute 配置宿主机路由
func (nm *NetworkManager) SetupHostRoute(hostVethName string, podIP net.IP) error {
	// 获取宿主机上的veth接口
	hostVeth, err := netlink.LinkByName(hostVethName)
	if err != nil {
		return fmt.Errorf("failed to find host veth %s: %v", hostVethName, err)
	}

	// 启用宿主机veth接口
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

// CleanupHostRoute 清理宿主机路由
func (nm *NetworkManager) CleanupHostRoute(hostVethName string) error {
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

// VethNameForWorkload 生成veth名称
func (nm *NetworkManager) VethNameForWorkload(namespace, podname string) string {
	// A SHA1 is always 20 bytes long, and so is sufficient for generating the
	// veth name and mac addr.
	h := sha1.New()
	h.Write([]byte(fmt.Sprintf("%s.%s", namespace, podname)))
	return fmt.Sprintf("veth%s", hex.EncodeToString(h.Sum(nil))[:11])
}

// writeProcSys 写入proc文件系统
func (nm *NetworkManager) writeProcSys(path, value string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err = f.WriteString(value); err != nil {
		return err
	}
	return nil
}

// InterfaceExists 检查接口是否存在
func (nm *NetworkManager) InterfaceExists(interfaceName string) bool {
	_, err := netlink.LinkByName(interfaceName)
	return err == nil
}

// CreateInterface 创建网络接口
func (nm *NetworkManager) CreateInterface(interfaceName string, interfaceType string) error {
	var link netlink.Link

	switch interfaceType {
	case "dummy":
		link = &netlink.Dummy{
			LinkAttrs: netlink.LinkAttrs{
				Name: interfaceName,
			},
		}
	case "bridge":
		link = &netlink.Bridge{
			LinkAttrs: netlink.LinkAttrs{
				Name: interfaceName,
			},
		}
	default:
		return fmt.Errorf("unsupported interface type: %s", interfaceType)
	}

	if err := netlink.LinkAdd(link); err != nil {
		return fmt.Errorf("failed to create interface %s: %v", interfaceName, err)
	}

	klog.V(4).Infof("Created interface: %s (type: %s)", interfaceName, interfaceType)
	return nil
}

// ConfigureInterface 配置网络接口
func (nm *NetworkManager) ConfigureInterface(interfaceName string, ip net.IP, mask net.IPMask) error {
	link, err := netlink.LinkByName(interfaceName)
	if err != nil {
		return fmt.Errorf("failed to find interface %s: %v", interfaceName, err)
	}

	// 配置 IP 地址
	addr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   ip,
			Mask: mask,
		},
	}

	if err := netlink.AddrAdd(link, addr); err != nil {
		return fmt.Errorf("failed to add IP to interface %s: %v", interfaceName, err)
	}

	// 启用接口
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to enable interface %s: %v", interfaceName, err)
	}

	klog.V(4).Infof("Configured interface: %s with IP %s", interfaceName, ip.String())
	return nil
}

// DeleteInterface 删除网络接口
func (nm *NetworkManager) DeleteInterface(interfaceName string) error {
	link, err := netlink.LinkByName(interfaceName)
	if err != nil {
		// 接口不存在，认为删除成功
		return nil
	}

	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("failed to delete interface %s: %v", interfaceName, err)
	}

	klog.V(4).Infof("Deleted interface: %s", interfaceName)
	return nil
}
