package k8s

import (
	"net"

	"github.com/pkg/errors"
	coreV1 "k8s.io/api/core/v1"
)

// SetNodeAnnotation 设置节点注解
func (c *Client) SetNodeAnnotation(node *coreV1.Node, key, value string) error {
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}
	node.Annotations[key] = value
	return c.PatchNode(node, node)
}

// GetNodeAnnotation 获取节点注解
func GetNodeAnnotation(node *coreV1.Node, key string) string {
	if node.Annotations == nil {
		return ""
	}
	return node.Annotations[key]
}

// SetNodeAnnotations 批量设置节点注解
func (c *Client) SetNodeAnnotations(node *coreV1.Node, annotations map[string]string) error {
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}

	for key, value := range annotations {
		node.Annotations[key] = value
	}

	return c.PatchNode(node, node)
}

// RemoveNodeAnnotation 移除节点注解
func (c *Client) RemoveNodeAnnotation(node *coreV1.Node, key string) error {
	if node.Annotations == nil {
		return nil
	}

	delete(node.Annotations, key)
	return c.PatchNode(node, node)
}

// HasNodeAnnotation 检查节点是否有指定注解
func HasNodeAnnotation(node *coreV1.Node, key string) bool {
	if node.Annotations == nil {
		return false
	}
	_, exists := node.Annotations[key]
	return exists
}

// GetNodeAnnotationAsIP 获取节点注解并转换为IP地址
func GetNodeAnnotationAsIP(node *coreV1.Node, key string) net.IP {
	value := GetNodeAnnotation(node, key)
	if value == "" {
		return nil
	}
	return net.ParseIP(value)
}

// 向后兼容的全局函数
func SetNodeAnnotation(node *coreV1.Node, key, value string) error {
	if globalClient == nil {
		return errors.New("global client not initialized")
	}
	return globalClient.SetNodeAnnotation(node, key, value)
}

func SetNodeAnnotations(node *coreV1.Node, annotations map[string]string) error {
	if globalClient == nil {
		return errors.New("global client not initialized")
	}
	return globalClient.SetNodeAnnotations(node, annotations)
}

func RemoveNodeAnnotation(node *coreV1.Node, key string) error {
	if globalClient == nil {
		return errors.New("global client not initialized")
	}
	return globalClient.RemoveNodeAnnotation(node, key)
}
