package monitoring

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
