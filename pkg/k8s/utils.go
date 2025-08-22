package k8s

import (
	"context"
	"net"
	"time"

	coreV1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

// WaitForNodeReady 等待节点就绪
func WaitForNodeReady(nodeName string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			node, err := GetNodeByID(nodeName)
			if err != nil {
				klog.Warningf("Failed to get node %s: %v", nodeName, err)
				continue
			}

			if IsNodeReady(node) {
				klog.Infof("Node %s is ready", nodeName)
				return nil
			}
		}
	}
}

// IsNodeReady 检查节点是否就绪
func IsNodeReady(node *coreV1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == coreV1.NodeReady {
			return condition.Status == coreV1.ConditionTrue
		}
	}
	return false
}

// GetNodeIP 获取节点IP地址
func GetNodeIP(node *coreV1.Node) string {
	for _, addr := range node.Status.Addresses {
		if addr.Type == coreV1.NodeInternalIP {
			return addr.Address
		}
	}
	return ""
}

// GetNodeExternalIP 获取节点外部IP地址
func GetNodeExternalIP(node *coreV1.Node) string {
	for _, addr := range node.Status.Addresses {
		if addr.Type == coreV1.NodeExternalIP {
			return addr.Address
		}
	}
	return ""
}

// GetNodePodCIDR 获取节点Pod CIDR
func GetNodePodCIDR(node *coreV1.Node) string {
	// 优先从 PodCIDRs 数组获取（支持双栈）
	if len(node.Spec.PodCIDRs) > 0 {
		return node.Spec.PodCIDRs[0] // 返回第一个CIDR
	}

	// 从 PodCIDR 字段获取（单栈或旧版本）
	return node.Spec.PodCIDR
}

// GetNodePodCIDRs 获取节点所有Pod CIDR
func GetNodePodCIDRs(node *coreV1.Node) []string {
	if len(node.Spec.PodCIDRs) > 0 {
		return node.Spec.PodCIDRs
	}

	if node.Spec.PodCIDR != "" {
		return []string{node.Spec.PodCIDR}
	}

	return nil
}

// HasNodeLabel 检查节点是否有指定标签
func HasNodeLabel(node *coreV1.Node, key string) bool {
	if node.Labels == nil {
		return false
	}
	_, exists := node.Labels[key]
	return exists
}

// GetNodeLabel 获取节点标签值
func GetNodeLabel(node *coreV1.Node, key string) string {
	if node.Labels == nil {
		return ""
	}
	return node.Labels[key]
}

// SetNodeLabel 设置节点标签
func SetNodeLabel(node *coreV1.Node, key, value string) error {
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}
	node.Labels[key] = value
	return PatchNode(node, node)
}

// RemoveNodeLabel 移除节点标签
func RemoveNodeLabel(node *coreV1.Node, key string) error {
	if node.Labels == nil {
		return nil
	}
	delete(node.Labels, key)
	return PatchNode(node, node)
}

// IsValidCIDR 验证 CIDR 格式
func IsValidCIDR(cidr string) bool {
	_, _, err := net.ParseCIDR(cidr)
	return err == nil
}
