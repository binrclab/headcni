package main

import (
	"fmt"

	"github.com/containernetworking/cni/pkg/skel"
	"k8s.io/klog/v2"
)

// cmdDel CNI DEL 命令
func cmdDel(args *skel.CmdArgs) error {
	klog.Infof("CNI DEL called for container %s", args.ContainerID)
	klog.V(4).Infof("CNI DEL netns: %s", args.Netns)
	klog.V(4).Infof("CNI DEL ifName: %s", args.IfName)
	klog.V(4).Infof("CNI DEL args: %s", args.Args)

	plugin, err := loadConfig(args.StdinData)
	if err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	// 解析 Pod 信息
	podInfo, err := parsePodInfo(args.Args)
	if err != nil {
		klog.Warningf("Failed to parse pod info during deletion: %v", err)
		// 删除时解析失败不应阻塞清理过程
	} else {
		klog.Infof("Cleaning up network for pod %s/%s", podInfo.namespace, podInfo.podName)

		// 释放 IP 地址
		plugin.releaseIP(podInfo, args.ContainerID)
	}

	// 清理 veth 对
	hostVethName := vethName(args.ContainerID)
	if err := plugin.cleanupVethPair(hostVethName, args.Netns); err != nil {
		klog.Warningf("Failed to cleanup veth pair: %v", err)
	}

	// 清理宿主机路由
	if err := plugin.cleanupHostRoute(hostVethName); err != nil {
		klog.Warningf("Failed to cleanup host route: %v", err)
	}

	// 如果启用了 Tailscale，清理 Tailscale 相关路由
	if plugin.config.TailscaleNic != "" {
		if err := plugin.cleanupTailscaleRoutes(podInfo); err != nil {
			klog.Warningf("Failed to cleanup Tailscale routes: %v", err)
		}
	}

	klog.Infof("Successfully cleaned up network for container %s", args.ContainerID)
	return nil
}
