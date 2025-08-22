package health

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/binrclab/headcni/pkg/backend/tailscale"
	"github.com/binrclab/headcni/pkg/ipam"
	"github.com/binrclab/headcni/pkg/networking"

	"k8s.io/klog/v2"
)

// Config 健康检查器配置
type Config struct {
	Port                    string        `json:"port"`
	HealthCheckInterval     time.Duration `json:"healthCheckInterval"`
	HealthCheckTimeout      time.Duration `json:"healthCheckTimeout"`
	ReadinessTimeout        time.Duration `json:"readinessTimeout"`
	LivenessTimeout         time.Duration `json:"livenessTimeout"`
	MaxConsecutiveFailures  int           `json:"maxConsecutiveFailures"`
	RecoveryTimeout         time.Duration `json:"recoveryTimeout"`
	TailscaleRestartTimeout time.Duration `json:"tailscaleRestartTimeout"`
	EnableMetrics           bool          `json:"enableMetrics"`
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Port:                    ":8081",
		HealthCheckInterval:     30 * time.Second,
		HealthCheckTimeout:      15 * time.Second,
		ReadinessTimeout:        10 * time.Second,
		LivenessTimeout:         5 * time.Second,
		MaxConsecutiveFailures:  3,
		RecoveryTimeout:         60 * time.Second,
		TailscaleRestartTimeout: 30 * time.Second,
		EnableMetrics:           true,
	}
}

// HealthChecker 健康检查器
type HealthChecker struct {
	ipamManager     *ipam.IPAMManager
	networkMgr      *networking.NetworkManager
	tailscaleClient *tailscale.SimpleClient
	httpServer      *http.Server
	config          *Config

	// 状态管理
	statusMutex         sync.RWMutex
	lastHealthCheck     time.Time
	consecutiveFailures int32
	isRecovering        int32
	lastRecoveryAttempt time.Time

	// 优雅关闭
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// 指标
	metrics *HealthMetrics
}

// HealthMetrics 健康检查指标
type HealthMetrics struct {
	TotalChecks       int64         `json:"totalChecks"`
	SuccessfulChecks  int64         `json:"successfulChecks"`
	FailedChecks      int64         `json:"failedChecks"`
	RecoveryAttempts  int64         `json:"recoveryAttempts"`
	LastCheckDuration time.Duration `json:"lastCheckDuration"`
	Uptime            time.Duration `json:"uptime"`
	startTime         time.Time
}

// NewHealthMetrics 创建新的指标收集器
func NewHealthMetrics() *HealthMetrics {
	return &HealthMetrics{
		startTime: time.Now(),
	}
}

// HealthCheck 健康检查项
type HealthCheck struct {
	Name     string
	Function func(context.Context) error
	Timeout  time.Duration
}

// HealthStatus 健康状态
type HealthStatus struct {
	Status              string            `json:"status"`
	Timestamp           time.Time         `json:"timestamp"`
	Uptime              time.Duration     `json:"uptime"`
	ConsecutiveFailures int32             `json:"consecutiveFailures"`
	LastCheckDuration   time.Duration     `json:"lastCheckDuration"`
	Checks              map[string]string `json:"checks"`
	Metrics             *HealthMetrics    `json:"metrics,omitempty"`
}

// NewHealthChecker 创建新的健康检查器
func NewHealthChecker(ipamMgr *ipam.IPAMManager, netMgr *networking.NetworkManager, tailscaleClient *tailscale.SimpleClient, config *Config) *HealthChecker {
	if config == nil {
		config = DefaultConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	hc := &HealthChecker{
		ipamManager:     ipamMgr,
		networkMgr:      netMgr,
		tailscaleClient: tailscaleClient,
		config:          config,
		ctx:             ctx,
		cancel:          cancel,
		metrics:         NewHealthMetrics(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", hc.healthzHandler)
	mux.HandleFunc("/readyz", hc.readyzHandler)
	mux.HandleFunc("/livez", hc.livezHandler)
	if config.EnableMetrics {
		mux.HandleFunc("/metrics", hc.metricsHandler)
	}

	hc.httpServer = &http.Server{
		Addr:         config.Port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return hc
}

// Start 启动健康检查器
func (hc *HealthChecker) Start() error {
	klog.Info("Starting health checker...")

	// 启动定期健康检查
	hc.wg.Add(1)
	go func() {
		defer hc.wg.Done()
		hc.periodicHealthCheck()
	}()

	// 启动 HTTP 健康检查服务
	return hc.httpServer.ListenAndServe()
}

// Stop 停止健康检查器
func (hc *HealthChecker) Stop() error {
	klog.Info("Stopping health checker...")

	// 取消上下文
	hc.cancel()

	// 优雅关闭 HTTP 服务器
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := hc.httpServer.Shutdown(ctx); err != nil {
		klog.Errorf("Failed to shutdown HTTP server: %v", err)
	}

	// 等待所有 goroutine 完成
	hc.wg.Wait()

	klog.Info("Health checker stopped")
	return nil
}

// healthzHandler 健康检查处理器
func (hc *HealthChecker) healthzHandler(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), hc.config.HealthCheckTimeout)
	defer cancel()

	checks := []HealthCheck{
		{"tailscale", hc.checkTailscale, 5 * time.Second},
		{"ipam", hc.checkIPAM, 5 * time.Second},
		{"network", hc.checkNetwork, 5 * time.Second},
	}

	allHealthy := true
	results := make(map[string]string)

	// 并发执行健康检查
	var wg sync.WaitGroup
	resultChan := make(chan struct {
		name   string
		result string
		err    error
	}, len(checks))

	for _, check := range checks {
		wg.Add(1)
		go func(check HealthCheck) {
			defer wg.Done()

			checkCtx, checkCancel := context.WithTimeout(ctx, check.Timeout)
			defer checkCancel()

			if err := check.Function(checkCtx); err != nil {
				resultChan <- struct {
					name   string
					result string
					err    error
				}{check.Name, fmt.Sprintf("ERROR: %v", err), err}
			} else {
				resultChan <- struct {
					name   string
					result string
					err    error
				}{check.Name, "OK", nil}
			}
		}(check)
	}

	// 等待所有检查完成
	wg.Wait()
	close(resultChan)

	// 收集结果
	for result := range resultChan {
		if result.err != nil {
			allHealthy = false
			results[result.name] = result.result
			klog.Warningf("Health check %s failed: %v", result.name, result.err)
		} else {
			results[result.name] = result.result
		}
	}

	// 更新指标
	atomic.AddInt64(&hc.metrics.TotalChecks, 1)
	if allHealthy {
		atomic.AddInt64(&hc.metrics.SuccessfulChecks, 1)
	} else {
		atomic.AddInt64(&hc.metrics.FailedChecks, 1)
	}

	hc.metrics.LastCheckDuration = time.Since(start)
	hc.updateLastHealthCheck()

	w.Header().Set("Content-Type", "application/json")

	if allHealthy {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	// 返回详细的检查结果
	status := &HealthStatus{
		Status:              map[bool]string{true: "healthy", false: "unhealthy"}[allHealthy],
		Timestamp:           time.Now(),
		Uptime:              time.Since(hc.metrics.startTime),
		ConsecutiveFailures: atomic.LoadInt32(&hc.consecutiveFailures),
		LastCheckDuration:   hc.metrics.LastCheckDuration,
		Checks:              results,
	}

	if hc.config.EnableMetrics {
		status.Metrics = hc.metrics
	}

	json.NewEncoder(w).Encode(status)
}

// readyzHandler 就绪检查处理器
func (hc *HealthChecker) readyzHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), hc.config.ReadinessTimeout)
	defer cancel()

	if err := hc.checkReadiness(ctx); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ready")
}

// livezHandler 存活检查处理器
func (hc *HealthChecker) livezHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), hc.config.LivenessTimeout)
	defer cancel()

	if err := hc.checkLiveness(ctx); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "alive")
}

// metricsHandler 指标处理器
func (hc *HealthChecker) metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(hc.metrics)
}

// checkTailscale 检查 Tailscale 连接状态
func (hc *HealthChecker) checkTailscale(ctx context.Context) error {
	ipnState, err := hc.tailscaleClient.GetStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to get tailscale status: %v", err)
	}
	if ipnState.Self == nil {
		return fmt.Errorf("tailscale not connected")
	}
	return nil
}

// checkIPAM 检查 IPAM 服务状态
func (hc *HealthChecker) checkIPAM(ctx context.Context) error {
	return hc.ipamManager.HealthCheck(ctx)
}

// checkNetwork 检查基本网络功能
func (hc *HealthChecker) checkNetwork(ctx context.Context) error {
	// 1. 检查 tailscale0 接口
	iface, err := net.InterfaceByName("tailscale0")
	if err != nil {
		return fmt.Errorf("tailscale0 interface not found: %v", err)
	}

	// 检查接口状态
	if iface.Flags&net.FlagUp == 0 {
		return fmt.Errorf("tailscale0 interface is down")
	}

	// 2. 检查到 Tailscale IP 的连通性
	ipnState, err := hc.tailscaleClient.GetIP(ctx)
	if err != nil {
		return fmt.Errorf("failed to get tailscale IP: %v", err)
	}
	tailscaleIP := net.ParseIP(ipnState.String())
	// 3. 检查路由表
	if err := hc.checkTailscaleRoutes(); err != nil {
		return fmt.Errorf("tailscale routes check failed: %v", err)
	}

	// 4. 简单的连通性测试（非阻塞）
	go func() {
		conn, err := net.DialTimeout("tcp", tailscaleIP.String()+":0", 2*time.Second)
		if err == nil {
			conn.Close()
		}
	}()

	return nil
}

// checkTailscaleRoutes 检查 Tailscale 路由
func (hc *HealthChecker) checkTailscaleRoutes() error {
	cmd := exec.Command("ip", "route", "show", "table", "all")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get routes: %v", err)
	}

	// 检查是否有通过 tailscale0 的路由
	if !contains(string(output), "dev tailscale0") {
		return fmt.Errorf("no routes through tailscale0 interface")
	}

	return nil
}

// contains 检查字符串是否包含子字符串
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) && (s[:len(substr)] == substr ||
			s[len(s)-len(substr):] == substr ||
			containsSubstring(s, substr))))
}

// containsSubstring 检查字符串中间是否包含子字符串
func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// checkReadiness 检查就绪状态
func (hc *HealthChecker) checkReadiness(ctx context.Context) error {
	// 确保 Tailscale 已连接且状态正常
	if err := hc.checkTailscale(ctx); err != nil {
		return fmt.Errorf("tailscale not ready: %v", err)
	}

	// 确保 IPAM 服务可用
	if err := hc.checkIPAM(ctx); err != nil {
		return fmt.Errorf("IPAM not ready: %v", err)
	}

	// 检查是否正在恢复中
	if atomic.LoadInt32(&hc.isRecovering) == 1 {
		return fmt.Errorf("system is currently recovering")
	}

	return nil
}

// checkLiveness 检查存活状态
func (hc *HealthChecker) checkLiveness(ctx context.Context) error {
	// 检查 tailscaled 进程
	cmd := exec.CommandContext(ctx, "pgrep", "tailscaled")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tailscaled process not running")
	}

	// 检查健康检查器本身的状态
	if time.Since(hc.getLastHealthCheck()) > hc.config.HealthCheckInterval*2 {
		return fmt.Errorf("health checker appears to be stuck")
	}

	return nil
}

// periodicHealthCheck 定期健康检查
func (hc *HealthChecker) periodicHealthCheck() {
	ticker := time.NewTicker(hc.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			hc.runPeriodicCheck()
		case <-hc.ctx.Done():
			return
		}
	}
}

// runPeriodicCheck 执行定期检查
func (hc *HealthChecker) runPeriodicCheck() {
	ctx, cancel := context.WithTimeout(hc.ctx, hc.config.HealthCheckTimeout)
	defer cancel()

	healthy := true
	var errors []string

	// 检查 Tailscale
	if err := hc.checkTailscale(ctx); err != nil {
		klog.Errorf("Periodic Tailscale check failed: %v", err)
		healthy = false
		errors = append(errors, fmt.Sprintf("tailscale: %v", err))
	}

	// 检查 IPAM
	if err := hc.checkIPAM(ctx); err != nil {
		klog.Errorf("Periodic IPAM check failed: %v", err)
		healthy = false
		errors = append(errors, fmt.Sprintf("ipam: %v", err))
	}

	if healthy {
		atomic.StoreInt32(&hc.consecutiveFailures, 0)
	} else {
		failures := atomic.AddInt32(&hc.consecutiveFailures, 1)
		klog.Warningf("Health check failed %d consecutive times: %v", failures, errors)

		if failures >= int32(hc.config.MaxConsecutiveFailures) {
			hc.triggerRecovery()
		}
	}
}

// triggerRecovery 触发恢复流程
func (hc *HealthChecker) triggerRecovery() {
	// 防止重复触发恢复
	if !atomic.CompareAndSwapInt32(&hc.isRecovering, 0, 1) {
		klog.Info("Recovery already in progress, skipping...")
		return
	}

	hc.wg.Add(1)
	go func() {
		defer hc.wg.Done()
		defer atomic.StoreInt32(&hc.isRecovering, 0)

		hc.attemptRecovery()
	}()
}

// attemptRecovery 尝试恢复
func (hc *HealthChecker) attemptRecovery() {
	atomic.AddInt64(&hc.metrics.RecoveryAttempts, 1)
	hc.lastRecoveryAttempt = time.Now()

	klog.Info("Starting automatic recovery process...")

	recoveryCtx, cancel := context.WithTimeout(hc.ctx, hc.config.RecoveryTimeout)
	defer cancel()

	// 1. 尝试重启 Tailscale 连接
	if err := hc.restartTailscale(recoveryCtx); err != nil {
		klog.Errorf("Failed to restart Tailscale: %v", err)
	}

	// 2. 清理僵死的网络接口
	if err := hc.cleanupStaleInterfaces(recoveryCtx); err != nil {
		klog.Errorf("Failed to cleanup stale interfaces: %v", err)
	}

	// 3. 重新同步 IPAM 状态
	if err := hc.resyncIPAM(recoveryCtx); err != nil {
		klog.Errorf("Failed to resync IPAM: %v", err)
	}

	// 重置失败计数器
	atomic.StoreInt32(&hc.consecutiveFailures, 0)

	klog.Info("Recovery process completed")
}

// restartTailscale 重启 Tailscale 连接
func (hc *HealthChecker) restartTailscale(ctx context.Context) error {
	klog.Info("Restarting Tailscale connection...")

	// 首先停止
	cmd := exec.CommandContext(ctx, "tailscale", "down")
	if err := cmd.Run(); err != nil {
		klog.Warningf("Failed to stop tailscale: %v", err)
	}

	// 等待停止完成
	time.Sleep(2 * time.Second)

	// 重新启动
	cmd = exec.CommandContext(ctx, "tailscale", "up", "--accept-routes")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to restart tailscale: %v", err)
	}

	// 等待连接建立
	deadline := time.Now().Add(hc.config.TailscaleRestartTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := hc.checkTailscale(ctx); err == nil {
			klog.Info("Tailscale connection restored")
			return nil
		}
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("tailscale failed to reconnect after restart")
}

// cleanupStaleInterfaces 清理僵死的网络接口
func (hc *HealthChecker) cleanupStaleInterfaces(ctx context.Context) error {
	klog.Info("Cleaning up stale network interfaces...")

	// 使用更安全的清理脚本
	script := `
		set -e
		# 查找孤立的 veth 接口
		ip link show type veth | grep -o 'veth[^:@]*' | while read iface; do
			# 检查对应的容器是否还存在
			if ! docker ps --format "table {{.ID}}" | grep -q "${iface#veth}" 2>/dev/null; then
				echo "Deleting stale interface: $iface"
				ip link delete "$iface" 2>/dev/null || true
			fi
		done
	`

	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	return cmd.Run()
}

// resyncIPAM 重新同步 IPAM 状态
func (hc *HealthChecker) resyncIPAM(ctx context.Context) error {
	klog.Info("Resyncing IPAM state...")
	return hc.ipamManager.ForceResync(ctx)
}

// updateLastHealthCheck 更新最后健康检查时间
func (hc *HealthChecker) updateLastHealthCheck() {
	hc.statusMutex.Lock()
	defer hc.statusMutex.Unlock()
	hc.lastHealthCheck = time.Now()
}

// getLastHealthCheck 获取最后健康检查时间
func (hc *HealthChecker) getLastHealthCheck() time.Time {
	hc.statusMutex.RLock()
	defer hc.statusMutex.RUnlock()
	return hc.lastHealthCheck
}

// GetStatus 获取当前健康状态
func (hc *HealthChecker) GetStatus() *HealthStatus {
	hc.statusMutex.RLock()
	defer hc.statusMutex.RUnlock()

	return &HealthStatus{
		Status:              "unknown",
		Timestamp:           time.Now(),
		Uptime:              time.Since(hc.metrics.startTime),
		ConsecutiveFailures: atomic.LoadInt32(&hc.consecutiveFailures),
		LastCheckDuration:   hc.metrics.LastCheckDuration,
		Checks:              make(map[string]string),
		Metrics:             hc.metrics,
	}
}
