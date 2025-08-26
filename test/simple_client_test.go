//go:build integration
// +build integration

package test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/binrclab/headcni/cmd/daemon/config"
	"github.com/binrclab/headcni/pkg/backend/tailscale"
	"github.com/binrclab/headcni/pkg/headscale"
)

// TestSimpleClientAutoMode 测试 SimpleClient 的 "auto" 模式功能
func TestSimpleClientAutoMode(t *testing.T) {
	// 从环境变量获取配置
	headscaleURL := os.Getenv("HEADSCALE_URL")
	if headscaleURL == "" {
		headscaleURL = "https://hs.binrc.com"
	}

	headscaleAPIKey := os.Getenv("HEADSCALE_API_KEY")
	if headscaleAPIKey == "" {
		headscaleAPIKey = "GoHTP1MUgQ.E7TZTzhUXnjaBB3aDmtcVXnkV9i-JCWQ7V8nh9ovLx4"
	}

	testUser := os.Getenv("HEADSCALE_USER")
	if testUser == "" {
		testUser = "server"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	t.Log("=== SimpleClient Auto 模式测试 ===")

	// 步骤1: 创建 Headscale 客户端
	t.Log("步骤1: 创建 Headscale 客户端")
	headscaleConfig := &config.HeadscaleConfig{
		URL:     headscaleURL,
		AuthKey: headscaleAPIKey,
		Timeout: "30s",
		Retries: 3,
	}

	headscaleClient, err := headscale.NewClient(headscaleConfig)
	if err != nil {
		t.Fatalf("创建 Headscale 客户端失败: %v", err)
	}

	// 步骤2: 创建预授权密钥
	t.Log("步骤2: 创建预授权密钥")
	preAuthKeyReq := &headscale.CreatePreAuthKeyRequest{
		User:       testUser,
		Reusable:   false,
		Ephemeral:  false,
		AclTags:    []string{"tag:test-client"},
		Expiration: time.Now().Add(1 * time.Hour),
	}

	preAuthResp, err := headscaleClient.CreatePreAuthKey(ctx, preAuthKeyReq)
	if err != nil {
		t.Fatalf("创建预授权密钥失败: %v", err)
	}

	authKey := preAuthResp.PreAuthKey.Key
	t.Logf("✓ 获取到认证密钥: %s...", authKey[:10])

	// 步骤3: 创建并启动 Tailscale 服务
	t.Log("步骤3: 创建并启动 Tailscale 服务")
	tempDir := "/tmp/headcni-test-client"
	defer os.RemoveAll(tempDir)

	// 生成唯一的 TUN 设备名称，避免冲突
	tunDevice := "t01"

	serviceOptions := tailscale.ServiceOptions{
		Hostname:   "test-client-auto",
		AuthKey:    authKey,
		ControlURL: headscaleURL,
		Mode:       tailscale.ModeStandaloneTailscaled,
		ConfigDir:  tempDir, // 添加配置目录
		SocketPath: tempDir + "/tailscale.sock",
		StateFile:  tempDir + "/tailscaled.state",
		Interface:  tunDevice, // 使用动态生成的 TUN 设备名称
		Logf: func(format string, args ...interface{}) {
			t.Logf("[Service] "+format, args...)
		},
	}

	serviceManager := tailscale.NewServiceManager()
	service, err := serviceManager.StartService(ctx, "test-auto", serviceOptions)
	if err != nil {
		t.Fatalf("启动服务失败: %v", err)
	}
	t.Logf("✓ 服务启动成功: %s", service.Name)

	// 等待服务启动
	time.Sleep(5 * time.Second)

	// 步骤4: 创建 SimpleClient 并测试 "auto" 模式
	t.Log("步骤4: 测试 SimpleClient 的 'auto' 模式")
	client := tailscale.NewSimpleClient(service.GetSocketPath())
	client.SetTimeout(30 * time.Second)

	// 测试1: 使用 authKey 进行初始登录
	t.Log("测试1: 使用 authKey 进行初始登录")
	err = client.UpWithOptions(ctx, tailscale.ClientOptions{
		AuthKey:         authKey,
		Hostname:        "test-client-auto",
		ControlURL:      headscaleURL,
		AcceptRoutes:    true,
		AdvertiseRoutes: []string{"192.168.100.0/24"},
		ShieldsUp:       false,
	})
	if err != nil {
		t.Fatalf("初始登录失败: %v", err)
	}
	t.Log("✓ 初始登录成功")

	// 等待连接建立
	time.Sleep(10 * time.Second)

	// 验证连接状态
	status, err := client.GetStatus(ctx)
	if err != nil {
		t.Fatalf("获取状态失败: %v", err)
	}
	t.Logf("连接状态: %s", status.BackendState)

	if status.BackendState != "Running" {
		t.Fatalf("连接未建立，状态: %s", status.BackendState)
	}

	// 测试2: 使用 "auto" 模式复用现有认证
	t.Log("测试2: 使用 'auto' 模式复用现有认证")
	err = client.UpWithOptions(ctx, tailscale.ClientOptions{
		AuthKey:         "auto", // 关键：使用 "auto" 模式
		Hostname:        "test-client-auto",
		ControlURL:      headscaleURL,
		AcceptRoutes:    true,
		AdvertiseRoutes: []string{"192.168.100.0/24"},
		ShieldsUp:       false,
	})
	if err != nil {
		t.Fatalf("'auto' 模式登录失败: %v", err)
	}
	t.Log("✓ 'auto' 模式登录成功")

	// 测试3: 验证配置变更检测
	t.Log("测试3: 验证配置变更检测")

	// 尝试使用不同的主机名，应该触发重新认证
	err = client.UpWithOptions(ctx, tailscale.ClientOptions{
		AuthKey:         "auto",
		Hostname:        "test-client-different", // 不同的主机名
		ControlURL:      headscaleURL,
		AcceptRoutes:    true,
		AdvertiseRoutes: []string{"192.168.100.0/24"},
		ShieldsUp:       false,
	})
	if err != nil {
		t.Logf("配置变更检测触发重新认证: %v", err)
		t.Log("✓ 配置变更检测功能正常")
	} else {
		t.Log("⚠ 配置变更检测可能未生效")
	}

	// 测试4: 测试路由管理功能
	t.Log("测试4: 测试路由管理功能")

	// 添加新路由
	newRoute := "192.168.200.0/24"
	err = client.AdvertiseRoute(ctx, newRoute)
	if err != nil {
		t.Logf("添加路由失败: %v", err)
	} else {
		t.Logf("✓ 添加路由成功: %s", newRoute)
	}

	// 获取所有路由
	allRoutes, err := client.GetPrefs(ctx)
	if err != nil {
		t.Logf("获取路由偏好失败: %v", err)
	} else {
		t.Logf("当前通告路由: %v", allRoutes.AdvertiseRoutes)
	}

	// 测试5: 测试连接性检查
	t.Log("测试5: 测试连接性检查")

	err = client.CheckConnectivity(ctx)
	if err != nil {
		t.Logf("连接性检查失败: %v", err)
	} else {
		t.Log("✓ 连接性检查成功")
	}

	// 测试6: 测试 IP 获取
	t.Log("测试6: 测试 IP 获取")

	ip, err := client.GetIP(ctx)
	if err != nil {
		t.Logf("获取 IP 失败: %v", err)
	} else {
		t.Logf("✓ 获得 Tailscale IP: %s", ip)
	}

	// 测试7: 测试状态检查
	t.Log("测试7: 测试状态检查")

	if client.IsRunning(ctx) {
		t.Log("✓ 客户端正在运行")
	} else {
		t.Log("✗ 客户端未运行")
	}

	if client.IsConnected(ctx) {
		t.Log("✓ 客户端已连接")
	} else {
		t.Log("✗ 客户端未连接")
	}

	t.Log("=== SimpleClient Auto 模式测试完成 ===")
}
