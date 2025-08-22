package k8s

import (
	"time"

	coreV1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

// NodeEventHandler 节点事件处理器接口
type NodeEventHandler interface {
	OnNodeAdd(node *coreV1.Node) error
	OnNodeUpdate(oldNode, newNode *coreV1.Node) error
	OnNodeDelete(node *coreV1.Node) error
}

// NodeFilter 节点过滤器函数类型
type NodeFilter func(node *coreV1.Node) bool

// Controller Kubernetes 控制器
type Controller struct {
	handler NodeEventHandler
	filter  NodeFilter
	stopCh  chan struct{}
}

// NewController 创建新的控制器
func NewController(handler NodeEventHandler, filter NodeFilter) *Controller {
	return &Controller{
		handler: handler,
		filter:  filter,
		stopCh:  make(chan struct{}),
	}
}

// Start 启动控制器
func (c *Controller) Start() {
	go c.watchNodes()
}

// Stop 停止控制器
func (c *Controller) Stop() {
	close(c.stopCh)
}

// watchNodes 监听节点变化
func (c *Controller) watchNodes() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	lastNodes := make(map[string]*coreV1.Node)

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			nodes, err := GetAllNodes()
			if err != nil {
				klog.Errorf("Failed to get nodes: %v", err)
				continue
			}

			currentNodes := make(map[string]*coreV1.Node)
			for _, node := range nodes {
				if c.filter != nil && !c.filter(node) {
					continue
				}

				currentNodes[node.Name] = node

				// 检查是否是新增节点
				if _, exists := lastNodes[node.Name]; !exists {
					if err := c.handler.OnNodeAdd(node); err != nil {
						klog.Errorf("Failed to handle node add event for %s: %v", node.Name, err)
					}
				} else {
					// 检查是否有更新
					if err := c.handler.OnNodeUpdate(lastNodes[node.Name], node); err != nil {
						klog.Errorf("Failed to handle node update event for %s: %v", node.Name, err)
					}
				}
			}

			// 检查删除的节点
			for name, node := range lastNodes {
				if _, exists := currentNodes[name]; !exists {
					if err := c.handler.OnNodeDelete(node); err != nil {
						klog.Errorf("Failed to handle node delete event for %s: %v", node.Name, err)
					}
				}
			}

			lastNodes = currentNodes
		}
	}
}

// FilterNodeByName 根据节点名称过滤
func FilterNodeByName(nodeName string) NodeFilter {
	return func(node *coreV1.Node) bool {
		return node.Name == nodeName
	}
}

// FilterNodeByLabel 根据标签过滤
func FilterNodeByLabel(key, value string) NodeFilter {
	return func(node *coreV1.Node) bool {
		if node.Labels == nil {
			return false
		}
		return node.Labels[key] == value
	}
}

// FilterNodeByAnnotation 根据注解过滤
func FilterNodeByAnnotation(key, value string) NodeFilter {
	return func(node *coreV1.Node) bool {
		if node.Annotations == nil {
			return false
		}
		return node.Annotations[key] == value
	}
}

// FilterNodeByCondition 根据节点条件过滤
func FilterNodeByCondition(conditionType coreV1.NodeConditionType, status coreV1.ConditionStatus) NodeFilter {
	return func(node *coreV1.Node) bool {
		for _, condition := range node.Status.Conditions {
			if condition.Type == conditionType && condition.Status == status {
				return true
			}
		}
		return false
	}
}
