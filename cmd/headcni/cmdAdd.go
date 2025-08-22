package main

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/binrclab/headcni/pkg/ipam"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	"k8s.io/klog/v2"
)

const (
	hostVethPairPrefix = "veth"
)

var (
	defaultHostVethMac, _ = net.ParseMAC("EE:EE:EE:EE:EE:EE")
	defaultPodGw          = net.IPv4(169, 254, 1, 1)
	defaultGwIPNet        = &net.IPNet{IP: defaultPodGw, Mask: net.CIDRMask(32, 32)}
	_, IPv4AllNet, _      = net.ParseCIDR("0.0.0.0/0")
	defaultRoutes         = []*net.IPNet{IPv4AllNet}
)

// cmdAdd CNI ADD 命令
func cmdAdd(args *skel.CmdArgs) error {
	klog.Infof("[cmdAdd] containerID: %s", args.ContainerID)
	klog.Infof("[cmdAdd] netNs: %s", args.Netns)
	klog.Infof("[cmdAdd] ifName: %s", args.IfName)
	klog.Infof("[cmdAdd] args: %s", args.Args)
	klog.Infof("[cmdAdd] path: %s", args.Path)
	klog.Infof("[cmdAdd] stdin: %s", string(args.StdinData))

	plugin, err := loadConfig(args.StdinData)
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

	// 从IPAM管理器获取本地池CIDR
	if plugin.ipamManager != nil {
		localPoolCIDR := plugin.ipamManager.GetLocalPoolCIDR()
		if localPoolCIDR != "" {
			podInfo.podCIDR = localPoolCIDR
			klog.Infof("Using local pool from IPAM: %s", podInfo.podCIDR)

			// 显示本地池统计信息
			if stats := plugin.ipamManager.GetLocalPoolStats(); stats != nil {
				klog.Infof("Local pool stats: CIDR=%s, Total=%d, Allocated=%d, Reserved=%d, Available=%d",
					stats["cidr"], stats["total_ips"], stats["allocated_ips"], stats["reserved_ips"], stats["available_ips"])
			}
		} else {
			// 如果IPAM管理器还没有初始化，从IPAM配置中获取
			podInfo.podCIDR = plugin.getPodCIDRFromIPAM()
			klog.Infof("Using local pool from IPAM config: %s", podInfo.podCIDR)
		}
	} else {
		// 如果IPAM管理器还没有初始化，从IPAM配置中获取
		podInfo.podCIDR = plugin.getPodCIDRFromIPAM()
		klog.Infof("Using local pool from IPAM config: %s", podInfo.podCIDR)
	}

	// 根据 IPAM 类型分配 IP 地址
	var allocation *ipam.IPAllocation

	switch plugin.config.IPAM.Type {
	case "headcni-ipam":
		// 使用自定义 IPAM
		if plugin.ipamManager == nil {
			return fmt.Errorf("IPAM manager not initialized for type: %s", plugin.config.IPAM.Type)
		}
		allocation, err = plugin.ipamManager.AllocateIP(
			ctx,
			podInfo.namespace,
			podInfo.podName,
			args.ContainerID,
		)
		if err != nil {
			return fmt.Errorf("IPAM allocation failed: %v", err)
		}

	case "host-local":
		// 使用 host-local IPAM
		if plugin.hostLocal == nil {
			return fmt.Errorf("host-local IPAM not initialized for type: %s", plugin.config.IPAM.Type)
		}
		allocation, err = plugin.hostLocal.AllocateIP(args.ContainerID, podInfo)
		if err != nil {
			return fmt.Errorf("host-local allocation failed: %v", err)
		}

	default:
		return fmt.Errorf("unsupported IPAM type: %s (supported: host-local, headcni-ipam)", plugin.config.IPAM.Type)
	}

	// 配置 Pod 网络
	result, err := plugin.setupPodNetwork(args, allocation)
	if err != nil {
		// 出错时清理分配的 IP
		plugin.releaseIP(podInfo, args.ContainerID)
		return fmt.Errorf("failed to setup pod network: %v", err)
	}

	// 通知 Daemon Pod 网络已配置完成
	if plugin.cniClient != nil {
		if err := plugin.notifyDaemonPodReady(podInfo, allocation); err != nil {
			klog.Warningf("Failed to notify daemon: %v", err)
		}
	}

	klog.Infof("Successfully configured network for pod %s/%s with IP %s",
		podInfo.namespace, podInfo.podName, allocation.IP.String())

	return types.PrintResult(result, plugin.config.CNIVersion)
}
