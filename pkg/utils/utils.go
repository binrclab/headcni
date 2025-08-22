package utils

import (
	"net"
	"syscall"

	"github.com/vishvananda/netlink"
)

// AddArp 添加ARP条目
func AddArp(localVtepID int, remoteVtepIP net.IP, remoteVtepMac net.HardwareAddr) error {
	return netlink.NeighSet(&netlink.Neigh{
		LinkIndex:    localVtepID,
		State:        netlink.NUD_PERMANENT,
		Type:         syscall.RTN_UNICAST,
		IP:           remoteVtepIP,
		HardwareAddr: remoteVtepMac,
	})
}

// DelArp 删除ARP条目
func DelArp(localVtepID int, remoteVtepIP net.IP, remoteVtepMac net.HardwareAddr) error {
	return netlink.NeighDel(&netlink.Neigh{
		LinkIndex:    localVtepID,
		State:        netlink.NUD_PERMANENT,
		Type:         syscall.RTN_UNICAST,
		IP:           remoteVtepIP,
		HardwareAddr: remoteVtepMac,
	})
}

// AddFDB 添加FDB条目
func AddFDB(localVtepID int, remoteHostIP net.IP, remoteVtepMac net.HardwareAddr) error {
	return netlink.NeighSet(&netlink.Neigh{
		LinkIndex:    localVtepID,
		Family:       syscall.AF_BRIDGE,
		State:        netlink.NUD_PERMANENT,
		Flags:        netlink.NTF_SELF,
		IP:           remoteHostIP,
		HardwareAddr: remoteVtepMac,
	})
}

// DelFDB 删除FDB条目
func DelFDB(localVtepID int, remoteHostIP net.IP, remoteVtepMac net.HardwareAddr) error {
	return netlink.NeighDel(&netlink.Neigh{
		LinkIndex:    localVtepID,
		Family:       syscall.AF_BRIDGE,
		State:        netlink.NUD_PERMANENT,
		Flags:        netlink.NTF_SELF,
		IP:           remoteHostIP,
		HardwareAddr: remoteVtepMac,
	})
}

// ReplaceRoute 替换路由
func ReplaceRoute(localVtepID int, dst *net.IPNet, gateway net.IP) error {
	return netlink.RouteReplace(&netlink.Route{
		LinkIndex: localVtepID,
		Scope:     netlink.SCOPE_UNIVERSE,
		Dst:       dst,
		Gw:        gateway,
		Flags:     syscall.RTNH_F_ONLINK,
	})
}

// DelRoute 删除路由
func DelRoute(localVtepID int, dst *net.IPNet, gateway net.IP) error {
	return netlink.RouteDel(&netlink.Route{
		LinkIndex: localVtepID,
		Scope:     netlink.SCOPE_UNIVERSE,
		Dst:       dst,
		Gw:        gateway,
		Flags:     syscall.RTNH_F_ONLINK,
	})
}

// GetDefaultGatewayInterface 获取默认网关接口
func GetDefaultGatewayInterface() (*net.Interface, error) {
	routes, err := netlink.RouteList(nil, syscall.AF_INET)
	if err != nil {
		return nil, err
	}

	for _, route := range routes {
		if route.Dst == nil || route.Dst.String() == "0.0.0.0/0" {
			if route.LinkIndex <= 0 {
				return nil, err
			}
			return net.InterfaceByIndex(route.LinkIndex)
		}
	}

	return nil, err
}

// GetIfaceAddr 获取接口地址
func GetIfaceAddr(iface *net.Interface) ([]netlink.Addr, error) {
	return netlink.AddrList(&netlink.Device{
		LinkAttrs: netlink.LinkAttrs{
			Index: iface.Index,
		},
	}, syscall.AF_INET)
}

// NewHardwareAddr 生成新的硬件地址
func NewHardwareAddr() (net.HardwareAddr, error) {
	hardwareAddr := make(net.HardwareAddr, 6)
	if _, err := net.ParseMAC("00:00:00:00:00:00"); err != nil {
		return nil, err
	}

	// ensure that address is locally administered and unicast
	hardwareAddr[0] = (hardwareAddr[0] & 0xfe) | 0x02

	return hardwareAddr, nil
}

// EnsureInterface 确保接口存在
func EnsureInterface(interfaceName string, interfaceType string) error {
	_, err := netlink.LinkByName(interfaceName)
	if err == nil {
		// 接口已存在
		return nil
	}

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
		return err
	}

	if err := netlink.LinkAdd(link); err != nil {
		return err
	}

	return nil
}

// ConfigureInterface 配置接口
func ConfigureInterface(interfaceName string, ip net.IP, mask net.IPMask) error {
	link, err := netlink.LinkByName(interfaceName)
	if err != nil {
		return err
	}

	// 配置 IP 地址
	addr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   ip,
			Mask: mask,
		},
	}

	if err := netlink.AddrAdd(link, addr); err != nil {
		return err
	}

	// 启用接口
	if err := netlink.LinkSetUp(link); err != nil {
		return err
	}

	return nil
}

// DeleteInterface 删除接口
func DeleteInterface(interfaceName string) error {
	link, err := netlink.LinkByName(interfaceName)
	if err != nil {
		// 接口不存在，认为删除成功
		return nil
	}

	if err := netlink.LinkDel(link); err != nil {
		return err
	}

	return nil
}
