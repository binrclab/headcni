package daemon

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/binrclab/headcni/pkg/cni"
	"github.com/binrclab/headcni/pkg/constants"
	"github.com/binrclab/headcni/pkg/headscale"
	"github.com/binrclab/headcni/pkg/logging"
)

// CNIService CNI 管理服务
type CNIService struct {
	preparer  *Preparer
	cniServer *cni.Server
	running   bool
	mu        sync.RWMutex
}

// NewCNIService 创建新的 CNI 服务
func NewCNIService(preparer *Preparer) *CNIService {
	return &CNIService{preparer: preparer}
}

func (s *CNIService) Name() string { return constants.ServiceNameCNI }

func (s *CNIService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	// 创建带有路由验证的 CNI 服务器
	s.cniServer = s.createCNIServerWithRouteValidation()

	// 启动 CNI 服务器
	if err := s.cniServer.Start(); err != nil {
		// 更新健康状态为失败
		healthMgr := GetGlobalHealthManager()
		healthMgr.UpdateServiceStatus(s.Name(), false, err)
		return fmt.Errorf("failed to start CNI server: %v", err)
	}

	s.running = true

	// 更新健康状态为成功
	healthMgr := GetGlobalHealthManager()
	healthMgr.UpdateServiceStatus(s.Name(), true, nil)

	logging.Infof("CNI service started successfully with route validation")
	return nil
}

func (s *CNIService) Reload(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	logging.Infof("Reloading CNI service")

	if !s.running {
		return fmt.Errorf("service is not running")
	}

	// 获取新的配置
	newConfig := s.preparer.GetConfig()
	if newConfig == nil {
		return fmt.Errorf("failed to get new configuration")
	}
	oldConfig := s.preparer.GetOldConfig()

	// 检查网络配置变更
	configChanged := false
	if oldConfig != nil {
		if newConfig.Network.PodCIDR.Base != oldConfig.Network.PodCIDR.Base ||
			newConfig.Network.ServiceCIDR != oldConfig.Network.ServiceCIDR ||
			newConfig.Network.MTU != oldConfig.Network.MTU {
			configChanged = true
		}
	}

	if !configChanged {
		logging.Infof("CNI configuration unchanged, no reload needed")
		return nil
	}

	logging.Infof("CNI configuration changed, performing reload")

	// 停止当前服务
	if err := s.Stop(ctx); err != nil {
		logging.Errorf("Failed to stop service during reload: %v", err)
	}

	// 重新启动服务
	if err := s.Start(ctx); err != nil {
		logging.Errorf("Failed to restart service during reload: %v", err)
		return err
	}

	logging.Infof("CNI service reloaded successfully")
	return nil
}

func (s *CNIService) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	// 停止 CNI 服务器
	var err error
	if s.cniServer != nil {
		if stopErr := s.cniServer.Stop(); stopErr != nil {
			logging.Errorf("Failed to stop CNI server: %v", stopErr)
			err = stopErr
		}
		s.cniServer = nil
	}

	s.running = false

	// 更新健康状态
	healthMgr := GetGlobalHealthManager()
	healthMgr.UpdateServiceStatus(s.Name(), false, err)

	logging.Infof("CNI service stopped")
	return err
}

func (s *CNIService) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// createCNIServerWithRouteValidation 创建带有路由验证的 CNI 服务器
func (s *CNIService) createCNIServerWithRouteValidation() *cni.Server {
	return cni.NewServerWithCallbacks(
		constants.DefaultSocketPath,
		s.handleAllocateWithValidation, // allocate 回调
		s.handleReleaseWithValidation,  // release 回调
		s.handleStatusWithValidation,   // status 回调
		s.handlePodReadyWithValidation, // pod_ready 回调
	)
}

// handleAllocateWithValidation 处理分配请求并验证路由
func (s *CNIService) handleAllocateWithValidation(req *cni.CNIRequest) *cni.CNIResponse {
	logging.Infof("CNI allocate request: namespace=%s, pod=%s, localPool=%s",
		req.Namespace, req.PodName, req.LocalPool)

	// 如果请求包含 local pool CIDR，验证路由状态
	if req.LocalPool != "" {
		if err := s.validateRouteStatus(req.LocalPool); err != nil {
			logging.Warnf("Route validation failed for CIDR %s: %v", req.LocalPool, err)
			return &cni.CNIResponse{
				Success: false,
				Error:   fmt.Sprintf("route validation failed: %v", err),
			}
		}
	}

	// 执行默认的分配逻辑
	return &cni.CNIResponse{
		Success: true,
	}
}

// handleReleaseWithValidation 处理释放请求
func (s *CNIService) handleReleaseWithValidation(req *cni.CNIRequest) *cni.CNIResponse {
	logging.Infof("CNI release request: namespace=%s, pod=%s", req.Namespace, req.PodName)
	// 执行默认的释放逻辑
	return &cni.CNIResponse{Success: true}
}

// handleStatusWithValidation 处理状态请求
func (s *CNIService) handleStatusWithValidation(req *cni.CNIRequest) *cni.CNIResponse {
	logging.Infof("CNI status request: namespace=%s, pod=%s", req.Namespace, req.PodName)

	// 执行默认的状态查询逻辑
	return &cni.CNIResponse{
		Success: true,
		Data: map[string]interface{}{
			"status": "ready",
		},
	}
}

// handlePodReadyWithValidation 处理 Pod 就绪请求并验证路由
func (s *CNIService) handlePodReadyWithValidation(req *cni.CNIRequest) *cni.CNIResponse {
	logging.Infof("CNI pod_ready request: namespace=%s, pod=%s, localPool=%s",
		req.Namespace, req.PodName, req.LocalPool)

	// 如果请求包含 local pool CIDR，验证路由状态
	if req.LocalPool != "" {
		if err := s.validateRouteStatus(req.LocalPool); err != nil {
			logging.Warnf("Route validation failed for CIDR %s: %v", req.LocalPool, err)
			// Pod 就绪时路由验证失败不应该阻止操作，只记录警告
		}
	}

	// 执行默认的 Pod 就绪逻辑
	return &cni.CNIResponse{
		Success: true,
		Data: map[string]interface{}{
			"ready": true,
		},
	}
}

// validateRouteStatus 验证路由状态，如果未开启则自动开启
func (s *CNIService) validateRouteStatus(podLocalCIDR string) error {
	logging.Infof("Validating route status for CIDR: %s", podLocalCIDR)

	// 1. 检查 Tailscale 是否已应用该路由
	tailscaleOK, err := s.checkTailscaleRouteStatus(podLocalCIDR)
	if err != nil {
		return fmt.Errorf("failed to check Tailscale route status: %v", err)
	}

	// 2. 检查 Headscale 云端是否已开启该路由
	headscaleOK, err := s.checkHeadscaleRouteStatus(podLocalCIDR)
	if err != nil {
		return fmt.Errorf("failed to check Headscale route status: %v", err)
	}

	// 3. 如果 Tailscale 路由未应用，尝试应用
	if !tailscaleOK {
		logging.Infof("Tailscale route not applied for CIDR: %s, attempting to apply...", podLocalCIDR)
		if err := s.applyTailscaleRoute(podLocalCIDR); err != nil {
			logging.Warnf("Failed to auto-apply Tailscale route: %v", err)
		} else {
			tailscaleOK = true
			logging.Infof("Successfully applied Tailscale route for CIDR: %s", podLocalCIDR)
		}
	}
	time.Sleep(2 * time.Second)
	// 4. 如果 Headscale 路由未开启，尝试自动开启
	if !headscaleOK {
		logging.Infof("Headscale route not enabled for CIDR: %s, attempting to enable...", podLocalCIDR)
		if err := s.enableHeadscaleRoute(podLocalCIDR); err != nil {
			logging.Warnf("Failed to auto-enable Headscale route: %v", err)
		} else {
			headscaleOK = true
			logging.Infof("Successfully enabled Headscale route for CIDR: %s", podLocalCIDR)
		}
	}
	// 5. 最终验证结果
	if !tailscaleOK && !headscaleOK {
		return fmt.Errorf("failed to configure routes for CIDR: %s after auto-attempts", podLocalCIDR)
	}

	logging.Infof("Route validation completed for CIDR: %s (Tailscale: %v, Headscale: %v)",
		podLocalCIDR, tailscaleOK, headscaleOK)
	return nil
}

// enableHeadscaleRoute 在 Headscale 中启用指定 CIDR 的路由
func (s *CNIService) enableHeadscaleRoute(podLocalCIDR string) error {
	logging.Infof("Enabling Headscale route for CIDR: %s", podLocalCIDR)

	// 获取 Headscale 客户端
	headscaleClient := s.preparer.GetHeadscaleClient()
	if headscaleClient == nil {
		return fmt.Errorf("Headscale client not available")
	}

	// 获取所有路由
	routesResp, err := headscaleClient.GetRoutes(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get routes from Headscale: %v", err)
	}

	// 查找匹配的路由
	var targetRoute *headscale.Route
	for _, route := range routesResp.Routes {
		if route.Prefix == podLocalCIDR {
			targetRoute = &route
			break
		}
	}

	if targetRoute == nil {
		return fmt.Errorf("route for CIDR %s not found in Headscale", podLocalCIDR)
	}

	// 如果路由已启用，直接返回
	if targetRoute.Enabled {
		logging.Infof("Route for CIDR %s is already enabled", podLocalCIDR)
		return nil
	}

	// 启用路由
	if err := headscaleClient.EnableRoute(context.Background(), targetRoute.ID); err != nil {
		return fmt.Errorf("failed to enable route %s: %v", targetRoute.ID, err)
	}

	logging.Infof("Successfully enabled Headscale route %s for CIDR: %s", targetRoute.ID, podLocalCIDR)
	return nil
}

// applyTailscaleRoute 在 Tailscale 中应用指定 CIDR 的路由（合并现有路由）
func (s *CNIService) applyTailscaleRoute(podLocalCIDR string) error {
	logging.Infof("Applying Tailscale route for CIDR: %s", podLocalCIDR)

	// 获取 Tailscale 客户端
	tailscaleClient := s.preparer.GetTailscaleClient()
	if tailscaleClient == nil {
		return fmt.Errorf("Tailscale client not available")
	}

	// 解析新的 CIDR
	newPrefix, err := netip.ParsePrefix(podLocalCIDR)
	if err != nil {
		return fmt.Errorf("invalid CIDR format %s: %v", podLocalCIDR, err)
	}

	// 获取当前已通告的路由
	prefs, err := tailscaleClient.GetPrefs(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get current preferences: %v", err)
	}

	// 检查路由是否已存在
	for _, existingRoute := range prefs.AdvertiseRoutes {
		if existingRoute.String() == podLocalCIDR {
			logging.Infof("Route %s already exists in Tailscale", podLocalCIDR)
			return nil
		}
	}

	// 合并现有路由和新路由
	mergedRoutes := make([]netip.Prefix, len(prefs.AdvertiseRoutes)+1)
	copy(mergedRoutes, prefs.AdvertiseRoutes)
	mergedRoutes[len(prefs.AdvertiseRoutes)] = newPrefix

	logging.Infof("Merging routes: existing %d routes + new route %s = total %d routes",
		len(prefs.AdvertiseRoutes), podLocalCIDR, len(mergedRoutes))

	// 应用合并后的路由
	if err := tailscaleClient.AdvertiseRoutes(context.Background(), mergedRoutes...); err != nil {
		return fmt.Errorf("failed to advertise merged routes: %v", err)
	}

	logging.Infof("Successfully applied Tailscale route for CIDR: %s (merged with existing routes)", podLocalCIDR)
	return nil
}

// checkTailscaleRouteStatus 检查 Tailscale 路由状态
func (s *CNIService) checkTailscaleRouteStatus(podLocalCIDR string) (bool, error) {
	tailscaleClient := s.preparer.GetTailscaleClient()
	if tailscaleClient == nil {
		return false, fmt.Errorf("tailscale client not available")
	}

	// 获取 Tailscale 偏好设置
	prefs, err := tailscaleClient.GetPrefs(context.Background())
	if err != nil {
		return false, fmt.Errorf("failed to get Tailscale preferences: %v", err)
	}

	// 检查是否在通告路由中
	for _, advertiseRoute := range prefs.AdvertiseRoutes {
		advertiseRouteStr := advertiseRoute.String()
		if strings.TrimSpace(advertiseRouteStr) == strings.TrimSpace(podLocalCIDR) {
			logging.Infof("Found advertised route %s in Tailscale", podLocalCIDR)
			return true, nil
		}
	}

	logging.Debugf("Route %s not found in Tailscale advertised routes", podLocalCIDR)
	return false, nil
}

// checkHeadscaleRouteStatus 检查 Headscale 路由状态
func (s *CNIService) checkHeadscaleRouteStatus(podLocalCIDR string) (bool, error) {
	headscaleClient := s.preparer.GetHeadscaleClient()
	if headscaleClient == nil {
		return false, fmt.Errorf("headscale client not available")
	}

	// 获取所有路由
	routes, err := headscaleClient.GetRoutes(context.Background())
	if err != nil {
		return false, fmt.Errorf("failed to get Headscale routes: %v", err)
	}

	// 查找匹配的路由并检查是否启用
	for _, route := range routes.Routes {
		if strings.TrimSpace(route.Prefix) == strings.TrimSpace(podLocalCIDR) {
			if route.Enabled {
				logging.Infof("Found enabled route %s in Headscale (ID: %s)", podLocalCIDR, route.ID)
				return true, nil
			} else {
				logging.Warnf("Found route %s in Headscale but not enabled (ID: %s)", podLocalCIDR, route.ID)
				return false, nil
			}
		}
	}

	logging.Debugf("Route %s not found in Headscale", podLocalCIDR)
	return false, nil
}
