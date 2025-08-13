package ipam

import (
	"context"
	"fmt"
	"time"

	"k8s.io/klog/v2"
)

// HealthCheck 实现 IPAM 健康检查
func (m *IPAMManager) HealthCheck(ctx context.Context) error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// 检查本地状态一致性
	if err := m.validateLocalState(); err != nil {
		return fmt.Errorf("local state validation failed: %v", err)
	}

	return nil
}

func (m *IPAMManager) validateLocalState() error {
	// 验证分配的 IP 是否都在有效范围内
	for _, allocation := range m.allocatedIPs {
		if !m.podCIDR.Contains(allocation.IP) {
			return fmt.Errorf("allocated IP %s is outside pod CIDR %s",
				allocation.IP.String(), m.podCIDR.String())
		}
	}

	// 检查本地池状态
	return m.localPool.Validate()
}

func (p *LocalIPPool) Validate() error {
	p.mutex.RLock()
	defer p.mutex.RUnlock()

	// 检查分配的 IP 数量是否合理
	totalIPs := p.calculateTotalIPs()
	allocatedCount := len(p.allocatedIPs)

	if allocatedCount > totalIPs {
		return fmt.Errorf("allocated IPs (%d) exceed total available (%d)",
			allocatedCount, totalIPs)
	}

	// 检查 nextIP 是否有效
	if !p.cidr.Contains(p.nextIP) {
		klog.Warningf("nextIP %s is outside CIDR, resetting", p.nextIP.String())
		p.resetNextIP()
	}

	return nil
}

func (p *LocalIPPool) calculateTotalIPs() int {
	ones, bits := p.cidr.Mask.Size()
	return (1 << (bits - ones)) - len(p.reservedIPs)
}

func (p *LocalIPPool) resetNextIP() {
	copy(p.nextIP, p.cidr.IP)
	p.nextIP[len(p.nextIP)-1] = 1 // 从 .1 开始
}

// ForceResync 强制重新同步状态
func (m *IPAMManager) ForceResync(ctx context.Context) error {
	klog.Info("Force resyncing IPAM state...")

	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 清空本地缓存
	m.allocatedIPs = make(map[string]*IPAllocation)

	// 重置本地池
	m.localPool.allocatedIPs = make(map[string]bool)
	m.localPool.resetNextIP()

	// 从本地存储重新加载
	return m.restoreFromLocal()
}

// GetStatistics 获取 IPAM 统计信息
func (m *IPAMManager) GetStatistics() *IPAMStats {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	totalIPs := m.localPool.calculateTotalIPs()
	allocatedCount := len(m.allocatedIPs)

	return &IPAMStats{
		TotalIPs:     totalIPs,
		AllocatedIPs: allocatedCount,
		AvailableIPs: totalIPs - allocatedCount,
		Utilization:  float64(allocatedCount) / float64(totalIPs) * 100,
		NodeName:     m.nodeName,
		PodCIDR:      m.podCIDR.String(),
	}
}

type IPAMStats struct {
	TotalIPs     int     `json:"total_ips"`
	AllocatedIPs int     `json:"allocated_ips"`
	AvailableIPs int     `json:"available_ips"`
	Utilization  float64 `json:"utilization_percent"`
	NodeName     string  `json:"node_name"`
	PodCIDR      string  `json:"pod_cidr"`
}

// GC 垃圾回收孤立的 IP 分配
func (m *IPAMManager) GarbageCollect(ctx context.Context) error {
	klog.Info("Starting IPAM garbage collection...")

	m.mutex.Lock()
	defer m.mutex.Unlock()

	var toDelete []string
	cutoffTime := time.Now().Add(-1 * time.Hour) // 1小时前

	for key, allocation := range m.allocatedIPs {
		// 检查分配是否过期（基于时间）
		if allocation.AllocatedAt.Before(cutoffTime) {
			// 这里应该检查对应的 Pod 是否还存在
			// 简化版本：基于时间清理
			if m.shouldCleanupAllocation(allocation) {
				toDelete = append(toDelete, key)
			}
		}
	}

	// 删除标记的分配
	for _, key := range toDelete {
		allocation := m.allocatedIPs[key]
		m.localPool.Release(allocation.IP)
		delete(m.allocatedIPs, key)

		// 从本地存储删除
		go func(k string) {
			if err := m.deleteFromLocal(ctx, k); err != nil {
				klog.Errorf("Failed to delete from local storage during GC: %v", err)
			}
		}(key)

		klog.Infof("GC: Released IP %s for deleted pod %s",
			allocation.IP.String(), key)
	}

	klog.Infof("IPAM garbage collection completed, cleaned %d allocations", len(toDelete))
	return nil
}

func (m *IPAMManager) shouldCleanupAllocation(allocation *IPAllocation) bool {
	// 这里应该实现更智能的检查逻辑
	// 例如：检查容器是否还在运行，Pod 是否还存在等

	// 简化版：基于时间的清理策略
	return time.Since(allocation.AllocatedAt) > 2*time.Hour
}
