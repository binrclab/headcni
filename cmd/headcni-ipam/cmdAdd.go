package main

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/binrclab/headcni/pkg/ipam"
	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	types100 "github.com/containernetworking/cni/pkg/types/100"
	"k8s.io/klog/v2"
)

func cmdAdd(args *skel.CmdArgs) error {
	conf, err := loadNetConf(args.StdinData)
	if err != nil {
		return fmt.Errorf("failed to load netconf: %v", err)
	}

	klog.Infof("IPAM ADD called for container %s", args.ContainerID)
	klog.V(4).Infof("IPAM ADD args: %s", args.Args)

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

	klog.Infof("Processing IPAM request for pod %s/%s", podInfo.Namespace, podInfo.Name)

	// 获取子网配置
	subnet, gateway, err := getSubnetConfig(conf.IPAM)
	if err != nil {
		return fmt.Errorf("failed to get subnet config: %v", err)
	}

	klog.V(4).Infof("Using subnet: %s, gateway: %s", subnet.String(), gateway.String())

	// 创建IPAM管理器
	// 注意：这里应该使用节点名而不是命名空间来创建管理器
	// 因为同一个节点上的所有 Pod 应该共享同一个 IPAM 管理器
	nodeName := getNodeName()
	manager, err := ipam.NewIPAMManager(nodeName, subnet)
	if err != nil {
		return fmt.Errorf("failed to create IPAM manager: %v", err)
	}

	// 分配IP
	allocation, err := manager.AllocateIP(context.Background(), podInfo.Namespace, podInfo.Name, args.ContainerID)
	if err != nil {
		return fmt.Errorf("failed to allocate IP: %v", err)
	}

	klog.Infof("Allocated IP %s for pod %s/%s", allocation.IP.String(), podInfo.Namespace, podInfo.Name)

	// 构造结果
	result := &types100.Result{
		CNIVersion: types100.ImplementedSpecVersion,
		IPs: []*types100.IPConfig{
			{
				Address: net.IPNet{
					IP:   allocation.IP,
					Mask: subnet.Mask,
				},
				Gateway: gateway,
			},
		},
		// 不设置 Routes，让主插件处理路由配置
	}

	return types.PrintResult(result, conf.CNIVersion)
}

// getNodeName 获取节点名
func getNodeName() string {
	// 优先从环境变量获取
	if nodeName := os.Getenv("NODE_NAME"); nodeName != "" {
		return nodeName
	}

	// 从主机名获取
	if hostname, err := os.Hostname(); err == nil {
		return hostname
	}

	// 默认值
	return "unknown-node"
}
