package daemon

import (
	"context"
	"fmt"
	"net/netip"
	"sync"
	"time"

	coreV1 "k8s.io/api/core/v1"

	"github.com/binrclab/headcni/pkg/constants"
	"github.com/binrclab/headcni/pkg/headscale"
	"github.com/binrclab/headcni/pkg/k8s"
	"github.com/binrclab/headcni/pkg/logging"
)

// PodMonitoringService Pod 状态监听服务
type PodMonitoringService struct {
	preparer  *Preparer
	k8sClient k8s.Client
	running   bool
	mu        sync.RWMutex

	// 网络配置状态
	currentPodCIDR string
	lastCheckTime  time.Time
	checkInterval  time.Duration
}

// NewPodMonitoringService 创建新的 Pod 监控服务
func NewPodMonitoringService(preparer *Preparer) *PodMonitoringService {
	return &PodMonitoringService{
		preparer:      preparer,
		checkInterval: 5 * time.Minute, // 每5分钟检查一次网络配置
	}
}

func (s *PodMonitoringService) Name() string { return constants.ServiceNamePodMonitoring }

func (s *PodMonitoringService) Reload(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	logging.Infof("Reloading Pod monitoring service")

	if !s.running {
		return fmt.Errorf("service is not running")
	}

	// 获取新的配置
	newConfig := s.preparer.GetConfig()
	if newConfig == nil {
		return fmt.Errorf("failed to get new configuration")
	}

	// 检查监控配置变更
	configChanged := false
	oldConfig := s.preparer.GetOldConfig()
	if oldConfig != nil {
		if newConfig.Network.PodCIDR.Base != oldConfig.Network.PodCIDR.Base {
			configChanged = true
		}
	}

	if !configChanged {
		logging.Infof("Pod monitoring configuration unchanged, no reload needed")
		return nil
	}

	logging.Infof("Pod monitoring configuration changed, performing reload")

	// 停止当前服务
	if err := s.Stop(ctx); err != nil {
		logging.Errorf("Failed to stop service during reload: %v", err)
	}

	// 重新启动服务
	if err := s.Start(ctx); err != nil {
		logging.Errorf("Failed to restart service during reload: %v", err)
		return err
	}

	logging.Infof("Pod monitoring service reloaded successfully")
	return nil
}

func (s *PodMonitoringService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	// 获取 Kubernetes 客户端
	k8sClient := s.preparer.GetK8sClient()

	// 获取当前节点名称
	nodeName, err := k8sClient.GetCurrentNodeName()
	if err != nil {
		// 更新健康状态为失败
		healthMgr := GetGlobalHealthManager()
		healthMgr.UpdateServiceStatus(s.Name(), false, err)
		return fmt.Errorf("failed to get current node name: %v", err)
	}

	// 获取当前节点信息
	node, err := k8sClient.Nodes().Get(context.Background(), nodeName)
	if err != nil {
		// 更新健康状态为失败
		healthMgr := GetGlobalHealthManager()
		healthMgr.UpdateServiceStatus(s.Name(), false, err)
		return fmt.Errorf("failed to get current node: %v", err)
	}

	// 记录当前 Pod CIDR
	s.currentPodCIDR, err = k8sClient.Nodes().GetPodCIDR(nodeName)
	if err != nil {
		// 更新健康状态为失败
		healthMgr := GetGlobalHealthManager()
		healthMgr.UpdateServiceStatus(s.Name(), false, err)
		return fmt.Errorf("failed to get Pod CIDR for node %s: %v", nodeName, err)
	}
	s.lastCheckTime = time.Now()

	logging.Infof("Pod monitoring service starting for node: %s, Pod CIDR: %s",
		node.Name, s.currentPodCIDR)

	// 保存 k8s 客户端引用
	s.k8sClient = k8sClient

	// 启动网络配置监控协程
	go s.networkConfigMonitor(ctx)

	s.running = true

	// 更新健康状态为成功
	healthMgr := GetGlobalHealthManager()
	healthMgr.UpdateServiceStatus(s.Name(), true, nil)

	return nil
}

func (s *PodMonitoringService) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	// 清理资源
	s.k8sClient = nil
	s.running = false

	// 更新健康状态
	healthMgr := GetGlobalHealthManager()
	healthMgr.UpdateServiceStatus(s.Name(), false, nil)

	logging.Infof("Pod monitoring service stopped")
	return nil
}

func (s *PodMonitoringService) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// DaemonNodeHandler 实现 k8s.NodeEventHandler 接口
type DaemonNodeHandler struct {
	preparer *Preparer
	service  *PodMonitoringService
}

func (h *DaemonNodeHandler) OnNodeAdd(node *coreV1.Node) error {
	logging.Infof("Node added - name: %s", node.Name)
	return nil
}

func (h *DaemonNodeHandler) OnNodeUpdate(oldNode, newNode *coreV1.Node) error {
	logging.Infof("Node updated - name: %s", newNode.Name)

	// 检查 Pod CIDR 是否发生变化
	oldPodCIDR := oldNode.Spec.PodCIDR
	newPodCIDR := newNode.Spec.PodCIDR

	if oldPodCIDR != newPodCIDR {
		logging.Infof("Pod CIDR changed from %s to %s", oldPodCIDR, newPodCIDR)

		// 通知服务处理网络配置变化
		if h.service != nil {
			h.service.handlePodCIDRChange(newPodCIDR)
		}
	}

	return nil
}

func (h *DaemonNodeHandler) OnNodeDelete(node *coreV1.Node) error {
	logging.Infof("Node deleted - name: %s", node.Name)
	return nil
}

// networkConfigMonitor 网络配置监控协程
func (s *PodMonitoringService) networkConfigMonitor(ctx context.Context) {
	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logging.Infof("Network config monitor stopped")
			return
		case <-ticker.C:
			s.checkNetworkConfiguration(ctx)
		}
	}
}

// checkNetworkConfiguration 检查网络配置状态
func (s *PodMonitoringService) checkNetworkConfiguration(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 获取当前节点名称
	k8sClient := s.preparer.GetK8sClient()
	nodeName, err := k8sClient.GetCurrentNodeName()
	if err != nil {
		logging.Errorf("Failed to get current node name: %v", err)
		return
	}

	// 获取当前 Pod CIDR
	currentPodCIDR, err := k8sClient.Nodes().GetPodCIDR(nodeName)
	if err != nil {
		logging.Errorf("Failed to get Pod CIDR for node %s: %v", nodeName, err)
		return
	}

	// 检查 Pod CIDR 是否发生变化
	if currentPodCIDR != s.currentPodCIDR {
		logging.Infof("Pod CIDR changed from %s to %s", s.currentPodCIDR, currentPodCIDR)
		s.handlePodCIDRChange(currentPodCIDR)
	}

	// 检查网络配置是否正常
	if err := s.validateNetworkConfiguration(ctx, currentPodCIDR); err != nil {
		logging.Warnf("Network configuration validation failed: %v", err)
		// 尝试自动修复
		s.attemptNetworkRepair(ctx, currentPodCIDR)
	}

	s.lastCheckTime = time.Now()
}

// handlePodCIDRChange 处理 Pod CIDR 变化
func (s *PodMonitoringService) handlePodCIDRChange(newPodCIDR string) {
	logging.Infof("Handling Pod CIDR change to: %s", newPodCIDR)

	// 更新内部状态
	s.currentPodCIDR = newPodCIDR

	// 1. 更新 Tailscale 路由
	if err := s.updateTailscaleRoutes(newPodCIDR); err != nil {
		logging.Errorf("Failed to update Tailscale routes: %v", err)
	}

	// 2. 更新 Headscale 路由
	if err := s.updateHeadscaleRoutes(newPodCIDR); err != nil {
		logging.Errorf("Failed to update Headscale routes: %v", err)
	}

	// 3. 更新 CNI 配置
	if err := s.updateCNIConfiguration(newPodCIDR); err != nil {
		logging.Errorf("Failed to update CNI configuration: %v", err)
	}

	logging.Infof("Completed Pod CIDR change handling for: %s", newPodCIDR)
}

// validateNetworkConfiguration 验证网络配置
func (s *PodMonitoringService) validateNetworkConfiguration(ctx context.Context, podCIDR string) error {
	logging.Debugf("Validating network configuration for Pod CIDR: %s", podCIDR)

	// 1. 检查 Tailscale 路由配置
	if err := s.checkTailscaleRouteConfiguration(podCIDR); err != nil {
		return fmt.Errorf("Tailscale route configuration check failed: %v", err)
	}

	// 2. 检查 Headscale 路由状态
	if err := s.checkHeadscaleRouteStatus(podCIDR); err != nil {
		return fmt.Errorf("Headscale route status check failed: %v", err)
	}

	// 3. 检查 CNI 配置
	if err := s.checkCNIConfiguration(podCIDR); err != nil {
		return fmt.Errorf("CNI configuration check failed: %v", err)
	}

	return nil
}

// checkTailscaleRouteConfiguration 检查 Tailscale 路由配置
func (s *PodMonitoringService) checkTailscaleRouteConfiguration(podCIDR string) error {
	tailscaleClient := s.preparer.GetTailscaleClient()
	if tailscaleClient == nil {
		return fmt.Errorf("tailscale client not available")
	}

	// 获取 Tailscale 偏好设置
	prefs, err := tailscaleClient.GetPrefs(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get Tailscale preferences: %v", err)
	}

	// 检查是否在通告路由中
	for _, advertiseRoute := range prefs.AdvertiseRoutes {
		if advertiseRoute.String() == podCIDR {
			logging.Debugf("Pod CIDR %s found in Tailscale advertised routes", podCIDR)
			return nil
		}
	}

	return fmt.Errorf("Pod CIDR %s not found in Tailscale advertised routes", podCIDR)
}

// checkHeadscaleRouteStatus 检查 Headscale 路由状态
func (s *PodMonitoringService) checkHeadscaleRouteStatus(podCIDR string) error {
	headscaleClient := s.preparer.GetHeadscaleClient()
	if headscaleClient == nil {
		return fmt.Errorf("headscale client not available")
	}

	// 获取所有路由
	routes, err := headscaleClient.GetRoutes(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get Headscale routes: %v", err)
	}

	// 查找匹配的路由并检查是否启用
	for _, route := range routes.Routes {
		if route.Prefix == podCIDR {
			if route.Enabled {
				logging.Debugf("Pod CIDR %s found and enabled in Headscale", podCIDR)
				return nil
			} else {
				return fmt.Errorf("Pod CIDR %s found in Headscale but not enabled", podCIDR)
			}
		}
	}

	return fmt.Errorf("Pod CIDR %s not found in Headscale", podCIDR)
}

// checkCNIConfiguration 检查 CNI 配置
func (s *PodMonitoringService) checkCNIConfiguration(podCIDR string) error {
	logging.Debugf("Checking CNI configuration for Pod CIDR: %s", podCIDR)

	cniConfigManager := s.preparer.GetCNIConfigManager()
	if cniConfigManager == nil {
		return fmt.Errorf("CNI config manager not available")
	}

	// 读取当前 CNI 配置
	configList, err := cniConfigManager.ReadConfigList()
	if err != nil {
		return fmt.Errorf("failed to read CNI configuration: %v", err)
	}

	// 检查 Pod CIDR 是否匹配
	for _, plugin := range configList.Plugins {
		if plugin.Type == "headcni" && plugin.PodCIDR == podCIDR {
			logging.Debugf("CNI configuration is up to date for Pod CIDR: %s", podCIDR)
			return nil
		}
	}

	return fmt.Errorf("CNI configuration is outdated for Pod CIDR: %s", podCIDR)
}

// attemptNetworkRepair 尝试修复网络配置
func (s *PodMonitoringService) attemptNetworkRepair(ctx context.Context, podCIDR string) {
	logging.Infof("Attempting to repair network configuration for Pod CIDR: %s", podCIDR)

	// 1. 尝试更新 Tailscale 路由
	if err := s.updateTailscaleRoutes(podCIDR); err != nil {
		logging.Errorf("Failed to repair Tailscale routes: %v", err)
	}

	//中途延时，确保路由在云端生效
	time.Sleep(2 * time.Second)

	// 2. 尝试更新 Headscale 路由
	if err := s.updateHeadscaleRoutes(podCIDR); err != nil {
		logging.Errorf("Failed to repair Headscale routes: %v", err)
	}

	// 3. 尝试更新 CNI 配置
	if err := s.updateCNIConfiguration(podCIDR); err != nil {
		logging.Errorf("Failed to repair CNI configuration: %v", err)
	}
}

// updateTailscaleRoutes 更新 Tailscale 路由（对比后智能添加）
func (s *PodMonitoringService) updateTailscaleRoutes(podCIDR string) error {
	logging.Infof("Updating Tailscale routes for Pod CIDR: %s", podCIDR)

	tailscaleClient := s.preparer.GetTailscaleClient()
	if tailscaleClient == nil {
		return fmt.Errorf("tailscale client not available")
	}

	// 1. 获取当前已通告的路由
	prefs, err := tailscaleClient.GetPrefs(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get current Tailscale preferences: %v", err)
	}

	// 2. 检查路由是否已存在
	for _, existingRoute := range prefs.AdvertiseRoutes {
		if existingRoute.String() == podCIDR {
			logging.Infof("Route %s already exists in Tailscale, no update needed", podCIDR)
			return nil
		}
	}

	// 3. 解析新的 CIDR
	newPrefix, err := netip.ParsePrefix(podCIDR)
	if err != nil {
		return fmt.Errorf("invalid CIDR format %s: %v", podCIDR, err)
	}

	// 4. 合并现有路由和新路由
	mergedRoutes := make([]netip.Prefix, len(prefs.AdvertiseRoutes)+1)
	copy(mergedRoutes, prefs.AdvertiseRoutes)
	mergedRoutes[len(prefs.AdvertiseRoutes)] = newPrefix

	logging.Infof("Merging routes: existing %d routes + new route %s = total %d routes",
		len(prefs.AdvertiseRoutes), podCIDR, len(mergedRoutes))

	// 5. 应用合并后的路由
	if err := tailscaleClient.AdvertiseRoutes(context.Background(), mergedRoutes...); err != nil {
		return fmt.Errorf("failed to advertise merged routes: %v", err)
	}

	logging.Infof("Successfully updated Tailscale routes, added %s (total: %d routes)", podCIDR, len(mergedRoutes))
	return nil
}

// updateHeadscaleRoutes 更新 Headscale 路由（检查并启用路由）
func (s *PodMonitoringService) updateHeadscaleRoutes(podCIDR string) error {
	logging.Infof("Updating Headscale routes for Pod CIDR: %s", podCIDR)

	headscaleClient := s.preparer.GetHeadscaleClient()
	if headscaleClient == nil {
		return fmt.Errorf("headscale client not available")
	}

	// 1. 获取所有路由
	routesResp, err := headscaleClient.GetRoutes(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get routes from Headscale: %v", err)
	}

	// 2. 查找匹配的路由
	var targetRoute *headscale.Route
	for _, route := range routesResp.Routes {
		if route.Prefix == podCIDR {
			targetRoute = &route
			break
		}
	}

	if targetRoute == nil {
		logging.Warnf("Route for CIDR %s not found in Headscale, it may not be advertised yet", podCIDR)
		return nil // 不是错误，路由可能还没有被 Tailscale 通告
	}

	// 3. 检查路由是否已启用
	if targetRoute.Enabled {
		logging.Infof("Route for CIDR %s is already enabled in Headscale", podCIDR)
		return nil
	}

	// 4. 启用路由
	logging.Infof("Enabling route %s in Headscale", targetRoute.ID)
	if err := headscaleClient.EnableRoute(context.Background(), targetRoute.ID); err != nil {
		return fmt.Errorf("failed to enable route %s in Headscale: %v", targetRoute.ID, err)
	}

	logging.Infof("Successfully enabled Headscale route %s for CIDR: %s", targetRoute.ID, podCIDR)
	return nil
}

// updateCNIConfiguration 更新 CNI 配置（局部更新）
func (s *PodMonitoringService) updateCNIConfiguration(podCIDR string) error {
	logging.Infof("Updating CNI configuration for Pod CIDR: %s", podCIDR)

	cniConfigManager := s.preparer.GetCNIConfigManager()
	if cniConfigManager == nil {
		return fmt.Errorf("CNI config manager not available")
	}

	// 获取当前配置
	config := s.preparer.GetConfig()
	if config == nil {
		return fmt.Errorf("configuration not available")
	}

	// 构建更新参数
	updates := map[string]interface{}{
		"pod_cidr":     podCIDR,
		"service_cidr": config.Network.ServiceCIDR,
		"mtu":          config.Network.MTU,
	}

	// 使用 UpdateConfigList 进行局部更新
	if err := cniConfigManager.UpdateConfigList(updates); err != nil {
		return fmt.Errorf("failed to update CNI configuration: %v", err)
	}

	logging.Infof("Successfully updated CNI configuration for Pod CIDR: %s", podCIDR)
	return nil
}
