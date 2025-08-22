package monitoring

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// CNI 操作指标
	cniOperationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "tailscale_cni_operation_duration_seconds",
			Help:    "Time spent on CNI operations",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation", "result"},
	)

	cniOperationTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tailscale_cni_operations_total",
			Help: "Total number of CNI operations",
		},
		[]string{"operation", "result"},
	)

	// IPAM 指标
	ipamAllocatedIPs = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "tailscale_cni_ipam_allocated_ips",
			Help: "Number of allocated IP addresses",
		},
	)

	ipamPoolUtilization = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "tailscale_cni_ipam_pool_utilization_percent",
			Help: "IP pool utilization percentage",
		},
	)

	// Tailscale 连接指标
	tailscaleConnectionStatus = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "tailscale_cni_connection_status",
			Help: "Tailscale connection status (1=connected, 0=disconnected)",
		},
	)

	tailscalePeerCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "tailscale_cni_peer_count",
			Help: "Number of Tailscale peers",
		},
	)

	// 系统健康指标
	systemHealthStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "tailscale_cni_system_health_status",
			Help: "System health status (1=healthy, 0=unhealthy)",
		},
		[]string{"component"},
	)

	// 错误计数指标
	errorCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tailscale_cni_errors_total",
			Help: "Total number of errors by component and type",
		},
		[]string{"component", "error_type"},
	)
)

// 监控装饰器
func RecordOperation(operation string) func(string) {
	start := time.Now()
	return func(result string) {
		duration := time.Since(start)
		cniOperationDuration.WithLabelValues(operation, result).Observe(duration.Seconds())
		cniOperationTotal.WithLabelValues(operation, result).Inc()
	}
}

func UpdateIPAMMetrics(allocated, total int) {
	ipamAllocatedIPs.Set(float64(allocated))
	if total > 0 {
		utilization := float64(allocated) / float64(total) * 100
		ipamPoolUtilization.Set(utilization)
	}
}

func UpdateTailscaleMetrics(connected bool, peerCount int) {
	if connected {
		tailscaleConnectionStatus.Set(1)
	} else {
		tailscaleConnectionStatus.Set(0)
	}
	tailscalePeerCount.Set(float64(peerCount))
}

// 更新系统健康状态
func UpdateSystemHealth(component string, healthy bool) {
	if healthy {
		systemHealthStatus.WithLabelValues(component).Set(1)
	} else {
		systemHealthStatus.WithLabelValues(component).Set(0)
	}
}

// 记录错误
func RecordError(component, errorType string) {
	errorCount.WithLabelValues(component, errorType).Inc()
}

var (
	// prometheusHandler Prometheus HTTP handler
	prometheusHandler = promhttp.Handler()
)

// InitMetrics 初始化监控指标
func InitMetrics() {
	// 设置初始值
	ipamAllocatedIPs.Set(0)
	ipamPoolUtilization.Set(0)
	tailscaleConnectionStatus.Set(0)
	tailscalePeerCount.Set(0)

	// 初始化系统健康状态
	components := []string{"cni", "tailscale", "headscale", "k8s", "overall"}
	for _, component := range components {
		systemHealthStatus.WithLabelValues(component).Set(0) // 初始状态为不健康
	}

	// 初始化错误计数
	errorTypes := []string{"network", "auth", "timeout", "validation", "unknown"}
	for _, errorType := range errorTypes {
		errorCount.WithLabelValues("cni", errorType).Add(0)
		errorCount.WithLabelValues("tailscale", errorType).Add(0)
		errorCount.WithLabelValues("headscale", errorType).Add(0)
	}
}

// GetPrometheusHandler 获取 Prometheus HTTP handler
func GetPrometheusHandler() http.Handler {
	return prometheusHandler
}

// ResetMetrics 重置所有指标（用于测试或重置）
func ResetMetrics() {
	ipamAllocatedIPs.Set(0)
	ipamPoolUtilization.Set(0)
	tailscaleConnectionStatus.Set(0)
	tailscalePeerCount.Set(0)

	// 重置系统健康状态
	components := []string{"cni", "tailscale", "headscale", "k8s", "overall"}
	for _, component := range components {
		systemHealthStatus.WithLabelValues(component).Set(0)
	}

	// 注意：Counter 类型不能重置，只能递增
	// 如果需要重置计数，需要重新创建指标
}
