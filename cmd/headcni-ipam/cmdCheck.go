package main

import (
	"context"
	"fmt"

	"github.com/binrclab/headcni/pkg/ipam"
	"github.com/containernetworking/cni/pkg/skel"
	"k8s.io/klog/v2"
)

func cmdCheck(args *skel.CmdArgs) error {
	conf, err := loadNetConf(args.StdinData)
	if err != nil {
		return fmt.Errorf("failed to load netconf: %v", err)
	}

	klog.V(4).Infof("IPAM CHECK called for container %s", args.ContainerID)
	klog.V(4).Infof("IPAM CHECK args: %s", args.Args)

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

	klog.V(4).Infof("Checking IPAM for pod %s/%s", podInfo.Namespace, podInfo.Name)

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

	// 检查IP分配状态（使用幂等性检查）
	allocation, err := manager.AllocateIP(context.Background(), podInfo.Namespace, podInfo.Name, args.ContainerID)
	if err != nil {
		return fmt.Errorf("IP allocation check failed: %v", err)
	}

	// 验证IP是否在子网范围内
	if !subnet.Contains(allocation.IP) {
		return fmt.Errorf("allocated IP %s is not in subnet %s", allocation.IP.String(), subnet.String())
	}

	klog.V(4).Infof("IPAM check passed for pod %s/%s with IP %s",
		podInfo.Namespace, podInfo.Name, allocation.IP.String())

	return nil
}
