package main

import (
	"fmt"

	"github.com/containernetworking/cni/pkg/skel"
	"k8s.io/klog/v2"
)

// cmdCheck CNI CHECK 命令
func cmdCheck(args *skel.CmdArgs) error {
	plugin, err := loadConfig(args.StdinData)
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
