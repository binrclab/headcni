package health

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"time"

	"github.com/binrclab/headcni/pkg/ipam"
	"github.com/binrclab/headcni/pkg/networking"

	"k8s.io/klog/v2"
)

type HealthChecker struct {
	ipamManager *ipam.IPAMManager
	networkMgr  *networking.NetworkManager
	httpServer  *http.Server
}

func NewHealthChecker(ipamMgr *ipam.IPAMManager, netMgr *networking.NetworkManager) *HealthChecker {
	hc := &HealthChecker{
		ipamManager: ipamMgr,
		networkMgr:  netMgr,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", hc.healthzHandler)
	mux.HandleFunc("/readyz", hc.readyzHandler)
	mux.HandleFunc("/livez", hc.livezHandler)

	hc.httpServer = &http.Server{
		Addr:    ":8081",
		Handler: mux,
	}

	return hc
}

func (hc *HealthChecker) Start() error {
	klog.Info("Starting health checker...")

	// 启动定期健康检查
	go hc.periodicHealthCheck()

	// 启动 HTTP 健康检查服务
	return hc.httpServer.ListenAndServe()
}

func (hc *HealthChecker) healthzHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	checks := []HealthCheck{
		{"tailscale", hc.checkTailscale},
		{"ipam", hc.checkIPAM},
		{"network", hc.checkNetwork},
	}

	allHealthy := true
	results := make(map[string]string)

	for _, check := range checks {
		if err := check.Function(ctx); err != nil {
			allHealthy = false
			results[check.Name] = fmt.Sprintf("ERROR: %v", err)
			klog.Warningf("Health check %s failed: %v", check.Name, err)
		} else {
			results[check.Name] = "OK"
		}
	}

	w.Header().Set("Content-Type", "application/json")

	if allHealthy {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	// 返回详细的检查结果
	fmt.Fprintf(w, `{"status":"%s","checks":%s}`,
		map[bool]string{true: "healthy", false: "unhealthy"}[allHealthy],
		toJSON(results))
}

func (hc *HealthChecker) readyzHandler(w http.ResponseWriter, r *http.Request) {
	// 就绪检查：确保服务可以处理请求
	if err := hc.checkReadiness(); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ready")
}

func (hc *HealthChecker) livezHandler(w http.ResponseWriter, r *http.Request) {
	// 存活检查：确保服务进程正常运行
	if err := hc.checkLiveness(); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "alive")
}

type HealthCheck struct {
	Name     string
	Function func(context.Context) error
}

func (hc *HealthChecker) checkTailscale(ctx context.Context) error {
	// 检查 Tailscale 连接状态
	return hc.networkMgr.CheckTailscaleConnectivity()
}

func (hc *HealthChecker) checkIPAM(ctx context.Context) error {
	// 检查 IPAM 服务状态
	return hc.ipamManager.HealthCheck(ctx)
}

func (hc *HealthChecker) checkNetwork(ctx context.Context) error {
	// 检查基本网络功能

	// 1. 检查 tailscale0 接口
	_, err := net.InterfaceByName("tailscale0")
	if err != nil {
		return fmt.Errorf("tailscale0 interface not found: %v", err)
	}

	// 2. 检查到 Tailscale IP 的连通性
	tailscaleIP, err := hc.networkMgr.GetTailscaleIP()
	if err != nil {
		return fmt.Errorf("failed to get tailscale IP: %v", err)
	}

	// 简单的连通性测试
	conn, err := net.DialTimeout("tcp", tailscaleIP.String()+":0", 2*time.Second)
	if err == nil {
		conn.Close()
	}
	// 这里不严格要求连接成功，只要接口存在即可

	return nil
}

func (hc *HealthChecker) checkReadiness() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 确保 Tailscale 已连接且状态正常
	if err := hc.checkTailscale(ctx); err != nil {
		return fmt.Errorf("tailscale not ready: %v", err)
	}

	// 确保 IPAM 服务可用
	if err := hc.checkIPAM(ctx); err != nil {
		return fmt.Errorf("IPAM not ready: %v", err)
	}

	return nil
}

func (hc *HealthChecker) checkLiveness() error {
	// 基本进程存活检查

	// 检查 tailscaled 进程
	cmd := exec.Command("pgrep", "tailscaled")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tailscaled process not running")
	}

	return nil
}

func (hc *HealthChecker) periodicHealthCheck() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	consecutiveFailures := 0
	const maxFailures = 3

	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)

			healthy := true
			if err := hc.checkTailscale(ctx); err != nil {
				klog.Errorf("Periodic Tailscale check failed: %v", err)
				healthy = false
			}

			if err := hc.checkIPAM(ctx); err != nil {
				klog.Errorf("Periodic IPAM check failed: %v", err)
				healthy = false
			}

			cancel()

			if healthy {
				consecutiveFailures = 0
			} else {
				consecutiveFailures++
				klog.Warningf("Health check failed %d consecutive times", consecutiveFailures)

				if consecutiveFailures >= maxFailures {
					klog.Errorf("Too many consecutive failures, attempting recovery...")
					hc.attemptRecovery()
					consecutiveFailures = 0 // 重置计数器
				}
			}
		}
	}
}

func (hc *HealthChecker) attemptRecovery() {
	klog.Info("Starting automatic recovery process...")

	// 1. 尝试重启 Tailscale 连接
	if err := hc.restartTailscale(); err != nil {
		klog.Errorf("Failed to restart Tailscale: %v", err)
	}

	// 2. 清理僵死的网络接口
	if err := hc.cleanupStaleInterfaces(); err != nil {
		klog.Errorf("Failed to cleanup stale interfaces: %v", err)
	}

	// 3. 重新同步 IPAM 状态
	if err := hc.resyncIPAM(); err != nil {
		klog.Errorf("Failed to resync IPAM: %v", err)
	}

	klog.Info("Recovery process completed")
}

func (hc *HealthChecker) restartTailscale() error {
	klog.Info("Restarting Tailscale connection...")

	// 首先停止
	cmd := exec.Command("tailscale", "down")
	if err := cmd.Run(); err != nil {
		klog.Warningf("Failed to stop tailscale: %v", err)
	}

	time.Sleep(2 * time.Second)

	// 重新启动
	cmd = exec.Command("tailscale", "up", "--accept-routes")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to restart tailscale: %v", err)
	}

	// 等待连接建立
	for i := 0; i < 30; i++ {
		if err := hc.networkMgr.CheckTailscaleConnectivity(); err == nil {
			klog.Info("Tailscale connection restored")
			return nil
		}
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("tailscale failed to reconnect after restart")
}

func (hc *HealthChecker) cleanupStaleInterfaces() error {
	klog.Info("Cleaning up stale network interfaces...")

	// 查找并删除孤立的 veth 接口
	cmd := exec.Command("sh", "-c", `
        ip link show type veth | grep -o 'veth[^:@]*' | while read iface; do
            # 检查对应的容器是否还存在
            if ! docker ps --format "table {{.ID}}" | grep -q "${iface#veth}"; then
                echo "Deleting stale interface: $iface"
                ip link delete "$iface" 2>/dev/null || true
            fi
        done
    `)

	return cmd.Run()
}

func (hc *HealthChecker) resyncIPAM() error {
	klog.Info("Resyncing IPAM state...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return hc.ipamManager.ForceResync(ctx)
}

func toJSON(data interface{}) string {
	// 简单的 JSON 序列化
	switch v := data.(type) {
	case map[string]string:
		result := "{"
		first := true
		for k, val := range v {
			if !first {
				result += ","
			}
			result += fmt.Sprintf(`"%s":"%s"`, k, val)
			first = false
		}
		result += "}"
		return result
	default:
		return "{}"
	}
}
