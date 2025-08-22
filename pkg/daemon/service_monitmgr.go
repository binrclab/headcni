package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/binrclab/headcni/pkg/constants"
	"github.com/binrclab/headcni/pkg/logging"
	"github.com/binrclab/headcni/pkg/monitoring"
)

// MonitoringService 监控服务，实现 Service 接口
type MonitoringService struct {
	preparer   *Preparer
	httpServer *http.Server
	running    bool
	startTime  time.Time
	mu         sync.RWMutex
}

// NewMonitoringService 创建新的监控服务
func NewMonitoringService(preparer *Preparer) *MonitoringService {
	return &MonitoringService{
		preparer: preparer,
	}
}

// Name 返回服务名称 (Service 接口)
func (s *MonitoringService) Name() string {
	return constants.ServiceNameMonitoring
}

// Start 启动监控服务 (Service 接口)
func (s *MonitoringService) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	// 创建并启动 HTTP 服务器
	if err := s.startHTTPServer(); err != nil {
		// 更新健康状态为失败
		healthMgr := GetGlobalHealthManager()
		healthMgr.UpdateServiceStatus(s.Name(), false, err)
		return fmt.Errorf("failed to start HTTP server: %v", err)
	}

	s.running = true
	s.startTime = time.Now()

	// 更新健康状态为成功
	healthMgr := GetGlobalHealthManager()
	healthMgr.UpdateServiceStatus(s.Name(), true, nil)

	logging.Infof("Monitoring service started successfully on port %d with /health and /metrics endpoints", s.getPort())
	return nil
}

// Stop 停止监控服务 (Service 接口)
func (s *MonitoringService) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	// 停止 HTTP 服务器
	var err error
	if s.httpServer != nil {
		if shutdownErr := s.httpServer.Shutdown(ctx); shutdownErr != nil {
			logging.Errorf("Failed to stop HTTP server: %v", shutdownErr)
			err = shutdownErr
		}
	}

	s.running = false

	// 更新健康状态
	healthMgr := GetGlobalHealthManager()
	healthMgr.UpdateServiceStatus(s.Name(), false, err)

	logging.Infof("Monitoring service stopped")
	return err
}

// Reload 重载监控服务 (Service 接口)
func (s *MonitoringService) Reload(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	logging.Infof("Reloading monitoring service")

	if !s.running {
		return fmt.Errorf("service is not running")
	}

	// MonitoringService 直接使用传入的配置，不需要从 preparer 获取
	// 这里假设配置已经通过其他方式更新了
	newConfig := s.preparer.GetConfig()
	if newConfig == nil {
		return fmt.Errorf("failed to get configuration")
	}
	oldConfig := s.preparer.GetOldConfig()
	// 检查监控配置变更
	configChanged := false
	if oldConfig != nil {
		if newConfig.Monitoring.Port != oldConfig.Monitoring.Port ||
			newConfig.Monitoring.Enabled != oldConfig.Monitoring.Enabled {
			configChanged = true
		}
	}

	if !configChanged {
		logging.Infof("Monitoring configuration unchanged, no reload needed")
		return nil
	}

	logging.Infof("Monitoring configuration changed, performing reload")

	// 停止当前服务
	if err := s.Stop(ctx); err != nil {
		logging.Errorf("Failed to stop service during reload: %v", err)
	}

	// 重新启动服务
	if err := s.Start(ctx); err != nil {
		logging.Errorf("Failed to restart service during reload: %v", err)
		return err
	}

	logging.Infof("Monitoring service reloaded successfully")
	return nil
}

// IsRunning 检查服务是否正在运行 (Service 接口)
func (s *MonitoringService) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// getPort 获取端口号
func (s *MonitoringService) getPort() int {
	port := s.preparer.GetConfig().Monitoring.Port
	if port == 0 {
		port = 9001 // 默认端口
	}
	return port
}

// startHTTPServer 启动 HTTP 服务器
func (s *MonitoringService) startHTTPServer() error {
	port := s.getPort()

	mux := http.NewServeMux()

	// 健康检查端点
	mux.HandleFunc("/health", s.handleHealth)

	// Prometheus 指标端点
	mux.Handle("/metrics", monitoring.GetPrometheusHandler())

	s.httpServer = &http.Server{
		Addr:    ":" + strconv.Itoa(port),
		Handler: mux,
	}

	// 在后台启动服务器
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logging.Errorf("HTTP server error: %v", err)
		}
	}()

	logging.Infof("HTTP server started on port %d with /health and /metrics endpoints", port)
	return nil
}

// handleHealth 处理健康检查请求
func (s *MonitoringService) handleHealth(w http.ResponseWriter, r *http.Request) {
	// 使用全局健康管理器获取整体健康状态
	healthMgr := GetGlobalHealthManager()
	health := healthMgr.GetHealthStatus()

	w.Header().Set("Content-Type", "application/json")

	// 根据健康状态设置 HTTP 状态码
	if health.Status == "healthy" {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	json.NewEncoder(w).Encode(health)
}

// GetMetrics 获取监控指标（对外接口）
func (s *MonitoringService) GetMetrics() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	metrics := map[string]interface{}{
		"service_name": s.Name(),
		"running":      s.running,
		"port":         s.getPort(),
		"enabled":      s.preparer.GetConfig().Monitoring.Enabled,
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}

	if s.running {
		metrics["uptime_seconds"] = time.Since(s.startTime).Seconds()
		metrics["start_time"] = s.startTime.UTC().Format(time.RFC3339)
	}

	return metrics
}
