// pkg/backend/tailscale/client_simple.go
package tailscale

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/binrclab/headcni/pkg/constants"
	"tailscale.com/client/local"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/types/persist"
)

// ClientOptions 客户端启动选项
type ClientOptions struct {
	AuthKey         string   // 认证密钥
	Hostname        string   // 主机名
	ControlURL      string   // 控制服务器URL
	AdvertiseRoutes []string // 要通告的路由
	AcceptRoutes    bool     // 是否接受路由
	ShieldsUp       bool     // 是否启用Shields Up模式
	Ephemeral       bool     // 是否为临时节点
}

// SimpleClient 是统一的Tailscale客户端，专注于通过socket与tailscaled交互
type SimpleClient struct {
	localClient *local.Client
	socketPath  string
	mu          sync.RWMutex
	timeout     time.Duration
}

// NewSimpleClient 创建新的简化Tailscale客户端
func NewSimpleClient(socketPath string) *SimpleClient {
	if socketPath == "" {
		socketPath = constants.DefaultTailscaleDaemonSocketPath
	}

	client := &SimpleClient{
		socketPath:  socketPath,
		timeout:     30 * time.Second,
		localClient: &local.Client{Socket: socketPath},
	}

	return client
}

// SetSocketPath 设置socket路径
func (c *SimpleClient) SetSocketPath(socketPath string) {
	c.socketPath = socketPath
	c.localClient.Socket = socketPath
}

// GetSocketPath 获取socket路径
func (c *SimpleClient) GetSocketPath() string {
	return c.socketPath
}

// IsSocketPathExists 检查socket是否存在
func (c *SimpleClient) IsSocketPathExists() bool {
	if _, err := os.Stat(c.socketPath); os.IsNotExist(err) {
		return false
	}
	c.localClient.Socket = c.socketPath
	return true
}

// IsHostMode 检查是否使用系统路径
func (c *SimpleClient) IsHostMode() bool {
	return c.socketPath == "/var/run/tailscale/tailscaled.sock" ||
		c.socketPath == "/var/run/tailscale/tailscaled.socket" ||
		c.socketPath == "/run/tailscale/tailscaled.sock"
}

// SetTimeout 设置超时时间
func (c *SimpleClient) SetTimeout(timeout time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.timeout = timeout
}

// GetStatus 获取当前状态
func (c *SimpleClient) GetStatus(ctx context.Context) (*ipnstate.Status, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.localClient.Status(ctx)
}

// CheckSocketExists 检查socket是否可访问
func (c *SimpleClient) CheckSocketExists() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := c.localClient.Status(ctx)
	return err
}

// Down 断开连接
func (c *SimpleClient) Down(ctx context.Context) error {
	log.Println("正在断开Tailscale连接...")

	status, err := c.GetStatus(ctx)
	if err != nil {
		log.Printf("获取状态失败: %v", err)
	} else if status.BackendState == "Stopped" {
		log.Println("连接已经处于停止状态")
		return nil
	}

	prefs := ipn.NewPrefs()
	prefs.WantRunning = false

	maskedPrefs := &ipn.MaskedPrefs{
		Prefs:          *prefs,
		WantRunningSet: true,
	}

	_, err = c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		return fmt.Errorf("停止连接失败: %v", err)
	}

	// 等待连接停止
	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Second)
		status, err := c.GetStatus(ctx)
		if err == nil && status.BackendState == "Stopped" {
			log.Println("连接已成功停止")
			return nil
		}
	}

	log.Println("连接停止命令已发送")
	return nil
}

// UpWithOptions 启动Tailscale连接 - 纯API版本
// UpWithOptions 启动Tailscale连接 - 增强诊断版本
// func (c *SimpleClient) UpWithOptions(ctx context.Context, options ClientOptions) error {
// 	log.Printf("=== 开始Tailscale登录流程 (增强诊断模式) ===")
// 	log.Printf("控制URL: %s", options.ControlURL)
// 	log.Printf("主机名: %s", options.Hostname)
// 	log.Printf("认证密钥: %s...", c.maskAuthKey(options.AuthKey))
// 	log.Printf("Socket路径: %s", c.socketPath)

// 	// 验证参数
// 	if err := c.validateOptions(options); err != nil {
// 		return fmt.Errorf("参数验证失败: %v", err)
// 	}

// 	// 步骤1: 检查socket连接性
// 	if err := c.CheckSocketExists(); err != nil {
// 		return fmt.Errorf("Socket连接失败: %v", err)
// 	}
// 	log.Println("✅ Socket连接正常")

// 	// 步骤2: 检查并复用现有状态
// 	if err := c.checkAndReuseExistingState(ctx, options); err == nil {
// 		log.Println("=== 复用现有状态，登录流程完成 ===")
// 		return nil
// 	}

// 	// 步骤3: 执行增强的API认证
// 	if err := c.authenticate(ctx, options); err != nil {
// 		return fmt.Errorf("认证失败: %v", err)
// 	}

// 	// 步骤4: 等待最终连接完成
// 	if err := c.waitForFullConnection(ctx); err != nil {
// 		return fmt.Errorf("等待连接完成失败: %v", err)
// 	}

// 	log.Println("=== 登录流程完成 ===")
// 	return nil
// }

// waitForFullConnection 等待完整连接建立
func (c *SimpleClient) waitForFullConnection(ctx context.Context) error {
	log.Println("等待完整连接建立...")

	maxWaitSeconds := 240 // 4分钟等待连接
	checkInterval := 2 * time.Second

	for i := 0; i < maxWaitSeconds/2; i++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("上下文取消: %v", ctx.Err())
		default:
		}

		time.Sleep(checkInterval)

		status, err := c.GetStatus(ctx)
		if err != nil {
			log.Printf("状态检查失败 %d: %v", i+1, err)
			continue
		}

		// 每10秒打印一次详细状态
		if i%5 == 0 || i < 5 {
			log.Printf("连接等待进度 %d/%ds - 状态: %s, HaveNodeKey: %v, Online: %v",
				(i+1)*2, maxWaitSeconds, status.BackendState, status.HaveNodeKey,
				status.Self != nil && status.Self.Online)
		}

		switch status.BackendState {
		case "Running":
			if c.isLoginComplete(status) {
				log.Printf("✅ 连接成功! 总耗时: %d秒", (i+1)*2)
				c.logConnectionInfo(status)
				return nil
			} else {
				// Running但没有IP，继续等待
				if i%10 == 0 {
					log.Printf("状态Running但IP未分配，继续等待...")
				}
			}

		case "Starting":
			if i%10 == 0 {
				log.Println("正在启动连接...")
			}

		case "NeedsLogin":
			// 如果有NodeKey但状态还是NeedsLogin，可能需要重新启用
			if status.HaveNodeKey {
				log.Println("有NodeKey但状态为NeedsLogin，尝试重新启用运行状态")
				if err := c.enableRunningAfterAuth(ctx); err != nil {
					log.Printf("重新启用失败: %v", err)
				}
			} else {
				// 诊断网络问题
				if i > 30 { // 60秒后开始诊断
					if i%15 == 0 { // 每30秒诊断一次
						c.diagnoseNetworkIssues(ctx)
					}
				}
			}

		case "Stopped":
			log.Println("连接被停止，尝试重新启用")
			if err := c.enableRunningAfterAuth(ctx); err != nil {
				log.Printf("重新启用失败: %v", err)
			}

		default:
			log.Printf("未知状态: %s", status.BackendState)
		}

		// 超时检查
		if i > 60 { // 120秒后更严格的检查
			if status.BackendState == "NeedsLogin" && !status.HaveNodeKey {
				return fmt.Errorf("120秒后仍无NodeKey，认证可能失败")
			}
		}
	}

	return fmt.Errorf("连接超时")
}

// diagnoseNetworkIssues 诊断网络问题
func (c *SimpleClient) diagnoseNetworkIssues(ctx context.Context) {
	log.Println("🔍 诊断网络问题...")

	// 检查偏好设置
	prefs, err := c.localClient.GetPrefs(ctx)
	if err != nil {
		log.Printf("无法获取偏好设置: %v", err)
		return
	}

	log.Printf("当前配置:")
	log.Printf("  ControlURL: %s", prefs.ControlURL)
	log.Printf("  Hostname: %s", prefs.Hostname)
	log.Printf("  WantRunning: %v", prefs.WantRunning)
	log.Printf("  LoggedOut: %v", prefs.LoggedOut)

	// 测试控制服务器连接
	if err := c.checkHeadscaleReachability(); err != nil {
		log.Printf("⚠️ 控制服务器连接问题: %v", err)
	} else {
		log.Println("✅ 控制服务器连接正常")
	}
}

// logConnectionInfo 记录连接信息
func (c *SimpleClient) logConnectionInfo(status *ipnstate.Status) {
	if status.Self == nil {
		return
	}

	log.Printf("🎉 连接信息:")
	log.Printf("  节点名: %s", status.Self.HostName)
	log.Printf("  在线状态: %v", status.Self.Online)

	if len(status.Self.TailscaleIPs) > 0 {
		log.Printf("  分配的IP:")
		for _, ip := range status.Self.TailscaleIPs {
			log.Printf("    %s", ip.String())
		}
	}

	log.Printf("  对等节点数: %d", len(status.Peer))
}

// checkAndReuseExistingState 检查并复用现有状态
func (c *SimpleClient) checkAndReuseExistingState(ctx context.Context, options ClientOptions) error {
	log.Println("检查现有状态，尝试复用...")

	status, err := c.GetStatus(ctx)
	if err != nil {
		log.Printf("无法获取状态: %v", err)
		return fmt.Errorf("无法获取状态")
	}

	log.Printf("当前状态: %s", status.BackendState)

	// 如果已经是运行状态，检查配置是否匹配
	if status.BackendState == "Running" {
		log.Println("✓ 客户端已处于运行状态")

		if status.Self != nil && len(status.Self.TailscaleIPs) > 0 {
			log.Printf("✓ 已有有效IP: %v", status.Self.TailscaleIPs)

			// 获取当前偏好设置
			prefs, err := c.localClient.GetPrefs(ctx)
			if err != nil {
				log.Printf("无法获取偏好设置: %v", err)
				return fmt.Errorf("无法获取偏好设置")
			}

			// 检查关键配置是否匹配
			configChanged := false
			changeReasons := []string{}

			// 检查控制URL
			if prefs.ControlURL != options.ControlURL {
				configChanged = true
				changeReasons = append(changeReasons, fmt.Sprintf("ControlURL: %s -> %s", prefs.ControlURL, options.ControlURL))
			}

			// 检查主机名
			if prefs.Hostname != options.Hostname {
				configChanged = true
				changeReasons = append(changeReasons, fmt.Sprintf("Hostname: %s -> %s", prefs.Hostname, options.Hostname))
			}

			// 检查路由配置
			if prefs.RouteAll != options.AcceptRoutes {
				configChanged = true
				changeReasons = append(changeReasons, fmt.Sprintf("AcceptRoutes: %v -> %v", prefs.RouteAll, options.AcceptRoutes))
			}

			// 检查通告路由
			if len(options.AdvertiseRoutes) > 0 {
				currentRoutes := make(map[string]bool)
				for _, route := range prefs.AdvertiseRoutes {
					currentRoutes[route.String()] = true
				}

				for _, newRoute := range options.AdvertiseRoutes {
					if !currentRoutes[newRoute] {
						configChanged = true
						changeReasons = append(changeReasons, fmt.Sprintf("AdvertiseRoutes: 新增 %s", newRoute))
						break
					}
				}
			}

			// 如果配置没有变化，可以复用
			if !configChanged {
				log.Println("✓ 配置完全匹配，可以复用现有状态")

				// 启用运行状态
				newPrefs := ipn.NewPrefs()
				newPrefs.WantRunning = true

				maskedPrefs := &ipn.MaskedPrefs{
					Prefs:          *newPrefs,
					WantRunningSet: true,
				}

				_, err = c.localClient.EditPrefs(ctx, maskedPrefs)
				if err == nil {
					log.Println("✓ 成功复用现有状态")
					return nil
				}
			} else {
				log.Println("⚠️ 配置发生变化，需要重新认证:")
				for _, reason := range changeReasons {
					log.Printf("  - %s", reason)
				}
				return fmt.Errorf("配置变更需要重新认证")
			}
		}
	}

	log.Println("无法复用现有状态，需要重新认证")
	return fmt.Errorf("需要重新认证")
}

// resetState 重置状态
func (c *SimpleClient) resetState(ctx context.Context) error {
	log.Println("重置连接状态...")

	prefs := ipn.NewPrefs()
	prefs.WantRunning = false

	maskedPrefs := &ipn.MaskedPrefs{
		Prefs:          *prefs,
		WantRunningSet: true,
	}

	_, err := c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		log.Printf("停止连接失败: %v", err)
	}

	// 等待停止
	for i := 0; i < 5; i++ {
		time.Sleep(1 * time.Second)
		if status, err := c.GetStatus(ctx); err == nil {
			if status.BackendState == "Stopped" {
				break
			}
		}
	}

	log.Println("状态重置完成")
	return nil
}

// authenticate 执行认证 - 增强诊断版本
func (c *SimpleClient) authenticate(ctx context.Context, options ClientOptions) error {
	log.Println("执行API认证...")

	// 如果是 "auto" 模式，处理现有状态
	if options.AuthKey == "auto" {
		return c.handleAutoModeAPI(ctx, options)
	}

	// 步骤1: 详细诊断当前状态
	if err := c.diagnoseCurrentState(ctx); err != nil {
		log.Printf("状态诊断失败: %v", err)
	}

	// 步骤2: 分步设置配置
	if err := c.setupAuthConfiguration(ctx, options); err != nil {
		return fmt.Errorf("配置设置失败: %v", err)
	}

	// 步骤3: 尝试不同的认证方法
	methods := []func(context.Context, ClientOptions) error{
		c.authenticateWithStartOptions,
		c.authenticateWithLoginInteractive,
		c.authenticateWithDirectConfig,
	}

	for i, method := range methods {
		log.Printf("尝试认证方法 %d...", i+1)

		if err := method(ctx, options); err != nil {
			log.Printf("认证方法 %d 失败: %v", i+1, err)
			continue
		}

		// 检查认证是否成功
		if err := c.waitForAuthCompletion(ctx); err != nil {
			log.Printf("认证方法 %d 完成失败: %v", i+1, err)
			continue
		}

		log.Printf("✅ 认证方法 %d 成功", i+1)
		return c.enableRunningAfterAuth(ctx)
	}

	return fmt.Errorf("所有认证方法都失败")
}

// diagnoseCurrentState 诊断当前状态
func (c *SimpleClient) diagnoseCurrentState(ctx context.Context) error {
	log.Println("🔍 诊断当前状态...")

	// 检查socket连接
	status, err := c.GetStatus(ctx)
	if err != nil {
		log.Printf("❌ 无法获取状态: %v", err)
		return err
	}

	log.Printf("📊 当前状态详情:")
	log.Printf("  版本: %s", status.Version)
	log.Printf("  后端状态: %s", status.BackendState)
	log.Printf("  HaveNodeKey: %v", status.HaveNodeKey)
	log.Printf("  TUN: %v", status.TUN)
	log.Printf("  AuthURL: %s", status.AuthURL)

	// 检查偏好设置
	prefs, err := c.localClient.GetPrefs(ctx)
	if err != nil {
		log.Printf("❌ 无法获取偏好设置: %v", err)
	} else {
		log.Printf("📋 当前偏好设置:")
		log.Printf("  ControlURL: %s", prefs.ControlURL)
		log.Printf("  Hostname: %s", prefs.Hostname)
		log.Printf("  WantRunning: %v", prefs.WantRunning)
		log.Printf("  LoggedOut: %v", prefs.LoggedOut)
		log.Printf("  Persist: %v", prefs.Persist != nil)
	}

	// 检查网络连接
	if err := c.checkNetworkConnectivity(ctx, ""); err != nil {
		log.Printf("⚠️ 网络连接问题: %v", err)
	}

	return nil
}

// setupAuthConfiguration 设置认证配置
func (c *SimpleClient) setupAuthConfiguration(ctx context.Context, options ClientOptions) error {
	log.Println("设置认证配置...")

	// 确保daemon处于正确状态
	prefs := ipn.NewPrefs()
	prefs.ControlURL = options.ControlURL
	prefs.WantRunning = false
	prefs.LoggedOut = false

	maskedPrefs := &ipn.MaskedPrefs{
		Prefs:          *prefs,
		ControlURLSet:  true,
		WantRunningSet: true,
		LoggedOutSet:   true,
	}

	_, err := c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		return fmt.Errorf("设置基础配置失败: %v", err)
	}

	// 等待配置生效
	time.Sleep(2 * time.Second)
	log.Println("基础配置设置完成")

	return nil
}

// authenticateWithStartOptions 使用StartOptions认证
func (c *SimpleClient) authenticateWithStartOptions(ctx context.Context, options ClientOptions) error {
	log.Println("方法1: 使用StartOptions认证...")

	// 获取当前偏好设置
	currentPrefs, err := c.localClient.GetPrefs(ctx)
	if err != nil {
		currentPrefs = ipn.NewPrefs()
	}

	// 设置完整的偏好设置
	authPrefs := *currentPrefs
	authPrefs.ControlURL = options.ControlURL
	authPrefs.Hostname = options.Hostname
	authPrefs.RouteAll = options.AcceptRoutes
	authPrefs.ShieldsUp = options.ShieldsUp
	authPrefs.WantRunning = true
	authPrefs.LoggedOut = false

	// 设置通告路由
	if len(options.AdvertiseRoutes) > 0 {
		var routes []netip.Prefix
		for _, route := range options.AdvertiseRoutes {
			if prefix, err := netip.ParsePrefix(route); err == nil {
				routes = append(routes, prefix)
			}
		}
		authPrefs.AdvertiseRoutes = routes
	}

	// startOptions := ipn.Options{
	// 	AuthKey: options.AuthKey,
	// }

	// 备用方案：使用 Start 但带更完整的选项
	log.Println("尝试备用 Start 方法")
	startOptions := ipn.Options{
		AuthKey:     options.AuthKey,
		UpdatePrefs: &authPrefs,
	}

	err = c.localClient.Start(ctx, startOptions)
	if err != nil {
		return fmt.Errorf("Start 和 Login 都失败: %v", err)
	}
	log.Printf("调用 Start() - AuthKey: %s...", c.maskAuthKey(options.AuthKey))
	return c.localClient.Start(ctx, startOptions)
}

// authenticateWithLoginInteractive 使用Login交互认证
func (c *SimpleClient) authenticateWithLoginInteractive(ctx context.Context, options ClientOptions) error {
	log.Println("方法2: 使用Login交互认证...")

	// 先设置偏好设置
	prefs, err := c.localClient.GetPrefs(ctx)
	if err != nil {
		prefs = ipn.NewPrefs()
	}

	prefs.ControlURL = options.ControlURL
	prefs.Hostname = options.Hostname
	prefs.RouteAll = options.AcceptRoutes
	prefs.ShieldsUp = options.ShieldsUp
	prefs.WantRunning = false
	prefs.LoggedOut = false

	if len(options.AdvertiseRoutes) > 0 {
		var routes []netip.Prefix
		for _, route := range options.AdvertiseRoutes {
			if prefix, err := netip.ParsePrefix(route); err == nil {
				routes = append(routes, prefix)
			}
		}
		prefs.AdvertiseRoutes = routes
	}

	maskedPrefs := &ipn.MaskedPrefs{
		Prefs:              *prefs,
		ControlURLSet:      true,
		HostnameSet:        true,
		RouteAllSet:        true,
		ShieldsUpSet:       true,
		WantRunningSet:     true,
		LoggedOutSet:       true,
		AdvertiseRoutesSet: len(options.AdvertiseRoutes) > 0,
	}

	// 应用偏好设置
	_, err = c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		return fmt.Errorf("设置偏好失败: %v", err)
	}

	time.Sleep(1 * time.Second)

	// 调用LoginInteractive
	log.Printf("调用 LoginInteractive() - AuthKey: %s...", c.maskAuthKey(options.AuthKey))
	return c.localClient.Start(ctx, ipn.Options{
		AuthKey: options.AuthKey,
	})
}

// authenticateWithDirectConfig 直接配置认证
func (c *SimpleClient) authenticateWithDirectConfig(ctx context.Context, options ClientOptions) error {
	log.Println("方法3: 直接配置认证...")

	// 创建包含authkey的完整偏好设置
	prefs := ipn.NewPrefs()
	prefs.ControlURL = options.ControlURL
	prefs.Hostname = options.Hostname
	prefs.RouteAll = options.AcceptRoutes
	prefs.ShieldsUp = options.ShieldsUp
	prefs.WantRunning = true // 直接启用运行
	prefs.LoggedOut = false

	// 设置通告路由
	if len(options.AdvertiseRoutes) > 0 {
		var routes []netip.Prefix
		for _, route := range options.AdvertiseRoutes {
			if prefix, err := netip.ParsePrefix(route); err == nil {
				routes = append(routes, prefix)
			}
		}
		prefs.AdvertiseRoutes = routes
	}

	// 尝试设置Persist字段（包含authkey）
	if prefs.Persist == nil {
		prefs.Persist = &persist.Persist{}
	}

	maskedPrefs := &ipn.MaskedPrefs{
		Prefs:              *prefs,
		ControlURLSet:      true,
		HostnameSet:        true,
		RouteAllSet:        true,
		ShieldsUpSet:       true,
		WantRunningSet:     true,
		LoggedOutSet:       true,
		AdvertiseRoutesSet: len(options.AdvertiseRoutes) > 0,
	}

	log.Printf("应用完整配置 - AuthKey: %s...", c.maskAuthKey(options.AuthKey))
	_, err := c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		return fmt.Errorf("应用配置失败: %v", err)
	}

	// 单独调用一个简单的Start
	startOptions := ipn.Options{
		AuthKey: options.AuthKey,
	}

	return c.localClient.Start(ctx, startOptions)
}

// checkNetworkConnectivity 检查网络连接
func (c *SimpleClient) checkNetworkConnectivity(ctx context.Context, controlURL string) error {
	if controlURL == "" {
		prefs, err := c.localClient.GetPrefs(ctx)
		if err != nil {
			return fmt.Errorf("无法获取控制URL: %v", err)
		}
		controlURL = prefs.ControlURL
	}

	if controlURL == "" {
		return fmt.Errorf("控制URL为空")
	}

	log.Printf("检查控制服务器连接: %s", controlURL)

	// 解析URL
	u, err := url.Parse(controlURL)
	if err != nil {
		return fmt.Errorf("无效的控制URL: %v", err)
	}

	// 检查DNS解析
	addrs, err := net.LookupHost(u.Hostname())
	if err != nil {
		log.Printf("❌ DNS解析失败: %v", err)
		return fmt.Errorf("DNS解析失败: %v", err)
	}
	log.Printf("✅ DNS解析成功: %v", addrs)

	// 检查TCP连接
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	conn, err := net.DialTimeout("tcp", net.JoinHostPort(u.Hostname(), port), 10*time.Second)
	if err != nil {
		log.Printf("❌ TCP连接失败: %v", err)
		return fmt.Errorf("TCP连接失败: %v", err)
	}
	defer conn.Close()

	log.Printf("✅ TCP连接成功")
	return nil
}

// waitForAuthCompletion 等待认证完成 - 增强版本
func (c *SimpleClient) waitForAuthCompletion(ctx context.Context) error {
	log.Println("等待认证完成...")

	maxWaitSeconds := 30 // 减少到30秒，专注于认证阶段
	checkInterval := 1 * time.Second

	for i := 0; i < maxWaitSeconds; i++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("上下文取消: %v", ctx.Err())
		default:
		}

		time.Sleep(checkInterval)

		status, err := c.GetStatus(ctx)
		if err != nil {
			log.Printf("状态检查失败 %d: %v", i+1, err)
			continue
		}

		// 每5秒详细打印状态
		if i%5 == 0 {
			log.Printf("认证进度 %d/%ds - 状态: %s, NodeKey: %v, AuthURL: %s",
				i+1, maxWaitSeconds, status.BackendState, status.HaveNodeKey, status.AuthURL)
		}

		// 成功条件
		if status.HaveNodeKey {
			log.Println("✅ NodeKey已获得，认证成功")
			return nil
		}

		if status.BackendState == "Starting" || status.BackendState == "Running" {
			log.Printf("✅ 状态变为 %s，认证成功", status.BackendState)
			return nil
		}

		// 如果有AuthURL，说明需要手动认证（这不应该发生在使用authkey时）
		if status.AuthURL != "" {
			log.Printf("⚠️ 需要手动认证: %s", status.AuthURL)
			return fmt.Errorf("需要手动认证，AuthKey可能无效")
		}
	}

	return fmt.Errorf("认证超时，未能获得NodeKey")
}

// enableRunningAfterAuth 认证后启用运行状态
func (c *SimpleClient) enableRunningAfterAuth(ctx context.Context) error {
	log.Println("认证完成，启用运行状态...")

	// 获取当前偏好设置
	currentPrefs, err := c.localClient.GetPrefs(ctx)
	if err != nil {
		return fmt.Errorf("获取偏好设置失败: %v", err)
	}

	// 只修改运行状态
	runPrefs := *currentPrefs
	runPrefs.WantRunning = true

	maskedPrefs := &ipn.MaskedPrefs{
		Prefs:          runPrefs,
		WantRunningSet: true,
	}

	_, err = c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		return fmt.Errorf("启用运行状态失败: %v", err)
	}

	log.Println("✅ 运行状态已启用")
	return nil
}

// // waitForAuthCompletion 等待认证完成（不等待完全连接）
// func (c *SimpleClient) waitForAuthCompletion(ctx context.Context) error {
// 	log.Println("等待认证完成...")

// 	maxWaitSeconds := 60 // 认证阶段只等待60秒
// 	checkInterval := 2 * time.Second

// 	for i := 0; i < maxWaitSeconds/2; i++ {
// 		select {
// 		case <-ctx.Done():
// 			return fmt.Errorf("上下文取消: %v", ctx.Err())
// 		default:
// 		}

// 		time.Sleep(checkInterval)

// 		status, err := c.GetStatus(ctx)
// 		if err != nil {
// 			log.Printf("状态检查失败 %d: %v", i+1, err)
// 			continue
// 		}

// 		log.Printf("认证等待进度 %d/%ds - 状态: %s, HaveNodeKey: %v",
// 			(i+1)*2, maxWaitSeconds, status.BackendState, status.HaveNodeKey)

// 		// 检查是否获得了NodeKey，这表明认证基本成功
// 		if status.HaveNodeKey {
// 			log.Println("✅ NodeKey已获得，认证基础完成")
// 			return nil
// 		}

// 		// 如果状态变为Starting或Running，也认为认证成功
// 		if status.BackendState == "Starting" || status.BackendState == "Running" {
// 			log.Printf("✅ 状态变为 %s，认证成功", status.BackendState)
// 			return nil
// 		}

// 		// 如果仍然是NeedsLogin且没有NodeKey，继续等待
// 		if status.BackendState == "NeedsLogin" {
// 			continue
// 		}

// 		// 其他状态
// 		log.Printf("未预期的状态: %s", status.BackendState)
// 	}

// 	return fmt.Errorf("认证超时，未能获得NodeKey")
// }

// handleAutoModeAPI 处理auto模式 - API版本
func (c *SimpleClient) handleAutoModeAPI(ctx context.Context, options ClientOptions) error {
	log.Println("Auto模式：API方式处理...")

	status, err := c.GetStatus(ctx)
	if err != nil {
		return fmt.Errorf("无法获取状态: %v", err)
	}

	// 如果已经运行且有IP，直接启用
	if status.BackendState == "Running" && status.Self != nil && len(status.Self.TailscaleIPs) > 0 {
		log.Println("Auto模式：已连接，启用运行状态")
		return c.enableRunningAfterAuth(ctx)
	}

	// 如果有NodeKey但没有运行，说明之前认证过，只需要启用运行
	if status.HaveNodeKey {
		log.Println("Auto模式：有NodeKey，只需启用运行状态")

		// 先更新配置以匹配当前选项
		if err := c.updatePrefsForAuto(ctx, options); err != nil {
			log.Printf("更新配置失败: %v", err)
		}

		return c.enableRunningAfterAuth(ctx)
	}

	return fmt.Errorf("Auto模式无法复用现有状态，需要提供有效的AuthKey")
}

// updatePrefsForAuto 为auto模式更新偏好设置
func (c *SimpleClient) updatePrefsForAuto(ctx context.Context, options ClientOptions) error {
	log.Println("更新auto模式配置...")

	currentPrefs, err := c.localClient.GetPrefs(ctx)
	if err != nil {
		return fmt.Errorf("获取当前偏好设置失败: %v", err)
	}

	// 检查是否需要更新配置
	needUpdate := false
	updatePrefs := *currentPrefs

	if currentPrefs.ControlURL != options.ControlURL {
		updatePrefs.ControlURL = options.ControlURL
		needUpdate = true
		log.Printf("更新ControlURL: %s -> %s", currentPrefs.ControlURL, options.ControlURL)
	}

	if currentPrefs.Hostname != options.Hostname {
		updatePrefs.Hostname = options.Hostname
		needUpdate = true
		log.Printf("更新Hostname: %s -> %s", currentPrefs.Hostname, options.Hostname)
	}

	if currentPrefs.RouteAll != options.AcceptRoutes {
		updatePrefs.RouteAll = options.AcceptRoutes
		needUpdate = true
		log.Printf("更新AcceptRoutes: %v -> %v", currentPrefs.RouteAll, options.AcceptRoutes)
	}

	// 更新通告路由
	if len(options.AdvertiseRoutes) > 0 {
		var newRoutes []netip.Prefix
		for _, route := range options.AdvertiseRoutes {
			if prefix, err := netip.ParsePrefix(route); err == nil {
				newRoutes = append(newRoutes, prefix)
			}
		}

		// 比较现有路由
		if !routesEqual(currentPrefs.AdvertiseRoutes, newRoutes) {
			updatePrefs.AdvertiseRoutes = newRoutes
			needUpdate = true
			log.Printf("更新AdvertiseRoutes")
		}
	}

	if needUpdate {
		maskedPrefs := &ipn.MaskedPrefs{
			Prefs:              updatePrefs,
			ControlURLSet:      currentPrefs.ControlURL != options.ControlURL,
			HostnameSet:        currentPrefs.Hostname != options.Hostname,
			RouteAllSet:        currentPrefs.RouteAll != options.AcceptRoutes,
			AdvertiseRoutesSet: len(options.AdvertiseRoutes) > 0,
		}

		_, err = c.localClient.EditPrefs(ctx, maskedPrefs)
		if err != nil {
			return fmt.Errorf("更新偏好设置失败: %v", err)
		}

		log.Println("配置更新完成")
	} else {
		log.Println("配置无需更新")
	}

	return nil
}

// routesEqual 比较两个路由列表是否相等
func routesEqual(a, b []netip.Prefix) bool {
	if len(a) != len(b) {
		return false
	}

	aMap := make(map[string]bool)
	for _, route := range a {
		aMap[route.String()] = true
	}

	for _, route := range b {
		if !aMap[route.String()] {
			return false
		}
	}

	return true
}

// setupConfiguration 设置配置 - 简化版本，只在必要时使用
func (c *SimpleClient) setupConfiguration(ctx context.Context, options ClientOptions) error {
	log.Println("设置基础配置...")

	// 只设置最基础的配置，其他配置在认证时一起设置
	prefs := ipn.NewPrefs()
	prefs.ControlURL = options.ControlURL
	prefs.WantRunning = false // 先不启动

	maskedPrefs := &ipn.MaskedPrefs{
		Prefs:          *prefs,
		ControlURLSet:  true,
		WantRunningSet: true,
	}

	_, err := c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		return fmt.Errorf("设置基础配置失败: %v", err)
	}

	log.Println("基础配置设置完成")
	return nil
}

// authenticateWithCLI 使用CLI进行认证
func (c *SimpleClient) authenticateWithCLI(ctx context.Context, options ClientOptions) error {
	log.Println("使用CLI认证方法...")

	// 如果是 "auto" 模式，使用 CLI 的 up 命令（不带 authkey）
	if options.AuthKey == "auto" {
		log.Println("Auto模式：使用CLI重新认证（配置变更）")

		// 直接尝试 up 命令，附加必要参数
		upArgs := []string{
			"--socket", c.socketPath,
			"up",
			"--login-server", options.ControlURL,
			"--hostname", options.Hostname,
		}

		if options.AcceptRoutes {
			upArgs = append(upArgs, "--accept-routes")
		}

		if len(options.AdvertiseRoutes) > 0 {
			upArgs = append(upArgs, "--advertise-routes", strings.Join(options.AdvertiseRoutes, ","))
		}

		log.Printf("执行 up 命令: tailscale %s", strings.Join(upArgs, " "))
		upCmd := exec.CommandContext(ctx, "tailscale", upArgs...)
		upOutput, upErr := upCmd.CombinedOutput()

		if upErr != nil {
			outputStr := string(upOutput)
			log.Printf("up 命令失败，tailscale 提示: %s", outputStr)

			// 检查输出中是否提示需要补全参数
			if strings.Contains(outputStr, "Usage:") || strings.Contains(outputStr, "tailscale up") ||
				strings.Contains(outputStr, "required") || strings.Contains(outputStr, "missing") {
				log.Println("✓ 检测到参数缺失提示，尝试解析输出中的命令")

				// 尝试从输出中解析 tailscale up 后面的完整命令
				parsedArgs, err := c.parseTailscaleCommand(outputStr)
				if err != nil {
					log.Printf("解析命令失败: %v，使用默认补全", err)
					// 回退到默认补全逻辑
					completeArgs := append([]string{}, upArgs...)
					if strings.Contains(outputStr, "authkey") || strings.Contains(outputStr, "auth") {
						completeArgs = append(completeArgs, "--reset")
					}
					parsedArgs = completeArgs
				}

				log.Printf("解析到的命令: tailscale %s", strings.Join(parsedArgs, " "))
				completeCmd := exec.CommandContext(ctx, "tailscale", parsedArgs...)
				completeOutput, completeErr := completeCmd.CombinedOutput()

				if completeErr != nil {
					log.Printf("解析命令执行失败: %v", completeErr)
					log.Printf("命令输出: %s", string(completeOutput))
					return fmt.Errorf("解析命令执行失败: %v, 输出: %s", completeErr, string(completeOutput))
				}

				log.Printf("解析命令执行成功，输出: %s", string(completeOutput))
				return nil
			} else {
				// 如果没有参数提示，返回原始错误
				return fmt.Errorf("up 命令失败: %v, 输出: %s", upErr, outputStr)
			}
		} else {
			log.Printf("up 命令成功，输出: %s", string(upOutput))
			return nil
		}
	}

	// 正常认证模式
	args := []string{
		"--socket", c.socketPath,
		"up",
		"--authkey", options.AuthKey,
		"--login-server", options.ControlURL,
		"--hostname", options.Hostname,
	}

	if options.AcceptRoutes {
		args = append(args, "--accept-routes")
	}

	if len(options.AdvertiseRoutes) > 0 {
		args = append(args, "--advertise-routes", strings.Join(options.AdvertiseRoutes, ","))
	}

	log.Printf("执行CLI命令: tailscale %s", strings.Join(args, " "))

	cmd := exec.CommandContext(ctx, "tailscale", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("CLI认证失败: %v", err)
		log.Printf("命令输出: %s", string(output))
		return fmt.Errorf("CLI认证失败: %v, 输出: %s", err, string(output))
	}

	log.Printf("CLI认证成功，输出: %s", string(output))
	return nil
}

// waitForLogin 等待登录完成
func (c *SimpleClient) waitForLogin(ctx context.Context) error {
	log.Println("等待登录完成...")

	maxWaitSeconds := 300 // 增加到5分钟
	checkInterval := 2 * time.Second

	log.Printf("开始等待，最多%d秒", maxWaitSeconds)

	for i := 0; i < maxWaitSeconds/2; i++ {
		select {
		case <-ctx.Done():
			return fmt.Errorf("上下文取消: %v", ctx.Err())
		default:
		}

		time.Sleep(checkInterval)

		status, err := c.GetStatus(ctx)
		if err != nil {
			log.Printf("状态检查失败 %d: %v", i+1, err)
			continue
		}

		// 每10秒打印一次状态
		if i%5 == 0 || i < 10 {
			//打印状态和偏好设置
			log.Printf("状态: %+v", status)
			prefs, err := c.localClient.GetPrefs(ctx)
			if err != nil {
				log.Printf("无法获取偏好设置: %v", err)
			}
			log.Printf("偏好设置: %+v", prefs)
			log.Printf("等待进度 %d/%ds - 状态: %s", (i+1)*2, maxWaitSeconds, status.BackendState)
		}

		switch status.BackendState {
		case "Running":
			if c.isLoginComplete(status) {
				log.Printf("✅ 登录成功! 耗时: %d秒", (i+1)*2)
				return nil
			}

		case "NeedsLogin":
			// 增加更详细的诊断信息
			if i > 60 { // 120秒后开始诊断
				log.Printf("⚠️  120秒后仍处于NeedsLogin状态，开始诊断...")

				// 检查网络连接
				if err := c.diagnoseConnection(); err != nil {
					log.Printf("网络诊断失败: %v", err)
				}

				// 检查 Headscale 服务器可达性
				if err := c.checkHeadscaleReachability(); err != nil {
					log.Printf("Headscale服务器不可达: %v", err)
					return fmt.Errorf("Headscale服务器不可达: %v", err)
				}
			}

			if i > 120 { // 240秒后返回错误
				return fmt.Errorf("认证失败：240秒后仍处于NeedsLogin状态，请检查网络连接和Headscale服务器状态")
			}

		case "Starting":
			if i%5 == 0 {
				log.Println("正在启动...")
			}

		case "Stopped":
			return fmt.Errorf("连接意外停止")

		default:
			log.Printf("未知状态: %s", status.BackendState)
		}
	}

	return fmt.Errorf("登录超时")
}

// isLoginComplete 检查登录是否完成
func (c *SimpleClient) isLoginComplete(status *ipnstate.Status) bool {
	if status.Self == nil {
		return false
	}

	if len(status.Self.TailscaleIPs) == 0 {
		return false
	}

	return status.Self.Online
}

// diagnoseConnection 诊断网络连接问题
func (c *SimpleClient) diagnoseConnection() error {
	log.Println("🔍 开始网络连接诊断...")

	// 检查本地网络接口
	cmd := exec.Command("ip", "addr", "show")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("无法获取网络接口信息: %v", err)
	}

	log.Printf("网络接口状态:\n%s", string(output))

	// 检查路由表
	cmd = exec.Command("ip", "route", "show")
	output, err = cmd.Output()
	if err != nil {
		return fmt.Errorf("无法获取路由信息: %v", err)
	}

	log.Printf("路由表:\n%s", string(output))

	return nil
}

// checkHeadscaleReachability 检查 Headscale 服务器可达性
func (c *SimpleClient) checkHeadscaleReachability() error {
	log.Println("🌐 检查 Headscale 服务器可达性...")

	prefs, err := c.localClient.GetPrefs(context.Background())
	if err != nil {
		return fmt.Errorf("无法获取偏好设置: %v", err)
	}

	controlURL := prefs.ControlURL
	if controlURL == "" {
		return fmt.Errorf("控制URL未设置")
	}

	log.Printf("检查控制URL: %s", controlURL)

	// 尝试解析URL
	u, err := url.Parse(controlURL)
	if err != nil {
		return fmt.Errorf("无效的控制URL: %v", err)
	}

	// 尝试建立TCP连接
	conn, err := net.DialTimeout("tcp", u.Host, 10*time.Second)
	if err != nil {
		return fmt.Errorf("无法连接到 %s: %v", u.Host, err)
	}
	defer conn.Close()

	log.Printf("✅ TCP连接成功: %s", u.Host)

	// 尝试HTTP请求
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(controlURL)
	if err != nil {
		return fmt.Errorf("HTTP请求失败: %v", err)
	}
	defer resp.Body.Close()

	log.Printf("✅ HTTP请求成功: %s (状态码: %d)", controlURL, resp.StatusCode)

	return nil
}

// validateOptions 验证选项参数
func (c *SimpleClient) validateOptions(options ClientOptions) error {
	// 支持 "auto" 模式（使用现有认证信息）
	if options.AuthKey == "" {
		return fmt.Errorf("认证密钥不能为空")
	}

	if options.ControlURL == "" {
		return fmt.Errorf("控制URL不能为空")
	}

	// 如果是 "auto" 模式，跳过长度验证
	if options.AuthKey != "auto" && len(options.AuthKey) < 20 {
		return fmt.Errorf("认证密钥格式可能不正确，长度过短")
	}

	for _, route := range options.AdvertiseRoutes {
		if _, err := netip.ParsePrefix(route); err != nil {
			return fmt.Errorf("无效的路由格式 '%s': %v", route, err)
		}
	}

	return nil
}

// maskAuthKey 遮蔽认证密钥敏感信息
func (c *SimpleClient) maskAuthKey(key string) string {
	if len(key) <= 15 {
		return "***"
	}
	return key[:15] + "***"
}

// Up 启动Tailscale连接（简化版本）
func (c *SimpleClient) Up(ctx context.Context, authKey string) error {
	options := ClientOptions{
		AuthKey: authKey,
	}
	return c.UpWithOptions(ctx, options)
}

// AdvertiseRoutes 通告路由
func (c *SimpleClient) AdvertiseRoutes(ctx context.Context, routes ...netip.Prefix) error {
	prefs := ipn.NewPrefs()
	prefs.AdvertiseRoutes = routes

	_, err := c.localClient.EditPrefs(ctx, &ipn.MaskedPrefs{
		Prefs:              *prefs,
		AdvertiseRoutesSet: true,
	})

	return err
}

// RemoveRoutes 移除通告的路由
func (c *SimpleClient) RemoveRoutes(ctx context.Context, routes ...netip.Prefix) error {
	currentPrefs, err := c.localClient.GetPrefs(ctx)
	if err != nil {
		return fmt.Errorf("failed to get current preferences: %v", err)
	}

	toRemove := make(map[netip.Prefix]bool)
	for _, route := range routes {
		toRemove[route] = true
	}

	var newRoutes []netip.Prefix
	for _, route := range currentPrefs.AdvertiseRoutes {
		if !toRemove[route] {
			newRoutes = append(newRoutes, route)
		}
	}

	prefs := ipn.NewPrefs()
	prefs.AdvertiseRoutes = newRoutes

	_, err = c.localClient.EditPrefs(ctx, &ipn.MaskedPrefs{
		Prefs:              *prefs,
		AdvertiseRoutesSet: true,
	})

	return err
}

// AcceptRoutes 接受路由
func (c *SimpleClient) AcceptRoutes(ctx context.Context) error {
	prefs := ipn.NewPrefs()
	prefs.RouteAll = true

	_, err := c.localClient.EditPrefs(ctx, &ipn.MaskedPrefs{
		Prefs:       *prefs,
		RouteAllSet: true,
	})

	return err
}

// RejectRoutes 拒绝路由
func (c *SimpleClient) RejectRoutes(ctx context.Context) error {
	prefs := ipn.NewPrefs()
	prefs.RouteAll = false

	_, err := c.localClient.EditPrefs(ctx, &ipn.MaskedPrefs{
		Prefs:       *prefs,
		RouteAllSet: true,
	})

	return err
}

// SetHostname 设置主机名
func (c *SimpleClient) SetHostname(ctx context.Context, hostname string) error {
	prefs := ipn.NewPrefs()
	prefs.Hostname = hostname

	_, err := c.localClient.EditPrefs(ctx, &ipn.MaskedPrefs{
		Prefs:       *prefs,
		HostnameSet: true,
	})

	return err
}

// GetIP 获取主要的Tailscale IP
func (c *SimpleClient) GetIP(ctx context.Context) (netip.Addr, error) {
	status, err := c.GetStatus(ctx)
	if err != nil {
		return netip.Addr{}, err
	}

	if status.Self == nil || len(status.Self.TailscaleIPs) == 0 {
		return netip.Addr{}, fmt.Errorf("no tailscale IP assigned")
	}

	// 优先返回IPv4地址
	for _, ip := range status.Self.TailscaleIPs {
		if ip.Is4() {
			return ip, nil
		}
	}

	return status.Self.TailscaleIPs[0], nil
}

// GetAllIPs 获取所有Tailscale IP地址
func (c *SimpleClient) GetAllIPs(ctx context.Context) ([]netip.Addr, error) {
	status, err := c.GetStatus(ctx)
	if err != nil {
		return nil, err
	}

	if status.Self == nil {
		return nil, fmt.Errorf("no self information available")
	}

	return status.Self.TailscaleIPs, nil
}

// IsRunning 检查Tailscale是否运行
func (c *SimpleClient) IsRunning(ctx context.Context) bool {
	status, err := c.GetStatus(ctx)
	if err != nil {
		return false
	}
	return status.BackendState == "Running"
}

// IsConnected 检查是否已连接到tailnet
func (c *SimpleClient) IsConnected(ctx context.Context) bool {
	status, err := c.GetStatus(ctx)
	if err != nil {
		return false
	}
	return status.BackendState == "Running" && status.Self != nil && len(status.Self.TailscaleIPs) > 0
}

// CheckConnectivity 检查连接性
func (c *SimpleClient) CheckConnectivity(ctx context.Context) error {
	status, err := c.GetStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to get status: %v", err)
	}

	if status.BackendState != "Running" {
		return fmt.Errorf("tailscale not running, state: %s", status.BackendState)
	}

	if status.Self == nil || len(status.Self.TailscaleIPs) == 0 {
		return fmt.Errorf("no tailscale IP assigned")
	}

	return nil
}

// AdvertiseRoute 通告路由（兼容旧接口）
func (c *SimpleClient) AdvertiseRoute(ctx context.Context, routes ...string) error {
	var prefixes []netip.Prefix
	for _, route := range routes {
		if route == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(route)
		if err != nil {
			return fmt.Errorf("invalid route %s: %v", route, err)
		}
		prefixes = append(prefixes, prefix)
	}

	return c.AdvertiseRoutes(ctx, prefixes...)
}

// RemoveRoute 移除路由（兼容旧接口）
func (c *SimpleClient) RemoveRoute(ctx context.Context, routes ...string) error {
	var prefixes []netip.Prefix
	for _, route := range routes {
		if route == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(route)
		if err != nil {
			return fmt.Errorf("invalid route %s: %v", route, err)
		}
		prefixes = append(prefixes, prefix)
	}

	return c.RemoveRoutes(ctx, prefixes...)
}

// GetPeers 获取对等节点
func (c *SimpleClient) GetPeers(ctx context.Context) (map[string]*ipnstate.PeerStatus, error) {
	status, err := c.GetStatus(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*ipnstate.PeerStatus)
	for key, peer := range status.Peer {
		result[key.String()] = peer
	}

	return result, nil
}

// GetPrefs 获取偏好设置
func (c *SimpleClient) GetPrefs(ctx context.Context) (*ipn.Prefs, error) {
	return c.localClient.GetPrefs(ctx)
}

// WhoIs 查询IP归属
func (c *SimpleClient) WhoIs(ctx context.Context, remoteAddr string) (interface{}, error) {
	return c.localClient.WhoIs(ctx, remoteAddr)
}

// Ping 测试连通性
func (c *SimpleClient) Ping(ctx context.Context, target string) error {
	_, err := c.localClient.Ping(ctx, netip.MustParseAddr(target), tailcfg.PingDisco)
	return err
}

// QuickConnect 快速连接 - 简化的连接方法
func (c *SimpleClient) QuickConnect(ctx context.Context, authKey, controlURL, hostname string) error {
	log.Println("🚀 快速连接模式")

	options := ClientOptions{
		AuthKey:      authKey,
		ControlURL:   controlURL,
		Hostname:     hostname,
		AcceptRoutes: true,
		ShieldsUp:    false,
	}

	return c.UpWithOptions(ctx, options)
}

// ForceLogin 强制重新登录
func (c *SimpleClient) ForceLogin(ctx context.Context, options ClientOptions) error {
	log.Println("🔄 开始强制重新登录...")

	// 强制登出
	prefs := ipn.NewPrefs()
	prefs.WantRunning = false
	prefs.LoggedOut = true

	maskedPrefs := &ipn.MaskedPrefs{
		Prefs:          *prefs,
		WantRunningSet: true,
		LoggedOutSet:   true,
	}

	_, err := c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		log.Printf("强制登出失败: %v", err)
	}

	time.Sleep(3 * time.Second)
	return c.UpWithOptions(ctx, options)
}

// parseTailscaleCommand 从 tailscale 命令输出中解析完整的命令参数
func (c *SimpleClient) parseTailscaleCommand(output string) ([]string, error) {
	log.Println("解析 tailscale 命令输出...")

	// 按行分割输出
	lines := strings.Split(output, "\n")

	// 查找包含 "tailscale up" 的行
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// 查找以 "tailscale up" 开头的行
		if strings.HasPrefix(line, "tailscale up") {
			log.Printf("找到命令行: %s", line)

			// 分割命令和参数
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}

			// 跳过 "tailscale" 和 "up"，只返回参数部分
			args := parts[2:]

			// 添加 socket 路径
			result := []string{"--socket", c.socketPath, "up"}
			result = append(result, args...)

			log.Printf("解析到的参数: %v", result)
			return result, nil
		}

		// 查找包含 "Usage:" 或 "Example:" 的行
		if strings.Contains(line, "Usage:") || strings.Contains(line, "Example:") {
			// 提取下一行或当前行中的命令部分
			if strings.Contains(line, "tailscale up") {
				// 从当前行提取
				startIdx := strings.Index(line, "tailscale up")
				if startIdx >= 0 {
					commandPart := line[startIdx:]
					parts := strings.Fields(commandPart)
					if len(parts) >= 2 {
						args := parts[2:]
						result := []string{"--socket", c.socketPath, "up"}
						result = append(result, args...)
						log.Printf("从 Usage 行解析到的参数: %v", result)
						return result, nil
					}
				}
			}
		}
	}

	// 如果没有找到明确的命令，尝试查找包含必要参数的行
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "--login-server") || strings.Contains(line, "--hostname") {
			log.Printf("找到包含参数的行: %s", line)

			// 提取参数部分
			if strings.Contains(line, "tailscale up") {
				startIdx := strings.Index(line, "tailscale up")
				commandPart := line[startIdx:]
				parts := strings.Fields(commandPart)
				if len(parts) >= 2 {
					args := parts[2:]
					result := []string{"--socket", c.socketPath, "up"}
					result = append(result, args...)
					log.Printf("从参数行解析到的参数: %v", result)
					return result, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("无法从输出中解析到有效的 tailscale up 命令")
}
