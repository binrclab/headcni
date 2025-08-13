package ipam

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestNewIPAMManager(t *testing.T) {
	podCIDR := &net.IPNet{
		IP:   net.ParseIP("10.244.0.0"),
		Mask: net.CIDRMask(24, 32),
	}

	manager, err := NewIPAMManager("test-node", podCIDR)
	if err != nil {
		t.Fatalf("Failed to create IPAM manager: %v", err)
	}

	if manager == nil {
		t.Fatal("Manager should not be nil")
	}

	if manager.nodeName != "test-node" {
		t.Errorf("Expected nodeName 'test-node', got '%s'", manager.nodeName)
	}
}

func TestIPAllocation(t *testing.T) {
	podCIDR := &net.IPNet{
		IP:   net.ParseIP("10.244.0.0"),
		Mask: net.CIDRMask(24, 32),
	}

	manager, err := NewIPAMManager("test-node", podCIDR)
	if err != nil {
		t.Fatalf("Failed to create IPAM manager: %v", err)
	}

	ctx := context.Background()

	// 测试IP分配
	allocation, err := manager.AllocateIP(ctx, "default", "test-pod", "container-1")
	if err != nil {
		t.Fatalf("Failed to allocate IP: %v", err)
	}

	if allocation == nil {
		t.Fatal("Allocation should not be nil")
	}

	if !podCIDR.Contains(allocation.IP) {
		t.Errorf("Allocated IP %s is not in CIDR %s", allocation.IP.String(), podCIDR.String())
	}

	// 测试幂等性
	allocation2, err := manager.AllocateIP(ctx, "default", "test-pod", "container-1")
	if err != nil {
		t.Fatalf("Failed to re-allocate IP: %v", err)
	}

	if !allocation.IP.Equal(allocation2.IP) {
		t.Errorf("Re-allocated IP should be the same: %s vs %s", allocation.IP.String(), allocation2.IP.String())
	}
}

func TestIPRelease(t *testing.T) {
	podCIDR := &net.IPNet{
		IP:   net.ParseIP("10.244.0.0"),
		Mask: net.CIDRMask(24, 32),
	}

	manager, err := NewIPAMManager("test-node", podCIDR)
	if err != nil {
		t.Fatalf("Failed to create IPAM manager: %v", err)
	}

	ctx := context.Background()

	// 分配IP
	allocation, err := manager.AllocateIP(ctx, "default", "test-pod", "container-1")
	if err != nil {
		t.Fatalf("Failed to allocate IP: %v", err)
	}

	// 释放IP
	err = manager.ReleaseIP(ctx, "default", "test-pod")
	if err != nil {
		t.Fatalf("Failed to release IP: %v", err)
	}

	// 等待异步操作完成
	time.Sleep(100 * time.Millisecond)

	// 再次分配应该能分配到相同的IP
	allocation2, err := manager.AllocateIP(ctx, "default", "test-pod-2", "container-2")
	if err != nil {
		t.Fatalf("Failed to allocate IP after release: %v", err)
	}

	if allocation.IP.Equal(allocation2.IP) {
		t.Logf("Successfully re-allocated the same IP: %s", allocation2.IP.String())
	}
}

func TestStatistics(t *testing.T) {
	podCIDR := &net.IPNet{
		IP:   net.ParseIP("10.244.0.0"),
		Mask: net.CIDRMask(24, 32),
	}

	manager, err := NewIPAMManager("test-node", podCIDR)
	if err != nil {
		t.Fatalf("Failed to create IPAM manager: %v", err)
	}

	stats := manager.GetStatistics()
	if stats == nil {
		t.Fatal("Statistics should not be nil")
	}

	if stats.TotalIPs <= 0 {
		t.Errorf("Total IPs should be positive, got %d", stats.TotalIPs)
	}

	if stats.NodeName != "test-node" {
		t.Errorf("Expected nodeName 'test-node', got '%s'", stats.NodeName)
	}
}
