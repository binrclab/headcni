package main

import (
	"context"
	"fmt"

	"github.com/binrclab/headcni/pkg/ipam"
	"github.com/containernetworking/cni/pkg/skel"
	"k8s.io/klog/v2"
)

func cmdDel(args *skel.CmdArgs) error {
	conf, err := loadNetConf(args.StdinData)
	if err != nil {
		return fmt.Errorf("failed to load netconf: %v", err)
	}

	klog.Infof("IPAM DEL called for container %s", args.ContainerID)
	klog.V(4).Infof("IPAM DEL args: %s", args.Args)

	// 解析 Pod 信息
	podInfo, err := parsePodInfo(args.Args)
	if err != nil {
		klog.Warningf("Failed to parse pod info: %v", err)
		// 使用默认值
		podInfo = &PodInfo{
			Namespace: "default",
			Name:      args.ContainerID,
			UID:       args.ContainerID,
		}
	}

	klog.Infof("Releasing IP for pod %s/%s", podInfo.Namespace, podInfo.Name)

	// 获取子网配置
	subnet, _, err := getSubnetConfig(conf.IPAM)
	if err != nil {
		return fmt.Errorf("failed to get subnet config: %v", err)
	}

	// 创建IPAM管理器（使用相同的节点名）
	nodeName := getNodeName()
	manager, err := ipam.NewIPAMManager(nodeName, subnet)
	if err != nil {
		return fmt.Errorf("failed to create IPAM manager: %v", err)
	}

	// 释放IP
	err = manager.ReleaseIP(context.Background(), podInfo.Namespace, podInfo.Name)
	if err != nil {
		return fmt.Errorf("failed to release IP: %v", err)
	}

	klog.Infof("Released IP for pod %s/%s", podInfo.Namespace, podInfo.Name)
	return nil
}
