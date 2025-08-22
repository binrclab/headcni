package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"k8s.io/klog/v2"

	"github.com/binrclab/headcni/pkg/ipam"
)

// HostLocalIPAM 实现简单的 host-local IPAM
type HostLocalIPAM struct {
	dataDir   string
	subnet    *net.IPNet
	allocated map[string]string // IP -> ContainerID
	mu        sync.RWMutex
	nextIP    net.IP
	startIP   net.IP
	endIP     net.IP
}

// NewHostLocalIPAM 创建新的 host-local IPAM
func NewHostLocalIPAM(subnetStr, strategy, dataDir string) (*HostLocalIPAM, error) {
	_, subnet, err := net.ParseCIDR(subnetStr)
	if err != nil {
		return nil, fmt.Errorf("invalid subnet: %v", err)
	}

	// 计算可用 IP 范围（跳过网络地址和广播地址）
	startIP := make(net.IP, len(subnet.IP))
	copy(startIP, subnet.IP)
	startIP[len(startIP)-1] = 10 // 从 .10 开始

	endIP := make(net.IP, len(subnet.IP))
	copy(endIP, subnet.IP)
	endIP[len(endIP)-1] = 254 // 到 .254 结束

	// 使用配置的 dataDir 或默认值
	if dataDir == "" {
		dataDir = "/var/lib/cni/networks/headcni"
	}

	return &HostLocalIPAM{
		dataDir:   dataDir,
		subnet:    subnet,
		allocated: make(map[string]string),
		nextIP:    make(net.IP, len(startIP)),
		startIP:   startIP,
		endIP:     endIP,
	}, nil
}

// AllocateIP 分配IP地址
func (h *HostLocalIPAM) AllocateIP(containerID string, podInfo *PodInfo) (*ipam.IPAllocation, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// 检查是否已经分配
	for ip, cid := range h.allocated {
		if cid == containerID {
			allocatedIP := net.ParseIP(ip)
			return &ipam.IPAllocation{
				IP:           allocatedIP,
				PodNamespace: podInfo.namespace,
				PodName:      podInfo.podName,
				ContainerID:  containerID,
				NodeName:     os.Getenv("NODE_NAME"),
				AllocatedAt:  time.Now(),
			}, nil
		}
	}

	// 分配新 IP
	allocatedIP := h.findNextAvailableIP()
	if allocatedIP == nil {
		return nil, fmt.Errorf("no available IP in subnet %s", h.subnet.String())
	}

	// 记录分配
	h.allocated[allocatedIP.String()] = containerID

	// 持久化分配记录
	if err := h.persistAllocation(allocatedIP.String(), containerID); err != nil {
		klog.Warningf("Failed to persist IP allocation: %v", err)
	}

	return &ipam.IPAllocation{
		IP:           allocatedIP,
		PodNamespace: podInfo.namespace,
		PodName:      podInfo.podName,
		ContainerID:  containerID,
		NodeName:     os.Getenv("NODE_NAME"),
		AllocatedAt:  time.Now(),
	}, nil
}

// ReleaseIP 释放IP地址
func (h *HostLocalIPAM) ReleaseIP(containerID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// 查找并删除分配记录
	for ip, cid := range h.allocated {
		if cid == containerID {
			delete(h.allocated, ip)
			// 删除持久化记录
			h.removeAllocation(ip)
			klog.Infof("Released IP %s for container %s", ip, containerID)
			return
		}
	}
}

// findNextAvailableIP 查找下一个可用IP
func (h *HostLocalIPAM) findNextAvailableIP() net.IP {
	// 简单的顺序分配
	if h.nextIP == nil {
		copy(h.nextIP, h.startIP)
	}

	// 查找下一个可用 IP
	for {
		// 检查是否超出范围
		if h.nextIP[len(h.nextIP)-1] > h.endIP[len(h.endIP)-1] {
			// 重置到开始
			copy(h.nextIP, h.startIP)
		}

		// 检查是否已被分配
		if _, exists := h.allocated[h.nextIP.String()]; !exists {
			allocatedIP := make(net.IP, len(h.nextIP))
			copy(allocatedIP, h.nextIP)

			// 移动到下一个 IP
			h.nextIP[len(h.nextIP)-1]++

			return allocatedIP
		}

		// 移动到下一个 IP
		h.nextIP[len(h.nextIP)-1]++
	}
}

// persistAllocation 持久化分配记录
func (h *HostLocalIPAM) persistAllocation(ip, containerID string) error {
	// 确保目录存在
	if err := os.MkdirAll(h.dataDir, 0755); err != nil {
		return err
	}

	// 写入分配记录
	recordPath := filepath.Join(h.dataDir, ip)
	return os.WriteFile(recordPath, []byte(containerID), 0644)
}

// removeAllocation 删除分配记录
func (h *HostLocalIPAM) removeAllocation(ip string) error {
	recordPath := filepath.Join(h.dataDir, ip)
	return os.Remove(recordPath)
}

// loadExistingAllocations 加载已存在的分配记录
func (h *HostLocalIPAM) loadExistingAllocations() error {
	// 加载已存在的分配记录
	if err := os.MkdirAll(h.dataDir, 0755); err != nil {
		return err
	}

	files, err := os.ReadDir(h.dataDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		ip := file.Name()
		data, err := os.ReadFile(filepath.Join(h.dataDir, ip))
		if err != nil {
			klog.Warningf("Failed to read allocation record for %s: %v", ip, err)
			continue
		}

		containerID := strings.TrimSpace(string(data))
		h.allocated[ip] = containerID
	}

	return nil
}

// releaseIP 释放 IP 地址
func (p *CNIPlugin) releaseIP(podInfo *PodInfo, containerID string) {
	switch p.config.IPAM.Type {
	case "headcni-ipam":
		if p.ipamManager != nil {
			p.ipamManager.ReleaseIP(context.Background(), podInfo.namespace, podInfo.podName)
		}
	case "host-local":
		if p.hostLocal != nil {
			p.hostLocal.ReleaseIP(containerID)
		}
	}
}
