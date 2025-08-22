package ipam

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"k8s.io/klog/v2"
)

type IPAMManager struct {
	nodeName  string
	podCIDR   *net.IPNet
	localPool *LocalIPPool
	mutex     sync.RWMutex

	// 本地缓存，提升性能
	allocatedIPs map[string]*IPAllocation

	// IP 分配策略
	strategy AllocationStrategy

	// 本地存储路径
	storagePath string
}

type IPAllocation struct {
	IP           net.IP            `json:"ip"`
	PodNamespace string            `json:"pod_namespace"`
	PodName      string            `json:"pod_name"`
	ContainerID  string            `json:"container_id"`
	NodeName     string            `json:"node_name"`
	AllocatedAt  time.Time         `json:"allocated_at"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

type AllocationStrategy int

const (
	StrategySequential AllocationStrategy = iota // 顺序分配
	StrategyRandom                               // 随机分配
	StrategyDensePack                            // 密集打包
)

type LocalIPPool struct {
	cidr         *net.IPNet
	nextIP       net.IP
	allocatedIPs map[string]bool // IP -> allocated
	reservedIPs  map[string]bool // 保留 IP（网关等）
	mutex        sync.RWMutex
}

func NewIPAMManager(nodeName string, podCIDR *net.IPNet) (*IPAMManager, error) {
	localPool, err := NewLocalIPPool(podCIDR)
	if err != nil {
		return nil, fmt.Errorf("failed to create local IP pool: %v", err)
	}

	// 使用 /var/lib/headcni 作为存储路径
	storagePath := "/var/lib/headcni"
	if err := os.MkdirAll(storagePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %v", err)
	}

	manager := &IPAMManager{
		nodeName:     nodeName,
		podCIDR:      podCIDR,
		localPool:    localPool,
		allocatedIPs: make(map[string]*IPAllocation),
		strategy:     StrategySequential,
		storagePath:  storagePath,
	}

	// 启动时从本地文件恢复状态
	if err := manager.restoreFromLocal(); err != nil {
		klog.Warningf("Failed to restore IPAM state from local storage: %v", err)
	}

	return manager, nil
}

func NewLocalIPPool(cidr *net.IPNet) (*LocalIPPool, error) {
	pool := &LocalIPPool{
		cidr:         cidr,
		allocatedIPs: make(map[string]bool),
		reservedIPs:  make(map[string]bool),
	}

	// 计算起始 IP（通常是 .1）
	pool.nextIP = make(net.IP, len(cidr.IP))
	copy(pool.nextIP, cidr.IP)

	// 增加到第一个可用 IP（跳过网络地址）
	pool.nextIP[len(pool.nextIP)-1]++

	// 预留特殊 IP
	pool.reserveSpecialIPs()

	return pool, nil
}

func (p *LocalIPPool) reserveSpecialIPs() {
	// 保留网络地址（.0）
	networkIP := make(net.IP, len(p.cidr.IP))
	copy(networkIP, p.cidr.IP)
	p.reservedIPs[networkIP.String()] = true

	// 保留网关地址（.1，Tailscale 网关）
	gatewayIP := make(net.IP, len(p.cidr.IP))
	copy(gatewayIP, p.cidr.IP)
	gatewayIP[len(gatewayIP)-1]++
	p.reservedIPs[gatewayIP.String()] = true

	// 保留广播地址（最后一个地址）
	broadcastIP := make(net.IP, len(p.cidr.IP))
	copy(broadcastIP, p.cidr.IP)
	ones, bits := p.cidr.Mask.Size()
	hostBits := bits - ones

	// 计算广播地址
	for i := len(broadcastIP) - 1; i >= 0 && hostBits > 0; i-- {
		if hostBits >= 8 {
			broadcastIP[i] = 0xFF
			hostBits -= 8
		} else {
			broadcastIP[i] |= 0xFF >> (8 - hostBits)
			hostBits = 0
		}
	}
	p.reservedIPs[broadcastIP.String()] = true

	// 可选：保留一些 IP 用于未来扩展（如 .2, .3）
	for i := 2; i <= 3; i++ {
		reservedIP := make(net.IP, len(p.cidr.IP))
		copy(reservedIP, p.cidr.IP)
		reservedIP[len(reservedIP)-1] = byte(i)
		if p.cidr.Contains(reservedIP) {
			p.reservedIPs[reservedIP.String()] = true
		}
	}
}

// AllocateIP 分配 IP 地址
func (m *IPAMManager) AllocateIP(ctx context.Context, podNamespace, podName, containerID string) (*IPAllocation, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// 检查是否已经分配过（幂等性）
	key := fmt.Sprintf("%s_%s", podNamespace, podName)
	if existing, exists := m.allocatedIPs[key]; exists {
		klog.Infof("IP already allocated for pod %s: %s", key, existing.IP.String())
		return existing, nil
	}

	// 从本地池分配 IP
	ip, err := m.localPool.AllocateNext(m.strategy)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate IP from local pool: %v", err)
	}

	allocation := &IPAllocation{
		IP:           ip,
		PodNamespace: podNamespace,
		PodName:      podName,
		ContainerID:  containerID,
		NodeName:     m.nodeName,
		AllocatedAt:  time.Now(),
		Metadata: map[string]string{
			"node":    m.nodeName,
			"version": "v1",
		},
	}

	// 保存到本地缓存
	m.allocatedIPs[key] = allocation

	// 异步保存到本地文件（避免阻塞）
	go func() {
		if err := m.saveToLocal(ctx, allocation); err != nil {
			klog.Errorf("Failed to save allocation to local storage: %v", err)
			// TODO: 实现重试机制
		}
	}()

	klog.Infof("Allocated IP %s for pod %s/%s", ip.String(), podNamespace, podName)
	return allocation, nil
}

func (p *LocalIPPool) AllocateNext(strategy AllocationStrategy) (net.IP, error) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	switch strategy {
	case StrategySequential:
		return p.allocateSequential()
	case StrategyRandom:
		return p.allocateRandom()
	case StrategyDensePack:
		return p.allocateDensePack()
	default:
		return p.allocateSequential()
	}
}

func (p *LocalIPPool) allocateSequential() (net.IP, error) {
	maxAttempts := 256 // 最大尝试次数，避免死循环
	attempt := 0

	startIP := make(net.IP, len(p.nextIP))
	copy(startIP, p.nextIP)

	for attempt < maxAttempts {
		ip := make(net.IP, len(p.nextIP))
		copy(ip, p.nextIP)

		// 检查 IP 是否可用
		if p.isIPAvailable(ip) {
			p.allocatedIPs[ip.String()] = true
			p.incrementNextIP()
			return ip, nil
		}

		// 移动到下一个 IP
		p.incrementNextIP()
		attempt++

		// 如果绕了一圈回到起始位置，说明没有可用 IP
		if p.nextIP.Equal(startIP) && attempt > 1 {
			break
		}
	}

	return nil, fmt.Errorf("no available IP in pool")
}

func (p *LocalIPPool) allocateRandom() (net.IP, error) {
	// 随机分配实现（用于负载均衡场景）
	ones, bits := p.cidr.Mask.Size()
	hostBits := bits - ones
	maxHosts := 1 << hostBits

	maxAttempts := min(maxHosts, 100)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		// 生成随机主机号
		hostNum := randomInt(4, maxHosts-2) // 跳过保留地址

		ip := make(net.IP, len(p.cidr.IP))
		copy(ip, p.cidr.IP)

		// 设置主机部分
		for i := len(ip) - 1; i >= 0 && hostNum > 0; i-- {
			ip[i] |= byte(hostNum & 0xFF)
			hostNum >>= 8
		}

		if p.isIPAvailable(ip) {
			p.allocatedIPs[ip.String()] = true
			return ip, nil
		}
	}

	return nil, fmt.Errorf("no available IP found with random allocation")
}

func (p *LocalIPPool) allocateDensePack() (net.IP, error) {
	// 密集打包策略：从最小的可用IP开始分配
	ones, bits := p.cidr.Mask.Size()
	hostBits := bits - ones
	maxHosts := 1 << hostBits

	// 从保留地址之后开始搜索
	for hostNum := 4; hostNum < maxHosts; hostNum++ {
		ip := make(net.IP, len(p.cidr.IP))
		copy(ip, p.cidr.IP)

		// 设置主机部分
		tempHostNum := hostNum
		for i := len(ip) - 1; i >= 0 && tempHostNum > 0; i-- {
			ip[i] |= byte(tempHostNum & 0xFF)
			tempHostNum >>= 8
		}

		if p.isIPAvailable(ip) {
			p.allocatedIPs[ip.String()] = true
			return ip, nil
		}
	}

	return nil, fmt.Errorf("no available IP found with dense pack allocation")
}

func (p *LocalIPPool) isIPAvailable(ip net.IP) bool {
	ipStr := ip.String()

	// 检查是否在 CIDR 范围内
	if !p.cidr.Contains(ip) {
		return false
	}

	// 检查是否已分配
	if p.allocatedIPs[ipStr] {
		return false
	}

	// 检查是否保留
	if p.reservedIPs[ipStr] {
		return false
	}

	return true
}

func (p *LocalIPPool) incrementNextIP() {
	// IP 地址自增
	for i := len(p.nextIP) - 1; i >= 0; i-- {
		p.nextIP[i]++
		if p.nextIP[i] != 0 {
			break
		}
	}

	// 如果超出 CIDR 范围，回到起始位置
	if !p.cidr.Contains(p.nextIP) {
		copy(p.nextIP, p.cidr.IP)
		p.nextIP[len(p.nextIP)-1]++
	}
}

// ReleaseIP 释放 IP 地址
func (m *IPAMManager) ReleaseIP(ctx context.Context, podNamespace, podName string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	key := fmt.Sprintf("%s_%s", podNamespace, podName)
	allocation, exists := m.allocatedIPs[key]
	if !exists {
		klog.Warningf("IP not found for pod %s", key)
		return nil // 幂等性
	}

	// 从本地池释放
	m.localPool.Release(allocation.IP)

	// 从本地缓存删除
	delete(m.allocatedIPs, key)

	// 异步从本地文件删除
	go func() {
		if err := m.deleteFromLocal(ctx, key); err != nil {
			klog.Errorf("Failed to delete allocation from local storage: %v", err)
		}
	}()

	klog.Infof("Released IP %s for pod %s", allocation.IP.String(), key)
	return nil
}

func (p *LocalIPPool) Release(ip net.IP) {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	delete(p.allocatedIPs, ip.String())
	klog.V(4).Infof("Released IP %s from local pool", ip.String())
}

// 本地存储相关方法
func (m *IPAMManager) saveToLocal(ctx context.Context, allocation *IPAllocation) error {
	filePath := filepath.Join(m.storagePath, fmt.Sprintf("%s_%s_%s.json", m.nodeName, allocation.PodNamespace, allocation.PodName))

	data, err := json.Marshal(allocation)
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}

func (m *IPAMManager) deleteFromLocal(ctx context.Context, podKey string) error {
	filePath := filepath.Join(m.storagePath, fmt.Sprintf("%s_%s.json", m.nodeName, podKey))
	return os.Remove(filePath)
}

func (m *IPAMManager) restoreFromLocal() error {
	files, err := filepath.Glob(filepath.Join(m.storagePath, fmt.Sprintf("%s_*.json", m.nodeName)))
	if err != nil {
		return err
	}

	for _, filePath := range files {
		data, err := os.ReadFile(filePath)
		if err != nil {
			klog.Warningf("Failed to read allocation file %s: %v", filePath, err)
			continue
		}

		var allocation IPAllocation
		if err := json.Unmarshal(data, &allocation); err != nil {
			klog.Warningf("Failed to unmarshal allocation from %s: %v", filePath, err)
			continue
		}

		// 恢复到本地状态
		podKey := fmt.Sprintf("%s_%s", allocation.PodNamespace, allocation.PodName)
		m.allocatedIPs[podKey] = &allocation
		m.localPool.allocatedIPs[allocation.IP.String()] = true
	}

	klog.Infof("Restored %d IP allocations from local storage", len(m.allocatedIPs))
	return nil
}

// syncLoop 和 syncWithEtcd 移除，因为不再依赖 etcd

// 辅助函数
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func randomInt(min, max int) int {
	// 简单的随机数生成（生产环境应使用 crypto/rand）
	return min + int(time.Now().UnixNano()%int64(max-min))
}

// GetLocalPoolCIDR 获取本地池的CIDR
func (m *IPAMManager) GetLocalPoolCIDR() string {
	if m.localPool != nil && m.localPool.cidr != nil {
		return m.localPool.cidr.String()
	}
	return ""
}

// GetLocalPool 获取本地池信息
func (m *IPAMManager) GetLocalPool() *LocalIPPool {
	return m.localPool
}

// GetLocalPoolStats 获取本地池统计信息
func (m *IPAMManager) GetLocalPoolStats() map[string]interface{} {
	if m.localPool == nil {
		return nil
	}

	m.localPool.mutex.RLock()
	defer m.localPool.mutex.RUnlock()

	// 计算可用IP数量
	totalIPs := 0
	allocatedIPs := len(m.localPool.allocatedIPs)
	reservedIPs := len(m.localPool.reservedIPs)

	// 计算总IP数量（简化计算）
	if m.localPool.cidr != nil {
		ones, bits := m.localPool.cidr.Mask.Size()
		totalIPs = 1 << (bits - ones)
	}

	return map[string]interface{}{
		"cidr":          m.localPool.cidr.String(),
		"total_ips":     totalIPs,
		"allocated_ips": allocatedIPs,
		"reserved_ips":  reservedIPs,
		"available_ips": totalIPs - allocatedIPs - reservedIPs,
		"next_ip":       m.localPool.nextIP.String(),
	}
}

// GetIPByContainerID 根据容器ID获取分配的IP
func (m *IPAMManager) GetIPByContainerID(containerID string) net.IP {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// 遍历已分配的IP，查找匹配的容器ID
	for _, allocation := range m.allocatedIPs {
		if allocation.ContainerID == containerID {
			return allocation.IP
		}
	}

	return nil
}

// GetAllocationByContainerID 根据容器ID获取完整的分配信息
func (m *IPAMManager) GetAllocationByContainerID(containerID string) *IPAllocation {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// 遍历已分配的IP，查找匹配的容器ID
	for _, allocation := range m.allocatedIPs {
		if allocation.ContainerID == containerID {
			return allocation
		}
	}

	return nil
}
