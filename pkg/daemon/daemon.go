package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/binrclab/headcni/pkg/headscale"
	"github.com/binrclab/headcni/pkg/logging"
	"github.com/binrclab/headcni/pkg/monitoring"
	"github.com/binrclab/headcni/pkg/networking"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
)

// Daemon 是 HeadCNI 守护进程
type Daemon struct {
	config     *Config
	headscale  *headscale.Client
	networkMgr *networking.NetworkManager
	monitor    *monitoring.Server
	logger     logging.Logger

	// Kubernetes 相关
	k8sClient   kubernetes.Interface
	podInformer cache.SharedIndexInformer
	podQueue    workqueue.RateLimitingInterface

	// 状态管理
	networkState *NetworkState
	stateMutex   sync.RWMutex

	// CNI 通信
	socketPath string
	socketMux  sync.RWMutex

	// 控制
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Config 是守护进程配置
type Config struct {
	HeadscaleURL       string
	HeadscaleAuthKey   string
	PodCIDR            string
	ServiceCIDR        string
	MTU                int
	IPAMType           string
	AllocationStrategy string
	MetricsPort        int
	MetricsPath        string
	Mode               string
	InterfaceName      string
	K8sClient          kubernetes.Interface
	Logger             logging.Logger
}

// NetworkState 是网络状态
type NetworkState struct {
	Nodes  map[string]*NodeInfo
	Routes map[string]*RouteInfo
	Pods   map[string]*PodInfo
}

// NodeInfo 是节点信息
type NodeInfo struct {
	ID       string
	Name     string
	NodeKey  string
	IP       net.IP
	Online   bool
	Created  time.Time
	LastSeen time.Time
}

// RouteInfo 是路由信息
type RouteInfo struct {
	ID      string
	NodeID  string
	Prefix  string
	Enabled bool
	Created time.Time
}

// PodInfo 是 Pod 信息
type PodInfo struct {
	Name      string
	Namespace string
	IP        net.IP
	NodeKey   string
	RouteID   string
	Created   time.Time
}

// CNIRequest 是 CNI 请求
type CNIRequest struct {
	Type        string `json:"type"` // "allocate", "release", "status"
	Namespace   string `json:"namespace"`
	PodName     string `json:"pod_name"`
	ContainerID string `json:"container_id"`
	PodIP       string `json:"pod_ip,omitempty"`
}

// CNIResponse 是 CNI 响应
type CNIResponse struct {
	Success bool        `json:"success"`
	Error   string      `json:"error,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

// New 创建新的守护进程
func New(config *Config) (*Daemon, error) {
	// 创建 Headscale 客户端
	headscaleClient, err := headscale.NewClient(config.HeadscaleURL, config.HeadscaleAuthKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create headscale client: %v", err)
	}

	// 创建网络管理器
	networkMgr, err := networking.NewNetworkManager(&networking.Config{
		TailscaleSocket: "/var/run/tailscale/tailscaled.sock",
		MTU:             config.MTU,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create network manager: %v", err)
	}

	// 创建监控服务器
	monitor := monitoring.NewServer(config.MetricsPort, config.MetricsPath)

	// 创建 Pod Informer
	podInformer := cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return config.K8sClient.CoreV1().Pods("").List(context.Background(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return config.K8sClient.CoreV1().Pods("").Watch(context.Background(), options)
			},
		},
		&corev1.Pod{},
		0,
		cache.Indexers{},
	)

	// 创建队列
	podQueue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())

	// 设置 socket 路径
	socketPath := "/var/run/headcni/daemon.sock"

	d := &Daemon{
		config:      config,
		headscale:   headscaleClient,
		networkMgr:  networkMgr,
		monitor:     monitor,
		logger:      config.Logger,
		k8sClient:   config.K8sClient,
		podInformer: podInformer,
		podQueue:    podQueue,
		socketPath:  socketPath,
		networkState: &NetworkState{
			Nodes:  make(map[string]*NodeInfo),
			Routes: make(map[string]*RouteInfo),
			Pods:   make(map[string]*PodInfo),
		},
	}

	// 设置事件处理器
	podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    d.handlePodEvent,
		UpdateFunc: d.handlePodChanged,
		DeleteFunc: d.handlePodDeleted,
	})

	return d, nil
}

// Start 启动守护进程
func (d *Daemon) Start() error {
	d.ctx, d.cancel = context.WithCancel(context.Background())

	d.logger.Info("Starting HeadCNI daemon")

	// 启动监控服务器
	if err := d.monitor.Start(d.ctx); err != nil {
		return fmt.Errorf("failed to start monitoring server: %v", err)
	}

	// 启动 CNI 通信服务器
	if err := d.startCNIServer(); err != nil {
		return fmt.Errorf("failed to start CNI server: %v", err)
	}

	// 启动 Pod Informer
	go d.podInformer.Run(d.ctx.Done())

	// 等待 Informer 同步
	if !cache.WaitForCacheSync(d.ctx.Done(), d.podInformer.HasSynced) {
		return fmt.Errorf("failed to sync pod informer")
	}

	// 启动工作协程
	d.wg.Add(4)
	go d.processPods()
	go d.manageNetworkState()
	go d.maintainHeadscaleConnection()
	go d.manageTailscaleInterface()

	d.logger.Info("HeadCNI daemon started successfully")
	return nil
}

// Stop 停止守护进程
func (d *Daemon) Stop() error {
	d.logger.Info("Stopping HeadCNI daemon")

	// 停止所有协程
	d.cancel()

	// 等待所有协程结束
	d.wg.Wait()

	// 停止监控服务器
	if err := d.monitor.Stop(d.ctx); err != nil {
		d.logger.Error("Failed to stop monitoring server", "error", err)
	}

	// 停止 CNI 服务器
	if err := d.stopCNIServer(); err != nil {
		d.logger.Error("Failed to stop CNI server", "error", err)
	}

	d.logger.Info("HeadCNI daemon stopped")
	return nil
}

// startCNIServer 启动 CNI 通信服务器
func (d *Daemon) startCNIServer() error {
	// 创建 socket 目录
	if err := os.MkdirAll(filepath.Dir(d.socketPath), 0755); err != nil {
		return fmt.Errorf("failed to create socket directory: %v", err)
	}

	// 删除已存在的 socket 文件
	if err := os.Remove(d.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing socket: %v", err)
	}

	// 创建 HTTP 服务器
	mux := http.NewServeMux()
	mux.HandleFunc("/cni", d.handleCNIRequest)

	server := &http.Server{
		Addr:    "unix://" + d.socketPath,
		Handler: mux,
	}

	// 启动服务器
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			d.logger.Error("CNI server error", "error", err)
		}
	}()

	d.logger.Info("CNI server started", "socket", d.socketPath)
	return nil
}

// stopCNIServer 停止 CNI 通信服务器
func (d *Daemon) stopCNIServer() error {
	// 删除 socket 文件
	if err := os.Remove(d.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove socket file: %v", err)
	}
	return nil
}

// handleCNIRequest 处理 CNI 请求
func (d *Daemon) handleCNIRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req CNIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var resp CNIResponse

	switch req.Type {
	case "allocate":
		resp = d.handleAllocateRequest(req)
	case "release":
		resp = d.handleReleaseRequest(req)
	case "status":
		resp = d.handleStatusRequest(req)
	default:
		resp = CNIResponse{
			Success: false,
			Error:   fmt.Sprintf("Unknown request type: %s", req.Type),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleAllocateRequest 处理 IP 分配请求
func (d *Daemon) handleAllocateRequest(req CNIRequest) CNIResponse {
	d.stateMutex.Lock()
	defer d.stateMutex.Unlock()

	podKey := fmt.Sprintf("%s/%s", req.Namespace, req.PodName)

	// 检查是否已分配
	if podInfo, exists := d.networkState.Pods[podKey]; exists {
		return CNIResponse{
			Success: true,
			Data: map[string]interface{}{
				"ip": podInfo.IP.String(),
			},
		}
	}

	// 分配新 IP（简化实现）
	// 这里应该调用 IPAM 管理器
	ip := net.ParseIP("10.244.0.100") // 临时实现

	d.networkState.Pods[podKey] = &PodInfo{
		Name:      req.PodName,
		Namespace: req.Namespace,
		IP:        ip,
		Created:   time.Now(),
	}

	return CNIResponse{
		Success: true,
		Data: map[string]interface{}{
			"ip": ip.String(),
		},
	}
}

// handleReleaseRequest 处理 IP 释放请求
func (d *Daemon) handleReleaseRequest(req CNIRequest) CNIResponse {
	d.stateMutex.Lock()
	defer d.stateMutex.Unlock()

	podKey := fmt.Sprintf("%s/%s", req.Namespace, req.PodName)
	delete(d.networkState.Pods, podKey)

	return CNIResponse{
		Success: true,
	}
}

// handleStatusRequest 处理状态查询请求
func (d *Daemon) handleStatusRequest(req CNIRequest) CNIResponse {
	d.stateMutex.RLock()
	defer d.stateMutex.RUnlock()

	podKey := fmt.Sprintf("%s/%s", req.Namespace, req.PodName)
	if podInfo, exists := d.networkState.Pods[podKey]; exists {
		return CNIResponse{
			Success: true,
			Data: map[string]interface{}{
				"ip":      podInfo.IP.String(),
				"nodekey": podInfo.NodeKey,
				"routeid": podInfo.RouteID,
			},
		}
	}

	return CNIResponse{
		Success: false,
		Error:   "Pod not found",
	}
}

// processPods 处理 Pod 事件
func (d *Daemon) processPods() {
	for {
		select {
		case <-d.ctx.Done():
			return
		default:
			// 处理队列中的 Pod
			key, quit := d.podQueue.Get()
			if quit {
				return
			}

			func() {
				defer d.podQueue.Done(key)

				// 解析 Pod 键
				_, _, err := cache.SplitMetaNamespaceKey(key.(string))
				if err != nil {
					d.logger.Error("Failed to split pod key", "key", key, "error", err)
					d.podQueue.Forget(key)
					return
				}

				// 获取 Pod 对象
				obj, exists, err := d.podInformer.GetIndexer().GetByKey(key.(string))
				if err != nil {
					d.logger.Error("Failed to get pod from cache", "key", key, "error", err)
					d.podQueue.Forget(key)
					return
				}

				if !exists {
					// Pod 已删除
					d.handlePodDeleted(obj)
					d.podQueue.Forget(key)
					return
				}

				pod := obj.(*corev1.Pod)

				// 根据模式处理 Pod
				switch d.config.Mode {
				case "host":
					d.handleHostModePod(pod)
				case "daemon":
					d.handleDaemonModePod(pod)
				default:
					d.logger.Error("Unknown mode", "mode", d.config.Mode)
				}

				d.podQueue.Forget(key)
			}()
		}
	}
}

// handlePodEvent 处理 Pod 事件
func (d *Daemon) handlePodEvent(obj interface{}) {
	pod := obj.(*corev1.Pod)
	key, err := cache.MetaNamespaceKeyFunc(pod)
	if err != nil {
		d.logger.Error("Failed to get pod key", "error", err)
		return
	}
	d.podQueue.Add(key)
}

// handlePodChanged 处理 Pod 变更
func (d *Daemon) handlePodChanged(oldObj, newObj interface{}) {
	oldPod := oldObj.(*corev1.Pod)
	newPod := newObj.(*corev1.Pod)

	// 检查 Pod IP 是否发生变化
	if oldPod.Status.PodIP != newPod.Status.PodIP {
		key, err := cache.MetaNamespaceKeyFunc(newPod)
		if err != nil {
			d.logger.Error("Failed to get pod key", "error", err)
			return
		}
		d.podQueue.Add(key)
	}
}

// handlePodDeleted 处理 Pod 删除
func (d *Daemon) handlePodDeleted(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			d.logger.Error("Failed to get pod from tombstone")
			return
		}
		pod, ok = tombstone.Obj.(*corev1.Pod)
		if !ok {
			d.logger.Error("Failed to get pod from tombstone obj")
			return
		}
	}

	// 清理 Pod 相关资源
	d.cleanupPodResources(pod)
}

// handleHostModePod 处理 Host 模式的 Pod
func (d *Daemon) handleHostModePod(pod *corev1.Pod) {
	// 检查 Pod 是否有 IP
	if pod.Status.PodIP == "" {
		return
	}

	podKey := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)

	// 检查 Pod 是否已经处理过
	d.stateMutex.RLock()
	_, exists := d.networkState.Pods[podKey]
	d.stateMutex.RUnlock()

	if exists {
		return
	}

	d.logger.Info("Processing host mode pod", "pod", podKey, "ip", pod.Status.PodIP)

	// 请求 Headscale 路由
	if err := d.requestHeadscaleRoute(pod.Status.PodIP); err != nil {
		d.logger.Error("Failed to request headscale route", "pod", podKey, "error", err)
		return
	}

	// 更新状态
	d.stateMutex.Lock()
	d.networkState.Pods[podKey] = &PodInfo{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		IP:        net.ParseIP(pod.Status.PodIP),
		Created:   time.Now(),
	}
	d.stateMutex.Unlock()

	d.logger.Info("Successfully processed host mode pod", "pod", podKey)
}

// handleDaemonModePod 处理 Daemon 模式的 Pod
func (d *Daemon) handleDaemonModePod(pod *corev1.Pod) {
	// 检查 Pod 是否有 IP
	if pod.Status.PodIP == "" {
		return
	}

	podKey := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)

	// 检查 Pod 是否已经处理过
	d.stateMutex.RLock()
	_, exists := d.networkState.Pods[podKey]
	d.stateMutex.RUnlock()

	if exists {
		return
	}

	d.logger.Info("Processing daemon mode pod", "pod", podKey, "ip", pod.Status.PodIP)

	// 创建 Tailscale 节点
	nodeKey, err := d.createTailscaleNode(pod.Status.PodIP)
	if err != nil {
		d.logger.Error("Failed to create tailscale node", "pod", podKey, "error", err)
		return
	}

	// 创建 HeadCNI 专用接口
	if err := d.createHeadCNIInterface(nodeKey); err != nil {
		d.logger.Error("Failed to create headcni01 interface", "pod", podKey, "error", err)
		return
	}

	// 启用路由
	if err := d.enablePodRoutes(pod.Status.PodIP); err != nil {
		d.logger.Error("Failed to enable pod routes", "pod", podKey, "error", err)
		return
	}

	// 更新状态
	d.stateMutex.Lock()
	d.networkState.Pods[podKey] = &PodInfo{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		IP:        net.ParseIP(pod.Status.PodIP),
		NodeKey:   nodeKey,
		Created:   time.Now(),
	}
	d.stateMutex.Unlock()

	d.logger.Info("Successfully processed daemon mode pod", "pod", podKey)
}

// requestHeadscaleRoute 请求 Headscale 路由
func (d *Daemon) requestHeadscaleRoute(podIP string) error {
	ctx, cancel := context.WithTimeout(d.ctx, 30*time.Second)
	defer cancel()

	// 检查 API Key 健康状态
	if err := d.headscale.CheckApiKeyHealth(ctx); err != nil {
		return fmt.Errorf("API key health check failed: %v", err)
	}

	// 请求路由
	if err := d.headscale.RequestRoute(podIP); err != nil {
		return fmt.Errorf("failed to request route: %v", err)
	}

	return nil
}

// createTailscaleNode 创建 Tailscale 节点
func (d *Daemon) createTailscaleNode(podIP string) (string, error) {
	ctx, cancel := context.WithTimeout(d.ctx, 30*time.Second)
	defer cancel()

	// 创建预授权密钥
	preAuthReq := &headscale.CreatePreAuthKeyRequest{
		User:      "headcni",
		Reusable:  true,
		Ephemeral: false,
	}

	preAuthResp, err := d.headscale.CreatePreAuthKey(ctx, preAuthReq)
	if err != nil {
		return "", fmt.Errorf("failed to create pre-auth key: %v", err)
	}

	// 创建节点
	nodeName := fmt.Sprintf("headcni-pod-%s", podIP)
	nodeReq := &headscale.DebugCreateNodeRequest{
		User:   "headcni",
		Key:    preAuthResp.PreAuthKey.Key,
		Name:   nodeName,
		Routes: []string{podIP + "/32"},
	}

	nodeResp, err := d.headscale.DebugCreateNode(ctx, nodeReq)
	if err != nil {
		return "", fmt.Errorf("failed to create node: %v", err)
	}

	// 更新状态
	d.stateMutex.Lock()
	d.networkState.Nodes[nodeResp.Node.ID] = &NodeInfo{
		ID:       nodeResp.Node.ID,
		Name:     nodeResp.Node.Name,
		NodeKey:  nodeResp.Node.NodeKey,
		IP:       net.ParseIP(podIP),
		Online:   nodeResp.Node.Online,
		Created:  nodeResp.Node.CreatedAt,
		LastSeen: nodeResp.Node.LastSeen,
	}
	d.stateMutex.Unlock()

	return nodeResp.Node.NodeKey, nil
}

// createHeadCNIInterface 创建 HeadCNI 专用接口
func (d *Daemon) createHeadCNIInterface(nodeKey string) error {
	// 检查接口是否已存在
	if d.networkMgr.InterfaceExists(d.config.InterfaceName) {
		d.logger.Debug("HeadCNI interface already exists", "interface", d.config.InterfaceName)
		return nil
	}

	d.logger.Info("Creating HeadCNI interface", "interface", d.config.InterfaceName)

	// 使用 tailscale up 命令创建专用接口
	if err := d.networkMgr.StartTailscaleService(d.config.InterfaceName, nodeKey, d.config.HeadscaleURL); err != nil {
		return fmt.Errorf("failed to start tailscale service for interface %s: %v", d.config.InterfaceName, err)
	}

	d.logger.Info("Successfully created HeadCNI interface", "interface", d.config.InterfaceName)
	return nil
}

// cleanupHeadCNIInterface 清理 HeadCNI 专用接口
func (d *Daemon) cleanupHeadCNIInterface() error {
	// 检查接口是否存在
	if !d.networkMgr.InterfaceExists(d.config.InterfaceName) {
		d.logger.Debug("HeadCNI interface does not exist", "interface", d.config.InterfaceName)
		return nil
	}

	d.logger.Info("Cleaning up HeadCNI interface", "interface", d.config.InterfaceName)

	// 停止 Tailscale 服务
	if err := d.networkMgr.StopTailscaleService(d.config.InterfaceName); err != nil {
		return fmt.Errorf("failed to stop tailscale service for interface %s: %v", d.config.InterfaceName, err)
	}

	// 删除接口
	if err := d.networkMgr.DeleteInterface(d.config.InterfaceName); err != nil {
		return fmt.Errorf("failed to delete interface %s: %v", d.config.InterfaceName, err)
	}

	d.logger.Info("Successfully cleaned up HeadCNI interface", "interface", d.config.InterfaceName)
	return nil
}

// enablePodRoutes 启用 Pod 路由
func (d *Daemon) enablePodRoutes(podIP string) error {
	ctx, cancel := context.WithTimeout(d.ctx, 30*time.Second)
	defer cancel()

	// 查找相关节点
	nodes, err := d.headscale.ListNodes(ctx, "")
	if err != nil {
		return fmt.Errorf("failed to list nodes: %v", err)
	}

	nodeName := fmt.Sprintf("headcni-pod-%s", podIP)
	for _, node := range nodes.Nodes {
		if node.Name == nodeName {
			// 获取节点路由
			routes, err := d.headscale.GetNodeRoutes(ctx, node.ID)
			if err != nil {
				return fmt.Errorf("failed to get node routes: %v", err)
			}

			// 启用路由
			for _, route := range routes.Routes {
				if err := d.headscale.EnableRoute(ctx, route.ID); err != nil {
					return fmt.Errorf("failed to enable route %s: %v", route.ID, err)
				}

				// 更新状态
				d.stateMutex.Lock()
				d.networkState.Routes[route.ID] = &RouteInfo{
					ID:      route.ID,
					NodeID:  node.ID,
					Prefix:  route.Prefix,
					Enabled: true,
					Created: route.CreatedAt,
				}
				d.stateMutex.Unlock()
			}

			return nil
		}
	}

	return fmt.Errorf("node for pod %s not found", podIP)
}

// cleanupPodResources 清理 Pod 资源
func (d *Daemon) cleanupPodResources(pod *corev1.Pod) {
	podKey := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)

	d.stateMutex.Lock()
	podInfo, exists := d.networkState.Pods[podKey]
	if !exists {
		d.stateMutex.Unlock()
		return
	}

	// 删除 Pod 信息
	delete(d.networkState.Pods, podKey)
	d.stateMutex.Unlock()

	d.logger.Info("Cleaning up pod resources", "pod", podKey)

	// 根据模式清理资源
	switch d.config.Mode {
	case "host":
		// Host 模式不需要特殊清理
		d.logger.Info("Host mode pod cleanup completed", "pod", podKey)

	case "daemon":
		// Daemon 模式需要删除节点和路由
		if err := d.cleanupDaemonModePod(podInfo); err != nil {
			d.logger.Error("Failed to cleanup daemon mode pod", "pod", podKey, "error", err)
		} else {
			d.logger.Info("Daemon mode pod cleanup completed", "pod", podKey)
		}

		// 清理 HeadCNI 专用接口（如果没有其他 Pod 使用）
		if err := d.cleanupHeadCNIInterface(); err != nil {
			d.logger.Error("Failed to cleanup headcni01 interface", "pod", podKey, "error", err)
		}
	}
}

// cleanupDaemonModePod 清理 Daemon 模式的 Pod
func (d *Daemon) cleanupDaemonModePod(podInfo *PodInfo) error {
	ctx, cancel := context.WithTimeout(d.ctx, 30*time.Second)
	defer cancel()

	// 查找相关节点
	nodes, err := d.headscale.ListNodes(ctx, "")
	if err != nil {
		return fmt.Errorf("failed to list nodes: %v", err)
	}

	nodeName := fmt.Sprintf("headcni-pod-%s", podInfo.IP.String())
	for _, node := range nodes.Nodes {
		if node.Name == nodeName {
			// 获取节点路由
			routes, err := d.headscale.GetNodeRoutes(ctx, node.ID)
			if err != nil {
				return fmt.Errorf("failed to get node routes: %v", err)
			}

			// 删除路由
			for _, route := range routes.Routes {
				if err := d.headscale.DeleteRoute(ctx, route.ID); err != nil {
					return fmt.Errorf("failed to delete route %s: %v", route.ID, err)
				}

				// 更新状态
				d.stateMutex.Lock()
				delete(d.networkState.Routes, route.ID)
				d.stateMutex.Unlock()
			}

			// 删除节点
			if err := d.headscale.DeleteNode(ctx, node.ID); err != nil {
				return fmt.Errorf("failed to delete node: %v", err)
			}

			// 更新状态
			d.stateMutex.Lock()
			delete(d.networkState.Nodes, node.ID)
			d.stateMutex.Unlock()

			return nil
		}
	}

	return fmt.Errorf("node for pod %s not found", podInfo.IP.String())
}

// manageNetworkState 管理网络状态
func (d *Daemon) manageNetworkState() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.updateNetworkState()
		}
	}
}

// updateNetworkState 更新网络状态
func (d *Daemon) updateNetworkState() {
	ctx, cancel := context.WithTimeout(d.ctx, 30*time.Second)
	defer cancel()

	// 更新节点状态
	nodes, err := d.headscale.ListNodes(ctx, "")
	if err != nil {
		d.logger.Error("Failed to update node state", "error", err)
		return
	}

	d.stateMutex.Lock()
	for _, node := range nodes.Nodes {
		if nodeInfo, exists := d.networkState.Nodes[node.ID]; exists {
			nodeInfo.Online = node.Online
			nodeInfo.LastSeen = node.LastSeen
		}
	}
	d.stateMutex.Unlock()

	// 更新路由状态
	routes, err := d.headscale.GetRoutes(ctx)
	if err != nil {
		d.logger.Error("Failed to update route state", "error", err)
		return
	}

	d.stateMutex.Lock()
	for _, route := range routes.Routes {
		if routeInfo, exists := d.networkState.Routes[route.ID]; exists {
			routeInfo.Enabled = route.Enabled
		}
	}
	d.stateMutex.Unlock()
}

// maintainHeadscaleConnection 维护 Headscale 连接
func (d *Daemon) maintainHeadscaleConnection() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.checkHeadscaleConnection()
		}
	}
}

// checkHeadscaleConnection 检查 Headscale 连接
func (d *Daemon) checkHeadscaleConnection() {
	ctx, cancel := context.WithTimeout(d.ctx, 10*time.Second)
	defer cancel()

	// 检查 API Key 健康状态
	if err := d.headscale.CheckApiKeyHealth(ctx); err != nil {
		d.logger.Error("Headscale API key health check failed", "error", err)
		// 可以在这里实现重试逻辑或告警
		return
	}

	// 清理过期节点
	if err := d.headscale.CleanupExpiredNodes(ctx); err != nil {
		d.logger.Error("Failed to cleanup expired nodes", "error", err)
	}

	d.logger.Debug("Headscale connection check completed")
}

// manageTailscaleInterface 管理 Tailscale 接口
func (d *Daemon) manageTailscaleInterface() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.checkTailscaleInterface()
		}
	}
}

// checkTailscaleInterface 检查 Tailscale 接口
func (d *Daemon) checkTailscaleInterface() {
	// 检查 Tailscale 连接状态
	if err := d.networkMgr.CheckTailscaleConnectivity(); err != nil {
		d.logger.Error("Tailscale connectivity check failed", "error", err)
		// 可以在这里实现重连逻辑
		return
	}

	// 获取 Tailscale IP
	ip, err := d.networkMgr.GetTailscaleIP()
	if err != nil {
		d.logger.Error("Failed to get Tailscale IP", "error", err)
		return
	}

	d.logger.Debug("Tailscale interface check completed", "ip", ip.String())
}

// GetNetworkState 获取网络状态
func (d *Daemon) GetNetworkState() *NetworkState {
	d.stateMutex.RLock()
	defer d.stateMutex.RUnlock()

	// 创建副本
	state := &NetworkState{
		Nodes:  make(map[string]*NodeInfo),
		Routes: make(map[string]*RouteInfo),
		Pods:   make(map[string]*PodInfo),
	}

	for k, v := range d.networkState.Nodes {
		state.Nodes[k] = v
	}
	for k, v := range d.networkState.Routes {
		state.Routes[k] = v
	}
	for k, v := range d.networkState.Pods {
		state.Pods[k] = v
	}

	return state
}
