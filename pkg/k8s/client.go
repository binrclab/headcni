package k8s

import (
	"context"
	"os"
	"sync"

	"github.com/pkg/errors"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listersCoreV1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

// Client Kubernetes 客户端包装器
type Client struct {
	clientset    *kubernetes.Clientset
	factory      informers.SharedInformerFactory
	nodeInformer cache.SharedIndexInformer
	nodeLister   listersCoreV1.NodeLister

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.RWMutex
}

// NewClient 创建新的 Kubernetes 客户端
func NewClient() *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		ctx:    ctx,
		cancel: cancel,
	}
}

// Connect 连接到 Kubernetes 集群
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 构建配置
	cfg, err := clientcmd.BuildConfigFromFlags("", "")
	if err != nil {
		return errors.Wrap(err, "failed to build config")
	}

	// 创建客户端
	c.clientset, err = kubernetes.NewForConfig(cfg)
	if err != nil {
		return errors.Wrap(err, "failed to create clientset")
	}

	// 创建 informer factory
	c.factory = informers.NewSharedInformerFactory(c.clientset, 0)

	klog.Info("Kubernetes client connected successfully")
	return nil
}

// StartNodeInformer 启动节点 informer
func (c *Client) StartNodeInformer() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.factory == nil {
		return errors.New("client not connected, call Connect() first")
	}

	c.nodeInformer = c.factory.Core().V1().Nodes().Informer()
	c.nodeLister = c.factory.Core().V1().Nodes().Lister()

	// 启动 informer
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.nodeInformer.Run(c.ctx.Done())
	}()

	// 等待缓存同步
	if !cache.WaitForCacheSync(c.ctx.Done(), c.nodeInformer.HasSynced) {
		return errors.New("failed to sync node informer cache")
	}

	klog.Info("Node informer started successfully")
	return nil
}

// Stop 停止客户端
func (c *Client) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	c.wg.Wait()
	klog.Info("Kubernetes client stopped")
}

// GetClientset 获取原始客户端
func (c *Client) GetClientset() *kubernetes.Clientset {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.clientset
}

// GetNodeInformer 获取节点 informer
func (c *Client) GetNodeInformer() cache.SharedIndexInformer {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.nodeInformer
}

// GetNodeLister 获取节点 lister
func (c *Client) GetNodeLister() listersCoreV1.NodeLister {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.nodeLister
}

// IsReady 检查客户端是否就绪
func (c *Client) IsReady() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.clientset != nil && c.nodeInformer != nil && c.nodeInformer.HasSynced()
}

// GetCurrentNodeName 获取当前节点名称
func (c *Client) GetCurrentNodeName() (string, error) {
	nodeName := os.Getenv("NODE_NAME")
	if nodeName != "" {
		return nodeName, nil
	}

	podName := os.Getenv("POD_NAME")
	podNamespace := os.Getenv("POD_NAMESPACE")
	if podName == "" || podNamespace == "" {
		return "", errors.New("POD_NAME and POD_NAMESPACE environment variables must be set")
	}

	c.mu.RLock()
	clientset := c.clientset
	c.mu.RUnlock()

	if clientset == nil {
		return "", errors.New("client not connected")
	}

	pod, err := clientset.CoreV1().Pods(podNamespace).Get(context.TODO(), podName, metaV1.GetOptions{})
	if err != nil {
		return "", errors.Wrapf(err, "failed to get pod %s/%s", podNamespace, podName)
	}

	if pod.Spec.NodeName == "" {
		return "", errors.Errorf("node name not present in pod spec %s/%s", podNamespace, podName)
	}

	return pod.Spec.NodeName, nil
}

// 全局客户端实例（向后兼容）
var globalClient *Client
var globalOnce sync.Once

// InitGlobalClient 初始化全局客户端
func InitGlobalClient() error {
	var err error
	globalOnce.Do(func() {
		globalClient = NewClient()
		err = globalClient.Connect()
		if err != nil {
			return
		}
		err = globalClient.StartNodeInformer()
	})
	return err
}

// GetGlobalClient 获取全局客户端
func GetGlobalClient() *Client {
	return globalClient
}

// StopGlobalClient 停止全局客户端
func StopGlobalClient() {
	if globalClient != nil {
		globalClient.Stop()
	}
}
