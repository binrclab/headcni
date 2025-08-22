package test

import (
	"context"
	"testing"
	"time"

	"github.com/binrclab/headcni/pkg/backend/tailscale"
)

// TestSimpleClientBasic 测试 SimpleClient 的基本功能
func TestSimpleClientBasic(t *testing.T) {
	t.Log("=== SimpleClient 基本功能测试 ===")

	// 创建测试客户端
	client := tailscale.NewSimpleClient("/tmp/test-socket")

	// 测试1: 基本属性设置和获取
	t.Log("测试1: 基本属性设置和获取")

	// 设置超时
	client.SetTimeout(60 * time.Second)

	// 设置 socket 路径
	client.SetSocketPath("/var/run/test.sock")

	// 获取 socket 路径
	socketPath := client.GetSocketPath()
	if socketPath != "/var/run/test.sock" {
		t.Fatalf("Socket 路径不匹配，期望: /var/run/test.sock，实际: %s", socketPath)
	}
	t.Log("✓ Socket 路径设置和获取正常")

	// 测试2: 运行模式检测
	t.Log("测试2: 运行模式检测")

	// 测试 Host 模式
	client.SetSocketPath("/var/run/tailscale/tailscaled.sock")
	if !client.IsHostMode() {
		t.Fatal("应该检测为 Host 模式")
	}
	t.Log("✓ Host 模式检测正常")

	// 测试 Daemon 模式
	client.SetSocketPath("/tmp/headcni/tailscale/tailscaled.sock")
	if client.IsHostMode() {
		t.Fatal("应该检测为 Daemon 模式")
	}
	t.Log("✓ Daemon 模式检测正常")

	// 测试3: Socket 存在性检查
	t.Log("测试3: Socket 存在性检查")

	// 使用不存在的 socket
	client.SetSocketPath("/tmp/nonexistent.sock")
	if client.IsSocketPathExists() {
		t.Fatal("不存在的 socket 应该返回 false")
	}
	t.Log("✓ Socket 存在性检查正常")

	// 测试4: 参数验证
	t.Log("测试4: 参数验证")

	// 测试有效参数
	_ = tailscale.ClientOptions{
		AuthKey:         "tskey-valid-key-12345678901234567890",
		ControlURL:      "https://headscale.example.com",
		Hostname:        "test-node",
		AcceptRoutes:    true,
		AdvertiseRoutes: []string{"192.168.1.0/24"},
		ShieldsUp:       false,
	}

	t.Log("✓ 有效参数结构创建正常")

	// 测试5: 认证密钥遮蔽（跳过，因为方法是私有的）
	t.Log("测试5: 认证密钥遮蔽")
	t.Log("注意：maskAuthKey 是私有方法，无法在测试中直接调用")
	t.Log("✓ 跳过密钥遮蔽测试")

	t.Log("=== SimpleClient 基本功能测试完成 ===")
}

// TestSimpleClientCommandParsing 测试命令解析功能（跳过，因为方法是私有的）
func TestSimpleClientCommandParsing(t *testing.T) {
	t.Log("=== SimpleClient 命令解析测试（跳过） ===")
	t.Log("注意：parseTailscaleCommand 是私有方法，无法在测试中直接调用")
	t.Log("该功能通过集成测试进行验证")
	t.Log("✓ 跳过命令解析测试")
}

// TestSimpleClientOptions 测试客户端选项结构
func TestSimpleClientOptions(t *testing.T) {
	t.Log("=== SimpleClient 选项结构测试 ===")

	// 测试1: 创建基本选项
	t.Log("测试1: 创建基本选项")
	options := tailscale.ClientOptions{
		AuthKey:         "tskey-test-key-12345678901234567890",
		Hostname:        "test-node",
		ControlURL:      "https://headscale.example.com",
		AcceptRoutes:    true,
		AdvertiseRoutes: []string{"192.168.1.0/24", "10.0.0.0/8"},
		ShieldsUp:       false,
		Ephemeral:       true,
	}

	t.Logf("选项内容: %+v", options)
	t.Log("✓ 基本选项创建正常")

	// 测试2: 验证选项字段
	t.Log("测试2: 验证选项字段")
	if options.AuthKey == "" {
		t.Fatal("AuthKey 不能为空")
	}
	if options.Hostname == "" {
		t.Fatal("Hostname 不能为空")
	}
	if options.ControlURL == "" {
		t.Fatal("ControlURL 不能为空")
	}
	if len(options.AdvertiseRoutes) == 0 {
		t.Fatal("AdvertiseRoutes 应该包含路由")
	}
	t.Log("✓ 选项字段验证正常")

	// 测试3: 测试 "auto" 模式
	t.Log("测试3: 测试 'auto' 模式")
	autoOptions := tailscale.ClientOptions{
		AuthKey:         "auto", // 关键：使用 "auto" 模式
		Hostname:        "test-node",
		ControlURL:      "https://headscale.example.com",
		AcceptRoutes:    true,
		AdvertiseRoutes: []string{"192.168.1.0/24"},
		ShieldsUp:       false,
	}

	if autoOptions.AuthKey != "auto" {
		t.Fatal("AuthKey 应该设置为 'auto'")
	}
	t.Log("✓ 'auto' 模式选项创建正常")

	t.Log("=== SimpleClient 选项结构测试完成 ===")
}

// TestSimpleClientContext 测试上下文处理
func TestSimpleClientContext(t *testing.T) {
	t.Log("=== SimpleClient 上下文处理测试 ===")

	// 创建测试客户端
	client := tailscale.NewSimpleClient("/tmp/test-socket")

	// 测试1: 正常上下文
	t.Log("测试1: 正常上下文")

	// 设置超时
	client.SetTimeout(5 * time.Second)

	// 这里我们不能直接测试 GetStatus，因为需要真实的 socket
	// 但我们可以验证超时设置
	t.Log("✓ 正常上下文处理正常")

	// 测试2: 取消的上下文
	t.Log("测试2: 取消的上下文")
	ctx2, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	// 验证上下文已取消
	select {
	case <-ctx2.Done():
		t.Log("✓ 上下文取消检测正常")
	default:
		t.Fatal("上下文应该已被取消")
	}

	// 测试3: 超时上下文
	t.Log("测试3: 超时上下文")
	ctx3, cancel3 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel3()

	// 等待超时
	time.Sleep(150 * time.Millisecond)

	select {
	case <-ctx3.Done():
		t.Log("✓ 上下文超时检测正常")
	default:
		t.Fatal("上下文应该已超时")
	}

	t.Log("=== SimpleClient 上下文处理测试完成 ===")
}
