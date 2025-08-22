package daemon

import (
	"context"
	"fmt"
	"os"
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
	// 每 2 分钟检查一次 Headscale API 状态
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	// 启动时立即执行一次检查
	s.performHealthCheck(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.performHealthCheck(ctx)
		}
	}
}

func (s *HeadscaleHealthService) performHealthCheck(ctx context.Context) {
	// 1. 检查 Headscale API 是否有效
	headscaleClient := s.preparer.GetHeadscaleClient()
	if headscaleClient == nil {
		logging.Errorf("Headscale client not available")
		healthMgr := GetGlobalHealthManager()
		healthMgr.UpdateServiceStatus(s.Name(), false, fmt.Errorf("headscale client not available"))
		return
	}

	// 获取路由列表验证 API 有效性
	routesResp, err := headscaleClient.GetRoutes(ctx)
	if err != nil {
		logging.Errorf("Headscale API health check failed: %v", err)
		healthMgr := GetGlobalHealthManager()
		healthMgr.UpdateServiceStatus(s.Name(), false, err)
		return
	}

	// 2. 检查路由冲突
	if err := s.checkRouteConflicts(ctx, routesResp.Routes); err != nil {
		logging.Warnf("Route conflict detected: %v", err)
		// 路由冲突不影响健康状态，但需要记录
	}

	// 3. 检查本机路由冲突
	if err := s.checkLocalRouteConflicts(ctx, routesResp.Routes); err != nil {
		logging.Warnf("Local route conflict detected: %v", err)
		// 本地路由冲突不影响健康状态，但需要记录
	}

	// API 正常，更新健康状态为成功
	healthMgr := GetGlobalHealthManager()
	healthMgr.UpdateServiceStatus(s.Name(), true, nil)

	logging.Debugf("Headscale API health check passed - GetRoutes successful")
}

// checkRouteConflicts 检查 Headscale 中的路由冲突
func (s *HeadscaleHealthService) checkRouteConflicts(ctx context.Context, routes []headscale.Route) error {
	if len(routes) < 2 {
		return nil // 少于2个路由不会有冲突
	}

	var conflicts []string

	// 检查路由前缀冲突
	for i, route1 := range routes {
		for j, route2 := range routes {
			if i >= j {
				continue
			}

			// 检查路由前缀是否重叠
			if s.isRoutePrefixOverlap(route1.Prefix, route2.Prefix) {
				conflict := fmt.Sprintf("路由冲突: %s (ID: %s) 与 %s (ID: %s) 前缀重叠",
					route1.Prefix, route1.ID, route2.Prefix, route2.ID)
				conflicts = append(conflicts, conflict)
			}
		}
	}

	if len(conflicts) > 0 {
		return fmt.Errorf("检测到 %d 个路由冲突: %s", len(conflicts), strings.Join(conflicts, "; "))
	}

	return nil
}

// checkLocalRouteConflicts 检查本机在云端开启的路由冲突
func (s *HeadscaleHealthService) checkLocalRouteConflicts(ctx context.Context, headscaleRoutes []headscale.Route) error {
	var conflicts []string

	// 1. 获取本机节点信息来识别本机路由
	localNodeInfo, err := s.getLocalNodeInfo(ctx)
	if err != nil {
		return fmt.Errorf("无法获取本机节点信息: %v", err)
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

	// 3. 检查本机路由是否与其他机器路由冲突
	for _, localRoute := range localRoutes {
		for _, otherRoute := range otherMachineRoutes {
			if s.isRoutePrefixOverlap(localRoute.Prefix, otherRoute.Prefix) {
				conflict := fmt.Sprintf("本机路由 %s (ID: %s) 与其他机器路由 %s (ID: %s, 机器: %s) 冲突",
					localRoute.Prefix, localRoute.ID, otherRoute.Prefix, otherRoute.ID, otherRoute.Node.Name)
				conflicts = append(conflicts, conflict)
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
			}
		}
	}

	if len(conflicts) > 0 {
		return fmt.Errorf("检测到 %d 个路由冲突: %s", len(conflicts), strings.Join(conflicts, "; "))
	}

	return nil
}

// getLocalNodeInfo 获取本机节点信息
func (s *HeadscaleHealthService) getLocalNodeInfo(ctx context.Context) (*headscale.Node, error) {
	// 方法1: 通过本机主机名查找节点
	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("无法获取主机名: %v", err)
	}

	// 获取所有节点列表
	headscaleClient := s.preparer.GetHeadscaleClient()
	if headscaleClient == nil {
		return nil, fmt.Errorf("headscale client not available")
	}

	nodesResp, err := headscaleClient.ListNodes(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("无法获取节点列表: %v", err)
	}

	// 通过主机名匹配本机节点
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
	// 简单的字符串前缀检查
	// 例如: 10.0.0.0/8 与 10.1.0.0/16 重叠
	// 或者: 10.0.0.0/8 与 10.0.0.0/24 重叠

	// 移除 CIDR 后缀进行比较
	base1 := strings.Split(prefix1, "/")[0]
	base2 := strings.Split(prefix2, "/")[0]

	// 检查一个是否是另一个的前缀
	return strings.HasPrefix(base1, base2) || strings.HasPrefix(base2, base1)
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
