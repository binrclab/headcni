package k8s

import (
	"context"
	"time"

	coreV1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// =============================================================================
// Core Types
// =============================================================================

// PermissionStatus 权限状态
type PermissionStatus struct {
	CanListNodes      bool `json:"canListNodes"`
	CanGetNodes       bool `json:"canGetNodes"`
	CanListServices   bool `json:"canListServices"`
	CanGetServices    bool `json:"canGetServices"`
	CanListPods       bool `json:"canListPods"`
	CanGetPods        bool `json:"canGetPods"`
	CanListConfigMaps bool `json:"canListConfigMaps"`
	CanGetConfigMaps  bool `json:"canGetConfigMaps"`
}

// =============================================================================
// Core Client Interface
// =============================================================================

// Client Kubernetes 客户端接口
type Client interface {
	// 连接管理
	Connect(ctx context.Context) error
	Disconnect() error

	// Informer 管理
	StartInformers(ctx context.Context) error
	WaitForCacheSync(ctx context.Context) error

	// 节点信息
	GetCurrentNodeName() (string, error)
	GetCurrentNode() (*coreV1.Node, error)

	// 资源客户端
	Nodes() NodeInterface
	Services() ServiceInterface
	Pods() PodInterface
	ConfigMaps() ConfigMapInterface

	// DNS 相关
	GetDNSServiceIP() (string, error)
	GetClusterDomain() (string, error)

	// 权限管理
	GetPermissions() *PermissionStatus
}

// =============================================================================
// Resource Interfaces
// =============================================================================

// NodeInterface 节点操作接口
type NodeInterface interface {
	// 基础操作
	Get(ctx context.Context, name string) (*coreV1.Node, error)
	List(ctx context.Context, opts *ListOptions) ([]*coreV1.Node, error)
	Create(ctx context.Context, node *coreV1.Node) (*coreV1.Node, error)
	Update(ctx context.Context, node *coreV1.Node) (*coreV1.Node, error)
	Delete(ctx context.Context, name string) error
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte) (*coreV1.Node, error)

	// 特殊操作
	GetPodCIDR(name string) (string, error)
	GetAllPodCIDRs() ([]string, error)
	UpdateAnnotations(name string, annotations map[string]string) error
	UpdateLabels(name string, labels map[string]string) error
}

// ServiceInterface 服务操作接口
type ServiceInterface interface {
	// 基础操作
	Get(ctx context.Context, namespace, name string) (*coreV1.Service, error)
	List(ctx context.Context, namespace string, opts *ListOptions) ([]*coreV1.Service, error)
	Create(ctx context.Context, namespace string, service *coreV1.Service) (*coreV1.Service, error)
	Update(ctx context.Context, namespace string, service *coreV1.Service) (*coreV1.Service, error)
	Delete(ctx context.Context, namespace, name string) error

	// 特殊操作
	GetClusterIP(namespace, name string) (string, error)
	GetPorts(namespace, name string) ([]coreV1.ServicePort, error)
	GetEndpoints(namespace, name string) ([]string, error)
}

// PodInterface Pod 操作接口
type PodInterface interface {
	// 基础操作
	Get(ctx context.Context, namespace, name string) (*coreV1.Pod, error)
	List(ctx context.Context, namespace string, opts *ListOptions) ([]*coreV1.Pod, error)
	Create(ctx context.Context, namespace string, pod *coreV1.Pod) (*coreV1.Pod, error)
	Update(ctx context.Context, namespace string, pod *coreV1.Pod) (*coreV1.Pod, error)
	Delete(ctx context.Context, namespace, name string) error

	// 特殊操作
	GetByNode(nodeName string) ([]*coreV1.Pod, error)
	GetByLabel(namespace, labelKey, labelValue string) ([]*coreV1.Pod, error)
	GetPodIP(namespace, name string) (string, error)
}

// ConfigMapInterface ConfigMap 操作接口
type ConfigMapInterface interface {
	// 基础操作
	Get(ctx context.Context, namespace, name string) (*coreV1.ConfigMap, error)
	List(ctx context.Context, namespace string, opts *ListOptions) ([]*coreV1.ConfigMap, error)
	Create(ctx context.Context, namespace string, configMap *coreV1.ConfigMap) (*coreV1.ConfigMap, error)
	Update(ctx context.Context, namespace string, configMap *coreV1.ConfigMap) (*coreV1.ConfigMap, error)
	Delete(ctx context.Context, namespace, name string) error

	// 特殊操作
	GetData(namespace, name, key string) (string, error)
	UpdateData(namespace, name string, data map[string]string) error
}

// =============================================================================
// Supporting Types
// =============================================================================

// ListOptions 列表选项
type ListOptions struct {
	LabelSelector        string        `json:"labelSelector,omitempty"`
	FieldSelector        string        `json:"fieldSelector,omitempty"`
	Limit                int64         `json:"limit,omitempty"`
	Continue             string        `json:"continue,omitempty"`
	Timeout              time.Duration `json:"timeout,omitempty"`
	Watch                bool          `json:"watch,omitempty"`
	ResourceVersion      string        `json:"resourceVersion,omitempty"`
	ResourceVersionMatch string        `json:"resourceVersionMatch,omitempty"`
}

// =============================================================================
// Event Handlers
// =============================================================================

// NodeEventHandler 节点事件处理器接口
type NodeEventHandler interface {
	OnNodeAdd(node *coreV1.Node) error
	OnNodeUpdate(oldNode, newNode *coreV1.Node) error
	OnNodeDelete(node *coreV1.Node) error
}

// ServiceEventHandler 服务事件处理器接口
type ServiceEventHandler interface {
	OnServiceAdd(service *coreV1.Service) error
	OnServiceUpdate(oldService, newService *coreV1.Service) error
	OnServiceDelete(service *coreV1.Service) error
}

// PodEventHandler Pod 事件处理器接口
type PodEventHandler interface {
	OnPodAdd(pod *coreV1.Pod) error
	OnPodUpdate(oldPod, newPod *coreV1.Pod) error
	OnPodDelete(pod *coreV1.Pod) error
}

// =============================================================================
// Filter Functions
// =============================================================================

// NodeFilter 节点过滤函数类型
type NodeFilter func(*coreV1.Node) bool

// ServiceFilter 服务过滤函数类型
type ServiceFilter func(*coreV1.Service) bool

// PodFilter Pod 过滤函数类型
type PodFilter func(*coreV1.Pod) bool

// 预定义的过滤函数
func FilterNodeByName(name string) NodeFilter {
	return func(node *coreV1.Node) bool {
		return node.Name == name
	}
}

func FilterNodeByLabel(key, value string) NodeFilter {
	return func(node *coreV1.Node) bool {
		if node.Labels == nil {
			return false
		}
		return node.Labels[key] == value
	}
}

func FilterNodeByCondition(conditionType string, status string) NodeFilter {
	return func(node *coreV1.Node) bool {
		for _, condition := range node.Status.Conditions {
			if string(condition.Type) == conditionType && string(condition.Status) == status {
				return true
			}
		}
		return false
	}
}
