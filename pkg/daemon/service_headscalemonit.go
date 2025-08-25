package daemon

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/binrclab/headcni/pkg/constants"
	"github.com/binrclab/headcni/pkg/headscale"
	"github.com/binrclab/headcni/pkg/logging"
)

// HeadscaleHealthService Headscale 健康监听服务
type HeadscaleHealthService struct {
	preparer *Preparer
	running  bool
	mu       sync.RWMutex

	// 健康检查统计信息
	totalChecks         int64
	successfulChecks    int64
	failedChecks        int64
	lastCheckTime       time.Time
	lastError           string
	consecutiveFailures int
}

// NewHeadscaleHealthService 创建新的 Headscale 健康服务
func NewHeadscaleHealthService(preparer *Preparer) *HeadscaleHealthService {
	return &HeadscaleHealthService{preparer: preparer}
}

func (s *HeadscaleHealthService) Name() string { return constants.ServiceNameHeadscaleHealth }

func (s *HeadscaleHealthService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	s.running = true

	// 启动健康检查协程
	go s.healthCheckLoop(ctx)

	// 更新健康状态为成功
	healthMgr := GetGlobalHealthManager()
	healthMgr.UpdateServiceStatus(s.Name(), true, nil)

	logging.Infof("Headscale health service started")
	return nil
}

func (s *HeadscaleHealthService) Reload(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	logging.Infof("Reloading Headscale health service")

	if !s.running {
		return fmt.Errorf("service is not running")
	}

	// 获取新的配置
	newConfig := s.preparer.GetConfig()
	if newConfig == nil {
		return fmt.Errorf("failed to get new configuration")
	}

	// 检查 Headscale 配置变更
	configChanged := false
	oldConfig := s.preparer.GetOldConfig()
	if oldConfig != nil {
		if newConfig.Headscale.URL != oldConfig.Headscale.URL ||
			newConfig.Headscale.Timeout != oldConfig.Headscale.Timeout ||
			newConfig.Monitoring.Enabled != oldConfig.Monitoring.Enabled {
			configChanged = true
		}
	}

	if !configChanged {
		logging.Infof("Headscale health monitoring configuration unchanged, no reload needed")
		return nil
	}

	logging.Infof("Headscale health monitoring configuration changed, performing reload")

	// 停止当前服务
	if err := s.Stop(ctx); err != nil {
		logging.Errorf("Failed to stop service during reload: %v", err)
	}

	// 重新启动服务
	if err := s.Start(ctx); err != nil {
		logging.Errorf("Failed to restart service during reload: %v", err)
		return err
	}

	logging.Infof("Headscale health service reloaded successfully")
	return nil
}

func (s *HeadscaleHealthService) healthCheckLoop(ctx context.Context) {
	// 健康检查间隔，默认5分钟
	checkInterval := 5 * time.Minute

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	logging.Infof("Starting Headscale health check loop with interval: %v", checkInterval)

	// 启动时立即执行一次检查
	if err := s.performHealthCheck(ctx); err != nil {
		logging.Warnf("Initial health check failed: %v", err)
	}

	maxConsecutiveFailures := 3

	for {
		select {
		case <-ctx.Done():
			logging.Infof("Health check loop stopped due to context cancellation")
			return
		case <-ticker.C:
			if err := s.performHealthCheck(ctx); err != nil {
				s.mu.Lock()
				currentFailures := s.consecutiveFailures
				s.mu.Unlock()

				logging.Warnf("Health check failed (consecutive: %d/%d): %v", currentFailures, maxConsecutiveFailures, err)

				// 如果连续失败次数过多，记录错误但不停止服务
				if currentFailures >= maxConsecutiveFailures {
					logging.Errorf("Too many consecutive health check failures (%d), but continuing to monitor", currentFailures)
				}
			} else {
				s.mu.Lock()
				if s.consecutiveFailures > 0 {
					logging.Infof("Health check recovered after %d consecutive failures", s.consecutiveFailures)
				}
				s.mu.Unlock()
			}
		}
	}
}

func (s *HeadscaleHealthService) performHealthCheck(ctx context.Context) error {
	s.mu.Lock()
	s.totalChecks++
	s.lastCheckTime = time.Now()
	s.mu.Unlock()

	// 1. 检查 Headscale API 是否有效
	headscaleClient := s.preparer.GetHeadscaleClient()
	if headscaleClient == nil {
		err := fmt.Errorf("headscale client not available")
		s.updateHealthCheckStats(false, err)
		healthMgr := GetGlobalHealthManager()
		healthMgr.UpdateServiceStatus(s.Name(), false, err)
		return err
	}

	// 获取路由列表验证 API 有效性
	routesResp, err := headscaleClient.GetRoutes(ctx)
	if err != nil {
		s.updateHealthCheckStats(false, err)
		healthMgr := GetGlobalHealthManager()
		healthMgr.UpdateServiceStatus(s.Name(), false, err)
		return err
	}

	var hostname string
	if s.preparer.k8sClient != nil && s.preparer.GetConfig().Tailscale.Mode == "host" {
		hostname, err = s.preparer.k8sClient.GetCurrentNodeName()
		if err != nil {
			s.updateHealthCheckStats(false, err)
			logging.Errorf("Failed to get current node name: %v", err)
			healthMgr := GetGlobalHealthManager()
			healthMgr.UpdateServiceStatus(s.Name(), false, err)
			return err
		}
	} else {
		if hostnameBytes, err := os.ReadFile(filepath.Dir(s.preparer.GetConfig().Tailscale.Socket.Path) + "/hostname"); err == nil {
			hostname = string(hostnameBytes)
		}
	}
	// 2. 检查路由冲突
	if err := s.checkLocalRouteConflicts(ctx, routesResp.Routes, hostname); err != nil {
		logging.Warnf("Local route conflict detected: %v", err)
		// 本地路由冲突不影响健康状态，但需要记录
	}

	// API 正常，更新健康状态为成功
	s.updateHealthCheckStats(true, nil)
	healthMgr := GetGlobalHealthManager()
	healthMgr.UpdateServiceStatus(s.Name(), true, nil)

	logging.Debugf("Headscale API health check passed - GetRoutes successful")
	return nil
}

// updateHealthCheckStats 更新健康检查统计信息
func (s *HeadscaleHealthService) updateHealthCheckStats(success bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if success {
		s.successfulChecks++
		s.consecutiveFailures = 0
		s.lastError = ""
	} else {
		s.failedChecks++
		s.consecutiveFailures++
		if err != nil {
			s.lastError = err.Error()
		}
	}
}

// checkLocalRouteConflicts 检查本机在云端开启的路由冲突
func (s *HeadscaleHealthService) checkLocalRouteConflicts(ctx context.Context, headscaleRoutes []headscale.Route, hostname string) error {
	var conflicts []string
	var conflictDetails []map[string]interface{}

	// 1. 获取本机节点信息来识别本机路由
	localNodeInfo, err := s.getLocalNodeInfoByHostname(ctx, hostname)
	if err != nil {
		logging.Warnf("无法获取本机节点信息，跳过本地路由冲突检查: %v", err)
		return nil // 不返回错误，避免影响健康检查
	}

	// 2. 分离本机路由和其他机器路由
	var localRoutes []headscale.Route
	var otherMachineRoutes []headscale.Route

	for _, route := range headscaleRoutes {
		if s.isLocalRoute(route, localNodeInfo) {
			localRoutes = append(localRoutes, route)
		} else {
			otherMachineRoutes = append(otherMachineRoutes, route)
		}
	}

	logging.Debugf("路由分析: 本机路由 %d 个，其他机器路由 %d 个", len(localRoutes), len(otherMachineRoutes))

	// 3. 检查本机路由是否与其他机器路由冲突
	for _, localRoute := range localRoutes {
		for _, otherRoute := range otherMachineRoutes {
			if s.isRoutePrefixOverlap(localRoute.Prefix, otherRoute.Prefix) {
				conflict := fmt.Sprintf("本机路由 %s (ID: %s) 与其他机器路由 %s (ID: %s, 机器: %s) 冲突",
					localRoute.Prefix, localRoute.ID, otherRoute.Prefix, otherRoute.ID, otherRoute.Node.Name)
				conflicts = append(conflicts, conflict)

				// 记录详细冲突信息
				conflictDetail := map[string]interface{}{
					"local_route_prefix": localRoute.Prefix,
					"local_route_id":     localRoute.ID,
					"other_route_prefix": otherRoute.Prefix,
					"other_route_id":     otherRoute.ID,
					"other_node_name":    otherRoute.Node.Name,
					"conflict_type":      "local_vs_remote",
				}
				conflictDetails = append(conflictDetails, conflictDetail)
			}
		}
	}

	// 4. 检查本机自己的多个路由是否有重复
	for i, route1 := range localRoutes {
		for j, route2 := range localRoutes {
			if i >= j {
				continue
			}

			if s.isRoutePrefixOverlap(route1.Prefix, route2.Prefix) {
				conflict := fmt.Sprintf("本机路由重复: %s (ID: %s) 与 %s (ID: %s) 前缀重叠",
					route1.Prefix, route1.ID, route2.Prefix, route2.ID)
				conflicts = append(conflicts, conflict)

				// 记录详细冲突信息
				conflictDetail := map[string]interface{}{
					"route1_prefix": route1.Prefix,
					"route1_id":     route1.ID,
					"route2_prefix": route2.Prefix,
					"route2_id":     route2.ID,
					"conflict_type": "local_duplicate",
				}
				conflictDetails = append(conflictDetails, conflictDetail)
			}
		}
	}

	if len(conflicts) > 0 {
		// 记录详细的冲突信息
		logging.Warnf("检测到 %d 个本地路由冲突，详细信息: %+v", len(conflicts), conflictDetails)
		return fmt.Errorf("检测到 %d 个本地路由冲突: %s", len(conflicts), strings.Join(conflicts, "; "))
	}

	return nil
}

// 获取当前主机名对于的节点信息
func (s *HeadscaleHealthService) getLocalNodeInfoByHostname(ctx context.Context, hostname string) (*headscale.Node, error) {
	headscaleClient := s.preparer.GetHeadscaleClient()
	if headscaleClient == nil {
		return nil, fmt.Errorf("headscale client not available")
	}

	nodesResp, err := headscaleClient.ListNodes(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("无法获取节点列表: %v", err)
	}

	for _, node := range nodesResp.Nodes {
		if node.Name == hostname {
			return &node, nil
		}
	}

	return nil, fmt.Errorf("未找到匹配的节点信息，主机名: %s", hostname)
}

// isLocalRoute 判断路由是否属于本机
func (s *HeadscaleHealthService) isLocalRoute(route headscale.Route, localNode *headscale.Node) bool {
	// 通过节点ID判断路由归属
	return route.Node.ID == localNode.ID
}

// isRoutePrefixOverlap 检查两个路由前缀是否重叠
func (s *HeadscaleHealthService) isRoutePrefixOverlap(prefix1, prefix2 string) bool {
	// 改进的 CIDR 重叠检测算法
	// 使用更精确的 IP 网络计算而不是简单的字符串前缀比较

	// 解析 CIDR 前缀
	ip1, ipNet1, err := net.ParseCIDR(prefix1)
	if err != nil {
		logging.Warnf("Invalid CIDR prefix 1: %s, error: %v", prefix1, err)
		return false
	}

	ip2, ipNet2, err := net.ParseCIDR(prefix2)
	if err != nil {
		logging.Warnf("Invalid CIDR prefix 2: %s, error: %v", prefix2, err)
		return false
	}

	// 检查网络是否重叠
	// 一个网络包含另一个网络的起始IP，或者两个网络有交集
	return ipNet1.Contains(ip2) || ipNet2.Contains(ip1)
}

func (s *HeadscaleHealthService) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.running = false

	// 更新健康状态
	healthMgr := GetGlobalHealthManager()
	healthMgr.UpdateServiceStatus(s.Name(), false, nil)

	logging.Infof("Headscale health service stopped")
	return nil
}

func (s *HeadscaleHealthService) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// =============================================================================
// Health Check Statistics and Metrics
// =============================================================================

// HealthCheckStats 健康检查统计信息
type HealthCheckStats struct {
	TotalChecks         int64     `json:"total_checks"`
	SuccessfulChecks    int64     `json:"successful_checks"`
	FailedChecks        int64     `json:"failed_checks"`
	LastCheckTime       time.Time `json:"last_check_time"`
	LastError           string    `json:"last_error,omitempty"`
	ConsecutiveFailures int       `json:"consecutive_failures"`
}

// GetHealthCheckStats 获取健康检查统计信息
func (s *HeadscaleHealthService) GetHealthCheckStats() *HealthCheckStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return &HealthCheckStats{
		TotalChecks:         s.totalChecks,
		SuccessfulChecks:    s.successfulChecks,
		FailedChecks:        s.failedChecks,
		LastCheckTime:       s.lastCheckTime,
		LastError:           s.lastError,
		ConsecutiveFailures: s.consecutiveFailures,
	}
}

// =============================================================================
// Utility Functions
// =============================================================================

// validateRoutePrefix 验证路由前缀格式
func (s *HeadscaleHealthService) validateRoutePrefix(prefix string) error {
	if prefix == "" {
		return fmt.Errorf("route prefix cannot be empty")
	}

	// 验证 CIDR 格式
	_, _, err := net.ParseCIDR(prefix)
	if err != nil {
		return fmt.Errorf("invalid CIDR format '%s': %v", prefix, err)
	}

	return nil
}

// formatConflictSummary 格式化冲突摘要
func (s *HeadscaleHealthService) formatConflictSummary(conflicts []string) string {
	if len(conflicts) == 0 {
		return "无路由冲突"
	}

	if len(conflicts) == 1 {
		return fmt.Sprintf("1 个路由冲突: %s", conflicts[0])
	}

	return fmt.Sprintf("%d 个路由冲突: %s", len(conflicts), strings.Join(conflicts, "; "))
}
