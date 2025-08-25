package k8s

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	coreV1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
)

// =============================================================================
// Client Implementation
// =============================================================================

// ClientConfig 客户端配置
type ClientConfig struct {
	KubeconfigPath  string           `json:"kubeconfigPath,omitempty"`
	Timeout         time.Duration    `json:"timeout,omitempty"`
	ResyncPeriod    time.Duration    `json:"resyncPeriod,omitempty"`
	WorkQueueConfig *WorkQueueConfig `json:"workQueueConfig,omitempty"`
	// 基础权限设置 - 在 NewClient 时可以预设
	BasePermissions *PermissionStatus `json:"basePermissions,omitempty"`
}

// WorkQueueConfig 工作队列配置
type WorkQueueConfig struct {
	MaxRetries  int                   `json:"maxRetries,omitempty"`
	RateLimiter workqueue.RateLimiter `json:"rateLimiter,omitempty"`
}

// client Kubernetes 客户端实现
type client struct {
	config     *ClientConfig
	clientset  *kubernetes.Clientset
	restConfig *rest.Config

	// 权限状态
	permissions *PermissionStatus

	// Informers
	nodeInformer      cache.SharedIndexInformer
	serviceInformer   cache.SharedIndexInformer
	podInformer       cache.SharedIndexInformer
	configMapInformer cache.SharedIndexInformer

	// 工作队列
	nodeWorkQueue    workqueue.RateLimitingInterface
	serviceWorkQueue workqueue.RateLimitingInterface
	podWorkQueue     workqueue.RateLimitingInterface

	// 状态
	isConnected bool
	mu          sync.RWMutex
}

// NewClient 创建新的 Kubernetes 客户端
func NewClient(config *ClientConfig) Client {
	if config == nil {
		config = &ClientConfig{
			Timeout:      30 * time.Second,
			ResyncPeriod: 10 * time.Minute,
		}
	}

	if config.WorkQueueConfig == nil {
		config.WorkQueueConfig = &WorkQueueConfig{
			MaxRetries: 5,
		}
	}

	// 设置基础权限 - 默认只允许节点操作
	if config.BasePermissions == nil {
		config.BasePermissions = &PermissionStatus{
			CanListNodes:      true,  // 基础权限：允许列出节点
			CanGetNodes:       true,  // 基础权限：允许获取节点
			CanListServices:   false, // 高级权限：需要额外配置
			CanGetServices:    false, // 高级权限：需要额外配置
			CanListPods:       false, // 高级权限：需要额外配置
			CanGetPods:        false, // 高级权限：需要额外配置
			CanListConfigMaps: false, // 高级权限：需要额外配置
			CanGetConfigMaps:  false, // 高级权限：需要额外配置
		}
	}

	return &client{
		config: config,
	}
}

// Connect 连接到 Kubernetes 集群
func (c *client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isConnected {
		return nil
	}

	var err error
	var restConfig *rest.Config

	// 尝试从 kubeconfig 文件加载配置
	if c.config.KubeconfigPath != "" {
		restConfig, err = clientcmd.BuildConfigFromFlags("", c.config.KubeconfigPath)
	} else {
		// 尝试从集群内部加载配置
		restConfig, err = rest.InClusterConfig()
	}

	if err != nil {
		return fmt.Errorf("failed to load kubernetes config: %w", err)
	}

	// 设置超时
	if c.config.Timeout > 0 {
		restConfig.Timeout = c.config.Timeout
	}

	// 创建 clientset
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}

	c.clientset = clientset
	c.restConfig = restConfig
	c.isConnected = true

	// 使用预设的基础权限（深拷贝）
	c.permissions = &PermissionStatus{
		CanListNodes:      c.config.BasePermissions.CanListNodes,
		CanGetNodes:       c.config.BasePermissions.CanGetNodes,
		CanListServices:   c.config.BasePermissions.CanListServices,
		CanGetServices:    c.config.BasePermissions.CanGetServices,
		CanListPods:       c.config.BasePermissions.CanListPods,
		CanGetPods:        c.config.BasePermissions.CanGetPods,
		CanListConfigMaps: c.config.BasePermissions.CanListConfigMaps,
		CanGetConfigMaps:  c.config.BasePermissions.CanGetConfigMaps,
	}

	// 记录权限设置
	klog.Infof("Using permissions: Nodes=%t, Services=%t, Pods=%t, ConfigMaps=%t",
		c.permissions.CanListNodes,
		c.permissions.CanListServices,
		c.permissions.CanListPods,
		c.permissions.CanListConfigMaps)

	klog.Infof("Successfully connected to Kubernetes cluster")
	return nil
}

// checkPermissions 检查客户端权限
func (c *client) checkPermissions(ctx context.Context) error {
	if !c.isConnected {
		return fmt.Errorf("client not connected")
	}

	permissions := &PermissionStatus{}

	// 检查节点权限
	if _, err := c.clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		if isPermissionError(err) {
			klog.Warningf("No permission to list nodes: %v", err)
			permissions.CanListNodes = false
		} else {
			permissions.CanListNodes = true
		}
	} else {
		permissions.CanListNodes = true
	}

	if _, err := c.clientset.CoreV1().Nodes().Get(ctx, "test", metav1.GetOptions{}); err != nil {
		if isPermissionError(err) {
			klog.Warningf("No permission to get nodes: %v", err)
			permissions.CanGetNodes = false
		} else {
			permissions.CanGetNodes = true
		}
	} else {
		permissions.CanGetNodes = true
	}

	// 检查服务权限
	if _, err := c.clientset.CoreV1().Services("kube-system").List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		if isPermissionError(err) {
			klog.Warningf("No permission to list services: %v", err)
			permissions.CanListServices = false
		} else {
			permissions.CanListServices = true
		}
	} else {
		permissions.CanListServices = true
	}

	if _, err := c.clientset.CoreV1().Services("kube-system").Get(ctx, "kube-dns", metav1.GetOptions{}); err != nil {
		if isPermissionError(err) {
			klog.Warningf("No permission to get services: %v", err)
			permissions.CanGetServices = false
		} else {
			permissions.CanGetServices = true
		}
	} else {
		permissions.CanGetServices = true
	}

	// 检查 Pod 权限
	if _, err := c.clientset.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		if isPermissionError(err) {
			klog.Warningf("No permission to list pods: %v", err)
			permissions.CanListPods = false
		} else {
			permissions.CanListPods = true
		}
	} else {
		permissions.CanListPods = true
	}

	if _, err := c.clientset.CoreV1().Pods("kube-system").Get(ctx, "test", metav1.GetOptions{}); err != nil {
		if isPermissionError(err) {
			klog.Warningf("No permission to get pods: %v", err)
			permissions.CanGetPods = false
		} else {
			permissions.CanGetPods = true
		}
	} else {
		permissions.CanGetPods = true
	}

	// 检查 ConfigMap 权限
	if _, err := c.clientset.CoreV1().ConfigMaps("kube-system").List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		if isPermissionError(err) {
			klog.Warningf("No permission to list configmaps: %v", err)
			permissions.CanListConfigMaps = false
		} else {
			permissions.CanListConfigMaps = true
		}
	} else {
		permissions.CanListConfigMaps = true
	}

	if _, err := c.clientset.CoreV1().ConfigMaps("kube-system").Get(ctx, "kube-dns", metav1.GetOptions{}); err != nil {
		if isPermissionError(err) {
			klog.Warningf("No permission to get configmaps: %v", err)
			permissions.CanGetConfigMaps = false
		} else {
			permissions.CanGetConfigMaps = true
		}
	} else {
		permissions.CanGetConfigMaps = true
	}

	c.permissions = permissions
	klog.Infof("Permission check completed: %+v", permissions)
	return nil
}

// isPermissionError 检查是否为权限错误
func isPermissionError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	return strings.Contains(errStr, "Forbidden") ||
		strings.Contains(errStr, "Unauthorized") ||
		strings.Contains(errStr, "403") ||
		strings.Contains(errStr, "401")
}

// GetPermissions 获取权限状态
func (c *client) GetPermissions() *PermissionStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.permissions == nil {
		return &PermissionStatus{}
	}
	return c.permissions
}

// checkPermission 检查是否有指定权限，如果没有则抛出错误
func (c *client) checkPermission(permissionName string, hasPermission bool) error {
	if !hasPermission {
		return fmt.Errorf("permission denied: %s requires advanced permissions", permissionName)
	}
	return nil
}

// Disconnect 断开连接
func (c *client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isConnected {
		return nil
	}

	// 停止所有 informer
	// 注意：informer 没有 Stop 方法，通过 context 取消来停止

	// 关闭工作队列
	if c.nodeWorkQueue != nil {
		c.nodeWorkQueue.ShutDown()
	}
	if c.serviceWorkQueue != nil {
		c.serviceWorkQueue.ShutDown()
	}
	if c.podWorkQueue != nil {
		c.podWorkQueue.ShutDown()
	}

	c.isConnected = false
	klog.Infof("Disconnected from Kubernetes cluster")
	return nil
}

// StartInformers 启动所有 informer
func (c *client) StartInformers(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.isConnected {
		return fmt.Errorf("client not connected")
	}

	// 根据权限启动节点 informer（基础权限）
	if c.permissions != nil && c.permissions.CanListNodes {
		// 只为有权限的资源创建 informer
		c.nodeInformer = informers.NewSharedInformerFactory(c.clientset, c.config.ResyncPeriod).Core().V1().Nodes().Informer()
		go c.nodeInformer.Run(ctx.Done())
		klog.Infof("Started node informer (basic permission)")
	} else {
		klog.Warningf("Skipping node informer due to insufficient permissions")
	}

	// 根据权限启动服务 informer（高级权限）
	if c.permissions != nil && c.permissions.CanListServices {
		// 只为有权限的资源创建 informer
		c.serviceInformer = informers.NewSharedInformerFactory(c.clientset, c.config.ResyncPeriod).Core().V1().Services().Informer()
		go c.serviceInformer.Run(ctx.Done())
		klog.Infof("Started service informer (advanced permission)")
	} else {
		klog.Infof("Skipping service informer - requires advanced permissions")
	}

	// 根据权限启动 Pod informer（高级权限）
	if c.permissions != nil && c.permissions.CanListPods {
		// 只为有权限的资源创建 informer
		c.podInformer = informers.NewSharedInformerFactory(c.clientset, c.config.ResyncPeriod).Core().V1().Pods().Informer()
		go c.podInformer.Run(ctx.Done())
		klog.Infof("Started pod informer (advanced permission)")
	} else {
		klog.Infof("Skipping pod informer - requires advanced permissions")
	}

	// 根据权限启动 ConfigMap informer（高级权限）
	if c.permissions != nil && c.permissions.CanListConfigMaps {
		// 只为有权限的资源创建 informer
		c.configMapInformer = informers.NewSharedInformerFactory(c.clientset, c.config.ResyncPeriod).Core().V1().ConfigMaps().Informer()
		go c.configMapInformer.Run(ctx.Done())
		klog.Infof("Started configmap informer (advanced permission)")
	} else {
		klog.Infof("Skipping configmap informer - requires advanced permissions")
	}

	// 创建并启动工作队列（根据权限）
	if c.permissions != nil && c.permissions.CanListNodes {
		if c.config.WorkQueueConfig.RateLimiter != nil {
			c.nodeWorkQueue = workqueue.NewRateLimitingQueue(c.config.WorkQueueConfig.RateLimiter)
		} else {
			c.nodeWorkQueue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
		}
		klog.Infof("Created node work queue (basic permission)")
	}

	if c.permissions != nil && c.permissions.CanListServices {
		if c.config.WorkQueueConfig.RateLimiter != nil {
			c.serviceWorkQueue = workqueue.NewRateLimitingQueue(c.config.WorkQueueConfig.RateLimiter)
		} else {
			c.serviceWorkQueue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
		}
		klog.Infof("Created service work queue (advanced permission)")
	}

	if c.permissions != nil && c.permissions.CanListPods {
		if c.config.WorkQueueConfig.RateLimiter != nil {
			c.podWorkQueue = workqueue.NewRateLimitingQueue(c.config.WorkQueueConfig.RateLimiter)
		} else {
			c.podWorkQueue = workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
		}
		klog.Infof("Created pod work queue (advanced permission)")
	}

	klog.Infof("Started informers based on permissions")
	return nil
}

// WaitForCacheSync 等待缓存同步
func (c *client) WaitForCacheSync(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.isConnected {
		return fmt.Errorf("client not connected")
	}

	// 检查是否有任何 informer 被初始化
	if c.nodeInformer == nil && c.serviceInformer == nil && c.podInformer == nil && c.configMapInformer == nil {
		klog.Warningf("No informers initialized due to insufficient permissions")
		return nil
	}

	// 等待已初始化的 informer 同步
	var informers []cache.SharedIndexInformer
	if c.nodeInformer != nil {
		informers = append(informers, c.nodeInformer)
	}
	if c.serviceInformer != nil {
		informers = append(informers, c.serviceInformer)
	}
	if c.podInformer != nil {
		informers = append(informers, c.podInformer)
	}
	if c.configMapInformer != nil {
		informers = append(informers, c.configMapInformer)
	}

	if !cache.WaitForCacheSync(ctx.Done(), func() bool {
		for _, informer := range informers {
			if !informer.HasSynced() {
				return false
			}
		}
		return true
	}) {
		return fmt.Errorf("failed to sync informer cache")
	}

	return nil
}

// GetCurrentNodeName 获取当前节点名称
func (c *client) GetCurrentNodeName() (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.isConnected {
		return "", fmt.Errorf("client not connected")
	}

	// 从环境变量获取节点名称
	nodeName := os.Getenv("NODE_NAME")
	if nodeName != "" {
		return nodeName, nil
	}

	// 从主机名获取
	hostname, err := os.Hostname()
	if err != nil {
		return "", fmt.Errorf("failed to get hostname: %w", err)
	}

	return hostname, nil
}

// GetCurrentNode 获取当前节点信息
func (c *client) GetCurrentNode() (*coreV1.Node, error) {
	// 检查是否有权限获取节点
	if c.permissions != nil && !c.permissions.CanGetNodes {
		return nil, fmt.Errorf("no permission to get nodes")
	}

	nodeName, err := c.GetCurrentNodeName()
	if err != nil {
		return nil, err
	}

	return c.Nodes().Get(context.Background(), nodeName)
}

// Nodes 返回节点客户端
func (c *client) Nodes() NodeInterface {
	// 检查是否有节点操作权限
	if err := c.checkPermission("Nodes", c.permissions != nil && c.permissions.CanListNodes); err != nil {
		klog.Errorf("Permission denied for Nodes interface: %v", err)
		return nil
	}
	return &nodeClient{client: c}
}

// Services 返回服务客户端
func (c *client) Services() ServiceInterface {
	// 检查是否有服务操作权限
	if err := c.checkPermission("Services", c.permissions != nil && c.permissions.CanListServices); err != nil {
		klog.Errorf("Permission denied for Services interface: %v", err)
		return nil
	}
	return &serviceClient{client: c}
}

// Pods 返回 Pod 客户端
func (c *client) Pods() PodInterface {
	// 检查是否有 Pod 操作权限
	if err := c.checkPermission("Pods", c.permissions != nil && c.permissions.CanListPods); err != nil {
		klog.Errorf("Permission denied for Pods interface: %v", err)
		return nil
	}
	return &podClient{client: c}
}

// ConfigMaps 返回 ConfigMap 客户端
func (c *client) ConfigMaps() ConfigMapInterface {
	// 检查是否有 ConfigMap 操作权限
	if err := c.checkPermission("ConfigMaps", c.permissions != nil && c.permissions.CanListConfigMaps); err != nil {
		klog.Errorf("Permission denied for ConfigMaps interface: %v", err)
		return nil
	}
	return nil
}

// getClientset 获取 clientset（内部使用）
func (c *client) getClientset() *kubernetes.Clientset {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.isConnected {
		return nil
	}

	return c.clientset
}

// =============================================================================
// Client Implementations
// =============================================================================

// nodeClient 节点客户端实现
type nodeClient struct {
	client *client
}

func (nc *nodeClient) Get(ctx context.Context, name string) (*coreV1.Node, error) {
	clientset := nc.client.getClientset()
	if clientset == nil {
		return nil, fmt.Errorf("client not connected")
	}

	// 检查权限
	if nc.client.permissions != nil && !nc.client.permissions.CanGetNodes {
		return nil, fmt.Errorf("no permission to get nodes")
	}

	// 获取节点
	node, err := clientset.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get node %s: %w", name, err)
	}

	return node, nil
}

func (nc *nodeClient) List(ctx context.Context, opts *ListOptions) ([]*coreV1.Node, error) {
	clientset := nc.client.getClientset()
	if clientset == nil {
		return nil, fmt.Errorf("client not connected")
	}

	// 检查权限
	if nc.client.permissions != nil && !nc.client.permissions.CanListNodes {
		return nil, fmt.Errorf("no permission to list nodes")
	}

	// 构建列表选项
	listOpts := metav1.ListOptions{}
	if opts != nil {
		listOpts.LabelSelector = opts.LabelSelector
		listOpts.FieldSelector = opts.FieldSelector
		listOpts.Limit = opts.Limit
		listOpts.Continue = opts.Continue
		listOpts.ResourceVersion = opts.ResourceVersion
		listOpts.ResourceVersionMatch = metav1.ResourceVersionMatch(opts.ResourceVersionMatch)
		if opts.Watch {
			listOpts.Watch = true
		}
	}

	// 获取原始节点列表
	k8sNodes, err := clientset.CoreV1().Nodes().List(ctx, listOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	// 转换为指针数组
	nodes := make([]*coreV1.Node, len(k8sNodes.Items))
	for i := range k8sNodes.Items {
		nodes[i] = &k8sNodes.Items[i]
	}

	return nodes, nil
}

func (nc *nodeClient) Create(ctx context.Context, node *coreV1.Node) (*coreV1.Node, error) {
	clientset := nc.client.getClientset()
	if clientset == nil {
		return nil, fmt.Errorf("client not connected")
	}

	// 创建节点
	createdNode, err := clientset.CoreV1().Nodes().Create(ctx, node, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create node: %w", err)
	}

	return createdNode, nil
}

func (nc *nodeClient) Update(ctx context.Context, node *coreV1.Node) (*coreV1.Node, error) {
	clientset := nc.client.getClientset()
	if clientset == nil {
		return nil, fmt.Errorf("client not connected")
	}

	// 更新节点
	updatedNode, err := clientset.CoreV1().Nodes().Update(ctx, node, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to update node: %w", err)
	}

	return updatedNode, nil
}

func (nc *nodeClient) Delete(ctx context.Context, name string) error {
	clientset := nc.client.getClientset()
	if clientset == nil {
		return fmt.Errorf("client not connected")
	}

	// 删除节点
	err := clientset.CoreV1().Nodes().Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete node %s: %w", name, err)
	}

	return nil
}

func (nc *nodeClient) Patch(ctx context.Context, name string, pt types.PatchType, data []byte) (*coreV1.Node, error) {
	clientset := nc.client.getClientset()
	if clientset == nil {
		return nil, fmt.Errorf("client not connected")
	}

	// 补丁节点
	patchedNode, err := clientset.CoreV1().Nodes().Patch(ctx, name, pt, data, metav1.PatchOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to patch node %s: %w", name, err)
	}

	return patchedNode, nil
}

func (nc *nodeClient) GetPodCIDR(name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	node, err := nc.Get(ctx, name)
	if err != nil {
		return "", err
	}

	if node.Spec.PodCIDR != "" {
		return node.Spec.PodCIDR, nil
	}

	if len(node.Spec.PodCIDRs) > 0 {
		return node.Spec.PodCIDRs[0], nil
	}

	return "", fmt.Errorf("no Pod CIDR found for node %s", name)
}

func (nc *nodeClient) GetAllPodCIDRs() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	nodes, err := nc.List(ctx, nil)
	if err != nil {
		return nil, err
	}

	var podCIDRs []string
	for _, node := range nodes {
		if node.Spec.PodCIDR != "" {
			podCIDRs = append(podCIDRs, node.Spec.PodCIDR)
		}
		if len(node.Spec.PodCIDRs) > 0 {
			podCIDRs = append(podCIDRs, node.Spec.PodCIDRs...)
		}
	}

	return podCIDRs, nil
}

func (nc *nodeClient) UpdateAnnotations(name string, annotations map[string]string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 获取当前节点
	node, err := nc.Get(ctx, name)
	if err != nil {
		return err
	}

	// 更新注解
	if node.Annotations == nil {
		node.Annotations = make(map[string]string)
	}
	for k, v := range annotations {
		node.Annotations[k] = v
	}

	// 保存更新
	_, err = nc.Update(ctx, node)
	return err
}

func (nc *nodeClient) UpdateLabels(name string, labels map[string]string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 获取当前节点
	node, err := nc.Get(ctx, name)
	if err != nil {
		return err
	}

	// 更新标签
	if node.Labels == nil {
		node.Labels = make(map[string]string)
	}
	for k, v := range labels {
		node.Labels[k] = v
	}

	// 保存更新
	_, err = nc.Update(ctx, node)
	return err
}

// serviceClient 服务客户端实现
type serviceClient struct {
	client *client
}

func (sc *serviceClient) Get(ctx context.Context, namespace, name string) (*coreV1.Service, error) {
	clientset := sc.client.getClientset()
	if clientset == nil {
		return nil, fmt.Errorf("client not connected")
	}

	// 检查权限
	if sc.client.permissions != nil && !sc.client.permissions.CanGetServices {
		return nil, fmt.Errorf("no permission to get services")
	}

	// 获取原始服务
	k8sService, err := clientset.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get service %s/%s: %w", namespace, name, err)
	}

	return k8sService, nil
}

func (sc *serviceClient) List(ctx context.Context, namespace string, opts *ListOptions) ([]*coreV1.Service, error) {
	clientset := sc.client.getClientset()
	if clientset == nil {
		return nil, fmt.Errorf("client not connected")
	}

	// 检查权限
	if sc.client.permissions != nil && !sc.client.permissions.CanListServices {
		return nil, fmt.Errorf("no permission to list services")
	}

	// 构建列表选项
	listOpts := metav1.ListOptions{}
	if opts != nil {
		listOpts.LabelSelector = opts.LabelSelector
		listOpts.FieldSelector = opts.FieldSelector
		listOpts.Limit = opts.Limit
		listOpts.Continue = opts.Continue
		listOpts.ResourceVersion = opts.ResourceVersion
		listOpts.ResourceVersionMatch = metav1.ResourceVersionMatch(opts.ResourceVersionMatch)
		if opts.Watch {
			listOpts.Watch = true
		}
	}

	// 获取原始服务列表
	k8sServices, err := clientset.CoreV1().Services(namespace).List(ctx, listOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list services in namespace %s: %w", namespace, err)
	}

	// 转换为指针数组
	services := make([]*coreV1.Service, len(k8sServices.Items))
	for i := range k8sServices.Items {
		services[i] = &k8sServices.Items[i]
	}

	return services, nil
}

func (sc *serviceClient) Create(ctx context.Context, namespace string, service *coreV1.Service) (*coreV1.Service, error) {
	clientset := sc.client.getClientset()
	if clientset == nil {
		return nil, fmt.Errorf("client not connected")
	}

	// 创建服务
	createdService, err := clientset.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create service: %w", err)
	}

	return createdService, nil
}

func (sc *serviceClient) Update(ctx context.Context, namespace string, service *coreV1.Service) (*coreV1.Service, error) {
	clientset := sc.client.getClientset()
	if clientset == nil {
		return nil, fmt.Errorf("client not connected")
	}

	// 更新服务
	updatedService, err := clientset.CoreV1().Services(namespace).Update(ctx, service, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to update service: %w", err)
	}

	return updatedService, nil
}

func (sc *serviceClient) Delete(ctx context.Context, namespace, name string) error {
	clientset := sc.client.getClientset()
	if clientset == nil {
		return fmt.Errorf("client not connected")
	}

	// 删除服务
	err := clientset.CoreV1().Services(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete service %s/%s: %w", namespace, name, err)
	}

	return nil
}

func (sc *serviceClient) GetClusterIP(namespace, name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	service, err := sc.Get(ctx, namespace, name)
	if err != nil {
		return "", err
	}

	if service.Spec.ClusterIP == "" {
		return "", fmt.Errorf("service %s/%s has no ClusterIP", namespace, name)
	}

	return service.Spec.ClusterIP, nil
}

func (sc *serviceClient) GetLoadBalancerIP(namespace, name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	service, err := sc.Get(ctx, namespace, name)
	if err != nil {
		return "", err
	}

	if len(service.Status.LoadBalancer.Ingress) == 0 {
		return "", fmt.Errorf("service %s/%s has no LoadBalancer ingress", namespace, name)
	}

	// 优先返回 IP，如果没有则返回主机名
	ingress := service.Status.LoadBalancer.Ingress[0]
	if ingress.IP != "" {
		return ingress.IP, nil
	}
	if ingress.Hostname != "" {
		return ingress.Hostname, nil
	}

	return "", fmt.Errorf("service %s/%s LoadBalancer ingress has no IP or hostname", namespace, name)
}

func (sc *serviceClient) UpdateAnnotations(namespace, name string, annotations map[string]string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 获取当前服务
	service, err := sc.Get(ctx, namespace, name)
	if err != nil {
		return err
	}

	// 更新注解
	if service.Annotations == nil {
		service.Annotations = make(map[string]string)
	}
	for k, v := range annotations {
		service.Annotations[k] = v
	}

	// 保存更新
	_, err = sc.Update(ctx, namespace, service)
	return err
}

func (sc *serviceClient) UpdateLabels(namespace, name string, labels map[string]string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 获取当前服务
	service, err := sc.Get(ctx, namespace, name)
	if err != nil {
		return err
	}

	// 更新标签
	if service.Labels == nil {
		service.Labels = make(map[string]string)
	}
	for k, v := range labels {
		service.Labels[k] = v
	}

	// 保存更新
	_, err = sc.Update(ctx, namespace, service)
	return err
}

func (sc *serviceClient) GetEndpoints(namespace, name string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientset := sc.client.getClientset()
	if clientset == nil {
		return nil, fmt.Errorf("client not connected")
	}

	// 获取端点
	endpoints, err := clientset.CoreV1().Endpoints(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get endpoints for service %s/%s: %w", namespace, name, err)
	}

	var addresses []string
	for _, subset := range endpoints.Subsets {
		for _, address := range subset.Addresses {
			if address.IP != "" {
				addresses = append(addresses, address.IP)
			}
		}
	}

	return addresses, nil
}

func (sc *serviceClient) GetPorts(namespace, name string) ([]coreV1.ServicePort, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	service, err := sc.Get(ctx, namespace, name)
	if err != nil {
		return nil, err
	}

	return service.Spec.Ports, nil
}

// podClient Pod 客户端实现
type podClient struct {
	client *client
}

func (pc *podClient) Get(ctx context.Context, namespace, name string) (*coreV1.Pod, error) {
	clientset := pc.client.getClientset()
	if clientset == nil {
		return nil, fmt.Errorf("client not connected")
	}

	// 检查权限
	if pc.client.permissions != nil && !pc.client.permissions.CanGetPods {
		return nil, fmt.Errorf("no permission to get pods")
	}

	// 获取 Pod
	k8sPod, err := clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod %s/%s: %w", namespace, name, err)
	}

	return k8sPod, nil
}

func (pc *podClient) List(ctx context.Context, namespace string, opts *ListOptions) ([]*coreV1.Pod, error) {
	clientset := pc.client.getClientset()
	if clientset == nil {
		return nil, fmt.Errorf("client not connected")
	}

	// 检查权限
	if pc.client.permissions != nil && !pc.client.permissions.CanListPods {
		return nil, fmt.Errorf("no permission to list pods")
	}

	// 构建列表选项
	listOpts := metav1.ListOptions{}
	if opts != nil {
		listOpts.LabelSelector = opts.LabelSelector
		listOpts.FieldSelector = opts.FieldSelector
		listOpts.Limit = opts.Limit
		listOpts.Continue = opts.Continue
		listOpts.ResourceVersion = opts.ResourceVersion
		listOpts.ResourceVersionMatch = metav1.ResourceVersionMatch(opts.ResourceVersionMatch)
		if opts.Watch {
			listOpts.Watch = true
		}
	}

	// 获取 Pod 列表
	k8sPods, err := clientset.CoreV1().Pods(namespace).List(ctx, listOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods in namespace %s: %w", namespace, err)
	}

	// 转换为指针数组
	pods := make([]*coreV1.Pod, len(k8sPods.Items))
	for i := range k8sPods.Items {
		pods[i] = &k8sPods.Items[i]
	}

	return pods, nil
}

func (pc *podClient) Create(ctx context.Context, namespace string, pod *coreV1.Pod) (*coreV1.Pod, error) {
	clientset := pc.client.getClientset()
	if clientset == nil {
		return nil, fmt.Errorf("client not connected")
	}

	// 创建 Pod
	createdPod, err := clientset.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create pod: %w", err)
	}

	return createdPod, nil
}

func (pc *podClient) Update(ctx context.Context, namespace string, pod *coreV1.Pod) (*coreV1.Pod, error) {
	clientset := pc.client.getClientset()
	if clientset == nil {
		return nil, fmt.Errorf("client not connected")
	}

	// 更新 Pod
	updatedPod, err := clientset.CoreV1().Pods(namespace).Update(ctx, pod, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to update pod: %w", err)
	}

	return updatedPod, nil
}

func (pc *podClient) Delete(ctx context.Context, namespace, name string) error {
	clientset := pc.client.getClientset()
	if clientset == nil {
		return fmt.Errorf("client not connected")
	}

	// 删除 Pod
	err := clientset.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete pod %s/%s: %w", namespace, name, err)
	}

	return nil
}

func (pc *podClient) GetByNode(nodeName string) ([]*coreV1.Pod, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 从所有命名空间获取
	clientset := pc.client.getClientset()
	if clientset == nil {
		return nil, fmt.Errorf("client not connected")
	}

	// 获取所有命名空间的 Pod
	listOpts := metav1.ListOptions{
		FieldSelector: fmt.Sprintf("spec.nodeName=%s", nodeName),
	}

	k8sPods, err := clientset.CoreV1().Pods("").List(ctx, listOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list pods on node %s: %w", nodeName, err)
	}

	// 转换为指针数组
	pods := make([]*coreV1.Pod, len(k8sPods.Items))
	for i := range k8sPods.Items {
		pods[i] = &k8sPods.Items[i]
	}

	return pods, nil
}

func (pc *podClient) GetByLabel(namespace, labelKey, labelValue string) ([]*coreV1.Pod, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 使用标签选择器获取 Pod
	opts := &ListOptions{
		LabelSelector: fmt.Sprintf("%s=%s", labelKey, labelValue),
	}

	return pc.List(ctx, namespace, opts)
}

func (pc *podClient) GetPodIP(namespace, name string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pod, err := pc.Get(ctx, namespace, name)
	if err != nil {
		return "", err
	}

	if pod.Status.PodIP == "" {
		return "", fmt.Errorf("pod %s/%s has no PodIP", namespace, name)
	}

	return pod.Status.PodIP, nil
}

// configMapClient ConfigMap 客户端实现
type configMapClient struct {
	client *client
}

func (cmc *configMapClient) Get(ctx context.Context, namespace, name string) (*coreV1.ConfigMap, error) {
	clientset := cmc.client.getClientset()
	if clientset == nil {
		return nil, fmt.Errorf("client not connected")
	}

	// 检查权限
	if cmc.client.permissions != nil && !cmc.client.permissions.CanGetConfigMaps {
		return nil, fmt.Errorf("no permission to get configmaps")
	}

	// 获取 ConfigMap
	k8sConfigMap, err := clientset.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get configmap %s/%s: %w", namespace, name, err)
	}

	return k8sConfigMap, nil
}

func (cmc *configMapClient) List(ctx context.Context, namespace string, opts *ListOptions) ([]*coreV1.ConfigMap, error) {
	clientset := cmc.client.getClientset()
	if clientset == nil {
		return nil, fmt.Errorf("client not connected")
	}

	// 检查权限
	if cmc.client.permissions != nil && !cmc.client.permissions.CanListConfigMaps {
		return nil, fmt.Errorf("no permission to list configmaps")
	}

	// 构建列表选项
	listOpts := metav1.ListOptions{}
	if opts != nil {
		listOpts.LabelSelector = opts.LabelSelector
		listOpts.FieldSelector = opts.FieldSelector
		listOpts.Limit = opts.Limit
		listOpts.Continue = opts.Continue
		listOpts.ResourceVersion = opts.ResourceVersion
		listOpts.ResourceVersionMatch = metav1.ResourceVersionMatch(opts.ResourceVersionMatch)
		if opts.Watch {
			listOpts.Watch = true
		}
	}

	// 获取原始 ConfigMap 列表
	k8sConfigMaps, err := clientset.CoreV1().ConfigMaps(namespace).List(ctx, listOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list configmaps in namespace %s: %w", namespace, err)
	}

	// 转换为指针数组
	configMaps := make([]*coreV1.ConfigMap, len(k8sConfigMaps.Items))
	for i := range k8sConfigMaps.Items {
		configMaps[i] = &k8sConfigMaps.Items[i]
	}

	return configMaps, nil
}

func (cmc *configMapClient) Create(ctx context.Context, namespace string, configMap *coreV1.ConfigMap) (*coreV1.ConfigMap, error) {
	clientset := cmc.client.getClientset()
	if clientset == nil {
		return nil, fmt.Errorf("client not connected")
	}

	// 创建 ConfigMap
	createdConfigMap, err := clientset.CoreV1().ConfigMaps(namespace).Create(ctx, configMap, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create configmap: %w", err)
	}

	return createdConfigMap, nil
}

func (cmc *configMapClient) Update(ctx context.Context, namespace string, configMap *coreV1.ConfigMap) (*coreV1.ConfigMap, error) {
	clientset := cmc.client.getClientset()
	if clientset == nil {
		return nil, fmt.Errorf("client not connected")
	}

	// 更新 ConfigMap
	updatedConfigMap, err := clientset.CoreV1().ConfigMaps(namespace).Update(ctx, configMap, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to update configmap: %w", err)
	}

	return updatedConfigMap, nil
}

func (cmc *configMapClient) Delete(ctx context.Context, namespace, name string) error {
	clientset := cmc.client.getClientset()
	if clientset == nil {
		return fmt.Errorf("client not connected")
	}

	// 删除 ConfigMap
	err := clientset.CoreV1().ConfigMaps(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete configmap %s/%s: %w", namespace, name, err)
	}

	return nil
}

func (cmc *configMapClient) GetData(namespace, name, key string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	configMap, err := cmc.Get(ctx, namespace, name)
	if err != nil {
		return "", err
	}

	value, exists := configMap.Data[key]
	if !exists {
		return "", fmt.Errorf("key %s not found in configmap %s/%s", key, namespace, name)
	}

	return value, nil
}

func (cmc *configMapClient) UpdateData(namespace, name string, data map[string]string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 获取当前 ConfigMap
	configMap, err := cmc.Get(ctx, namespace, name)
	if err != nil {
		return err
	}

	// 更新数据
	if configMap.Data == nil {
		configMap.Data = make(map[string]string)
	}
	for k, v := range data {
		configMap.Data[k] = v
	}

	// 保存更新
	_, err = cmc.Update(ctx, namespace, configMap)
	return err
}

// GetDNSServiceIP 获取 DNS 服务 IP
func (c *client) GetDNSServiceIP() (string, error) {
	if !c.isConnected {
		return "", fmt.Errorf("client not connected")
	}

	clientset := c.getClientset()
	if clientset == nil {
		return "", fmt.Errorf("client not connected")
	}

	// 检查是否有权限获取服务
	if c.permissions != nil && !c.permissions.CanGetServices {
		klog.Warningf("No permission to get services, returning default DNS IP")
		return "10.96.0.10", nil
	}

	// 定义可能的 DNS 服务名称（按优先级排序）
	dnsServiceNames := []string{
		"kube-dns",                     // 标准 Kubernetes
		"coredns",                      // CoreDNS
		"rke2-coredns-rke2-coredns",    // RKE2
		"k3s-coredns",                  // k3s
		"rancher-coredns",              // Rancher
		"aws-node-termination-handler", // EKS
	}

	// 尝试获取 DNS 服务
	for _, serviceName := range dnsServiceNames {
		if dnsService, err := clientset.CoreV1().Services("kube-system").Get(context.Background(), serviceName, metav1.GetOptions{}); err == nil {
			// 检查服务是否有正确的标签或注解标识为 DNS 服务
			if isDNSService(dnsService) {
				if len(dnsService.Spec.ClusterIPs) > 0 {
					return dnsService.Spec.ClusterIPs[0], nil
				} else if dnsService.Spec.ClusterIP != "" {
					return dnsService.Spec.ClusterIP, nil
				}
			}
		}
	}

	// 如果通过服务名找不到，尝试通过标签选择器查找
	if dnsService, err := findDNSServiceBySelector(clientset); err == nil && dnsService != nil {
		if len(dnsService.Spec.ClusterIPs) > 0 {
			return dnsService.Spec.ClusterIPs[0], nil
		} else if dnsService.Spec.ClusterIP != "" {
			return dnsService.Spec.ClusterIP, nil
		}
	}

	// 如果都找不到，返回默认值
	return "10.96.0.10", nil
}

// isDNSService 检查服务是否为 DNS 服务
func isDNSService(service *coreV1.Service) bool {
	// 检查标签
	if service.Labels != nil {
		if _, hasDNS := service.Labels["k8s-app"]; hasDNS {
			if service.Labels["k8s-app"] == "kube-dns" || service.Labels["k8s-app"] == "coredns" {
				return true
			}
		}
		if _, hasDNS := service.Labels["app"]; hasDNS {
			if service.Labels["app"] == "coredns" || service.Labels["app"] == "kube-dns" {
				return true
			}
		}
	}

	// 检查注解
	if service.Annotations != nil {
		if _, hasDNS := service.Annotations["service.kubernetes.io/name"]; hasDNS {
			if strings.Contains(service.Annotations["service.kubernetes.io/name"], "dns") {
				return true
			}
		}
	}

	// 检查端口
	for _, port := range service.Spec.Ports {
		if port.Port == 53 || port.Name == "dns" || port.Name == "dns-tcp" {
			return true
		}
	}

	return false
}

// findDNSServiceBySelector 通过标签选择器查找 DNS 服务
func findDNSServiceBySelector(clientset *kubernetes.Clientset) (*coreV1.Service, error) {
	// 尝试通过标签选择器查找
	selector := "k8s-app in (kube-dns,coredns)"
	services, err := clientset.CoreV1().Services("kube-system").List(context.Background(), metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, err
	}

	for _, service := range services.Items {
		if isDNSService(&service) {
			return &service, nil
		}
	}

	return nil, fmt.Errorf("no DNS service found")
}

// GetClusterDomain 获取集群域名
func (c *client) GetClusterDomain() (string, error) {
	if !c.isConnected {
		return "", fmt.Errorf("client not connected")
	}

	clientset := c.getClientset()
	if clientset == nil {
		return "", fmt.Errorf("client not connected")
	}

	// 检查是否有权限获取 ConfigMap
	if c.permissions != nil && !c.permissions.CanGetConfigMaps {
		klog.Warningf("No permission to get configmaps, returning default cluster domain")
		return "cluster.local", nil
	}

	// 定义可能的 ConfigMap 名称和键
	configMapSources := []struct {
		name string
		key  string
	}{
		{"cluster-info", "cluster-domain"},
		{"kube-dns", "stubDomains"},
		{"coredns", "Corefile"},
		{"k3s-coredns", "Corefile"},
		{"rke2-coredns", "Corefile"},
	}

	// 尝试从不同的 ConfigMap 获取集群域名
	for _, source := range configMapSources {
		if cm, err := clientset.CoreV1().ConfigMaps("kube-system").Get(context.Background(), source.name, metav1.GetOptions{}); err == nil {
			if domain, exists := cm.Data[source.key]; exists {
				// 对于 Corefile，需要解析内容
				if source.key == "Corefile" {
					if parsedDomain := parseCorefileDomain(domain); parsedDomain != "" {
						return parsedDomain, nil
					}
				} else {
					return domain, nil
				}
			}
		}
	}

	// 尝试从 kubelet 配置获取
	if kubeletConfig, err := clientset.CoreV1().ConfigMaps("kube-system").Get(context.Background(), "kubelet-config", metav1.GetOptions{}); err == nil {
		if config, exists := kubeletConfig.Data["kubelet"]; exists {
			if domain := parseKubeletConfigDomain(config); domain != "" {
				return domain, nil
			}
		}
	}

	// 如果找不到，返回默认值
	return "cluster.local", nil
}

// parseCorefileDomain 从 Corefile 解析集群域名
func parseCorefileDomain(corefile string) string {
	lines := strings.Split(corefile, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "kubernetes") {
			// 查找 cluster.local 或类似的域名
			parts := strings.Fields(line)
			for _, part := range parts {
				if strings.Contains(part, ".") && !strings.HasPrefix(part, "in-addr.arpa") {
					return part
				}
			}
		}
	}
	return ""
}

// parseKubeletConfigDomain 从 kubelet 配置解析集群域名
func parseKubeletConfigDomain(config string) string {
	lines := strings.Split(config, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "clusterDomain:") {
			parts := strings.Split(line, ":")
			if len(parts) > 1 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}
