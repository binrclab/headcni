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
	"sync"
	"time"

	"github.com/binrclab/headcni/pkg/constants"
	"tailscale.com/client/local"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
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

// 辅助方法：创建WantRunning的MaskedPrefs
func (c *SimpleClient) createWantRunningPrefs(wantRunning bool) *ipn.MaskedPrefs {
	prefs := ipn.NewPrefs()
	prefs.WantRunning = wantRunning
	return &ipn.MaskedPrefs{
		Prefs:          *prefs,
		WantRunningSet: true,
	}
}

// 辅助方法：创建基础配置的MaskedPrefs
func (c *SimpleClient) createBasicPrefs(options ClientOptions) *ipn.MaskedPrefs {
	prefs := ipn.NewPrefs()
	prefs.ControlURL = options.ControlURL
	prefs.Hostname = options.Hostname
	prefs.WantRunning = false
	prefs.LoggedOut = false
	prefs.RouteAll = options.AcceptRoutes
	prefs.ShieldsUp = options.ShieldsUp

	if len(options.AdvertiseRoutes) > 0 {
		var routes []netip.Prefix
		for _, route := range options.AdvertiseRoutes {
			if prefix, err := netip.ParsePrefix(route); err == nil {
				routes = append(routes, prefix)
			}
		}
		prefs.AdvertiseRoutes = routes
	}

	return &ipn.MaskedPrefs{
		Prefs:              *prefs,
		ControlURLSet:      true,
		HostnameSet:        options.Hostname != "",
		WantRunningSet:     true,
		LoggedOutSet:       true,
		RouteAllSet:        true,
		AdvertiseRoutesSet: len(options.AdvertiseRoutes) > 0,
		ShieldsUpSet:       true,
	}
}

// 辅助方法：等待状态变化
func (c *SimpleClient) waitForStateChange(ctx context.Context, targetState string, maxWait int) error {
	for i := 0; i < maxWait; i++ {
		time.Sleep(1 * time.Second)
		if status, err := c.GetStatus(ctx); err == nil && status.BackendState == targetState {
			return nil
		}
	}
	return fmt.Errorf("等待状态变化到 %s 超时", targetState)
}

// 辅助方法：创建路由相关的MaskedPrefs
func (c *SimpleClient) createRoutePrefs(routes []netip.Prefix, routeAll *bool, hostname string) *ipn.MaskedPrefs {
	prefs := ipn.NewPrefs()
	if routes != nil {
		prefs.AdvertiseRoutes = routes
	}
	if routeAll != nil {
		prefs.RouteAll = *routeAll
	}
	if hostname != "" {
		prefs.Hostname = hostname
	}

	maskedPrefs := &ipn.MaskedPrefs{
		Prefs: *prefs,
	}

	if routes != nil {
		maskedPrefs.AdvertiseRoutesSet = true
	}
	if routeAll != nil {
		maskedPrefs.RouteAllSet = true
	}
	if hostname != "" {
		maskedPrefs.HostnameSet = true
	}

	return maskedPrefs
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

	maskedPrefs := c.createWantRunningPrefs(false)
	_, err = c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		return fmt.Errorf("停止连接失败: %v", err)
	}

	// 等待连接停止
	if err := c.waitForStateChange(ctx, "Stopped", 10); err == nil {
		log.Println("连接已成功停止")
		return nil
	}

	log.Println("连接停止命令已发送")
	return nil
}

// UpWithOptionsWithRetry - 带重试机制的登录方法
func (c *SimpleClient) UpWithOptionsWithRetry(ctx context.Context, options ClientOptions) error {
	maxRetries := 2
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		log.Printf("尝试第 %d/%d 次登录", attempt, maxRetries)

		err := c.UpWithOptions(ctx, options)
		if err == nil {
			log.Printf("✅ 第 %d 次尝试成功!", attempt)
			return nil
		}

		log.Printf("❌ 第 %d 次尝试失败: %v", attempt, err)
		lastErr = err

		if attempt < maxRetries {
			log.Printf("等待15秒后重试...")
			time.Sleep(15 * time.Second)
		}
	}

	return fmt.Errorf("所有 %d 次尝试都失败了，最后错误: %v", maxRetries, lastErr)
}

// 修复版本的 UpWithOptions - 解决 Headscale 认证问题
func (c *SimpleClient) UpWithOptions(ctx context.Context, options ClientOptions) error {
	log.Printf("开始Tailscale登录流程")
	log.Printf("控制URL: %s", options.ControlURL)
	log.Printf("主机名: %s", options.Hostname)
	log.Printf("认证密钥: %s...", c.maskAuthKey(options.AuthKey))
	log.Printf("Socket路径: %s", c.socketPath)

	// 验证必要参数
	if err := c.validateOptions(options); err != nil {
		return fmt.Errorf("参数验证失败: %v", err)
	}

	if err := c.waitForDaemonReady(ctx); err != nil {
		return fmt.Errorf("waitForDaemonReady 失败: %w", err)
	}

	// 步骤2: 检查并复用现有状态
	if err := c.checkAndReuseExistingState(ctx, options); err == nil {
		log.Println("复用现有状态，登录流程完成")
		return nil
	}

	if err := c.completeReset(ctx); err != nil {
		return fmt.Errorf("completeReset 失败: %w", err)
	}
	log.Printf("completeReset 完成")
	// 关键修复2: 分步骤精确设置
	if err := c.preciseSetup(ctx, options); err != nil {
		return fmt.Errorf("精确设置失败: %v", err)
	}

	// 关键修复3: 改进的认证流程
	if err := c.improvedAuthentication(ctx, options); err != nil {
		return fmt.Errorf("认证失败: %v", err)
	}

	// 步骤4: 等待最终连接完成
	if err := c.waitForFullConnection(ctx); err != nil {
		return fmt.Errorf("等待连接完成失败: %v", err)
	}

	log.Println("修复版登录流程完成")
	return nil
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
				maskedPrefs := c.createWantRunningPrefs(true)

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

// waitForDaemonReady 等待守护进程就绪
func (c *SimpleClient) waitForDaemonReady(ctx context.Context) error {
	log.Println("等待 Tailscale 守护进程就绪...")

	for i := 0; i < 30; i++ {
		status, err := c.GetStatus(ctx)
		if err != nil {
			log.Printf("守护进程检查 %d/30: 连接失败 - %v", i+1, err)
			time.Sleep(1 * time.Second)
			continue
		}

		// 检查守护进程是否处于稳定状态
		if status.BackendState == "Stopped" || status.BackendState == "NeedsLogin" {
			log.Printf("守护进程就绪: %s", status.BackendState)
			// 额外等待2秒确保稳定
			time.Sleep(2 * time.Second)
			return nil
		}

		if i%10 == 0 || i < 3 {
			log.Printf("守护进程检查 %d/30: %s", i+1, status.BackendState)
		}
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("守护进程30秒内未就绪")
}

// completeReset 智能重置状态（优化版）
func (c *SimpleClient) completeReset(ctx context.Context) error {
	log.Println("智能重置连接状态")

	// 获取当前状态
	status, err := c.GetStatus(ctx)
	if err != nil {
		log.Printf("无法获取状态: %v", err)
		return nil
	}

	log.Printf("重置前状态: %s", status.BackendState)

	// 智能判断是否需要重置
	switch status.BackendState {
	case "Stopped":
		log.Println("已经是停止状态，跳过重置")
		return nil
	case "NeedsLogin":
		// 检查是否有残留的认证状态
		if status.Self != nil && len(status.Self.TailscaleIPs) > 0 {
			log.Println("NeedsLogin状态但有残留IP，需要完整重置")
		} else {
			log.Println("干净的 NeedsLogin 状态，跳过重置")
			return nil
		}
	case "Running":
		log.Println("当前正在运行，需要重置")
	case "Starting":
		log.Println("正在启动中，等待完成或重置")
	default:
		log.Printf("未知状态 %s，尝试重置", status.BackendState)
	}

	// 执行重置
	prefs := ipn.NewPrefs()
	prefs.WantRunning = false
	prefs.LoggedOut = true

	maskedPrefs := &ipn.MaskedPrefs{
		Prefs:          *prefs,
		WantRunningSet: true,
		LoggedOutSet:   true,
	}

	_, err = c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		log.Printf("停止连接失败: %v", err)
		return err
	}

	// 智能等待 - 根据初始状态调整等待时间
	maxWait := 10 // 默认10秒
	if status.BackendState == "Running" {
		maxWait = 15 // Running状态需要更多时间停止
	}

	log.Printf("等待状态重置（最多%d秒）...", maxWait)

	// 等待状态变为Stopped或NeedsLogin
	for i := 0; i < maxWait; i++ {
		time.Sleep(1 * time.Second)
		if status, err := c.GetStatus(ctx); err == nil {
			if i%5 == 0 || status.BackendState != "Stopping" {
				log.Printf("重置进度 %d/%d: %s", i+1, maxWait, status.BackendState)
			}

			if status.BackendState == "Stopped" || status.BackendState == "NeedsLogin" {
				log.Printf("✅ 状态重置完成: %s", status.BackendState)
				time.Sleep(1 * time.Second) // 短暂等待状态稳定
				return nil
			}
		}
	}

	// 检查最终状态
	if finalStatus, err := c.GetStatus(ctx); err == nil {
		if finalStatus.BackendState == "NeedsLogin" || finalStatus.BackendState == "Stopped" {
			log.Printf("✅ 重置完成: %s", finalStatus.BackendState)
			return nil
		}
		log.Printf("⚠️ 重置可能不完整，当前状态: %s", finalStatus.BackendState)
	}

	log.Println("状态重置完成")
	return nil
}

// preciseSetup 精确设置配置（增强版）
func (c *SimpleClient) preciseSetup(ctx context.Context, options ClientOptions) error {
	log.Println("精确配置设置")

	// 直接使用辅助方法创建配置，无需获取当前配置

	// 使用辅助方法创建基础配置
	maskedPrefs := c.createBasicPrefs(options)

	log.Printf("应用精确配置...")
	_, err := c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		return fmt.Errorf("精确配置失败: %v", err)
	}

	// 增加等待时间确保配置生效
	log.Println("等待配置生效...")
	time.Sleep(5 * time.Second) // 从3秒增加到5秒

	// 验证配置
	updatedPrefs, err := c.localClient.GetPrefs(ctx)
	if err == nil {
		// 验证关键配置是否正确应用
		if updatedPrefs.ControlURL != options.ControlURL {
			return fmt.Errorf("控制URL配置验证失败: 期望 %s, 实际 %s", options.ControlURL, updatedPrefs.ControlURL)
		}
	}

	log.Println("精确配置完成")
	return nil
}

// improvedAuthentication 优化的认证流程
func (c *SimpleClient) improvedAuthentication(ctx context.Context, options ClientOptions) error {
	log.Println("优化的认证流程")
	// 如果是 "auto" 模式，处理现有状态
	if options.AuthKey == "auto" {
		return c.handleAutoModeAPI(ctx, options)
	}
	// 3.1 检查当前状态
	status, err := c.GetStatus(ctx)
	if err != nil {
		return fmt.Errorf("无法获取当前状态: %v", err)
	}

	log.Printf("认证前状态: %s", status.BackendState)

	// 如果已经在运行，检查是否需要重新认证
	if status.BackendState == "Running" {
		if c.isLoginComplete(status) {
			log.Println("✅ 已经登录完成，跳过认证")
			return nil
		}
		log.Println("Running 但登录不完整，继续认证流程")
	}

	// 3.2 启用运行状态
	log.Println("启用运行状态")
	prefs := ipn.NewPrefs()
	prefs.WantRunning = true

	maskedPrefs := &ipn.MaskedPrefs{
		Prefs:          *prefs,
		WantRunningSet: true,
	}

	_, err = c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		return fmt.Errorf("启用运行状态失败: %v", err)
	}

	// 3.3 快速检查状态变化
	log.Println("检查状态变化...")
	time.Sleep(2 * time.Second)

	var finalState string
	for i := 0; i < 60; i++ { // 减少到10次检查
		status, err := c.GetStatus(ctx)
		if err != nil {
			log.Printf("状态检查失败 %d: %v", i+1, err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		finalState = status.BackendState
		log.Printf("状态检查 %d/10: %s", i+1, status.BackendState)

		if status.BackendState == "Running" {
			if c.isLoginComplete(status) {
				log.Println("✅ 直接进入完整 Running 状态")
				return nil
			}
			log.Println("Running 但不完整，继续认证")
		}

		if status.BackendState == "NeedsLogin" {
			log.Println("✅ 进入 NeedsLogin 状态，开始认证")
			break
		}

		time.Sleep(500 * time.Millisecond)
	}

	// 3.4 发送认证请求并等待初步响应
	if finalState == "NeedsLogin" {
		log.Println("发送认证请求")
		startOptions := ipn.Options{
			AuthKey: options.AuthKey,
		}
		// 创建预清理配置
		prefs := ipn.NewPrefs()
		prefs.ControlURL = options.ControlURL
		prefs.LoggedOut = true
		prefs.WantRunning = false

		_, err := c.localClient.EditPrefs(ctx, &ipn.MaskedPrefs{
			Prefs:          *prefs,
			ControlURLSet:  true,
			LoggedOutSet:   true,
			WantRunningSet: true,
		})
		if err != nil {
			return fmt.Errorf("预清理失败: %w", err)
		}

		log.Printf("使用认证密钥: %s...", c.maskAuthKey(options.AuthKey))
		err = c.localClient.Start(ctx, startOptions)
		if err != nil {
			return fmt.Errorf("Start 命令失败: %v", err)
		}

		// 3) 再开启 WantRunning
		err = c.enableRunningAfterAuth(ctx)
		if err != nil {
			return fmt.Errorf("启用运行状态失败: %v", err)
		}
		// 检查认证是否成功
		if err := c.waitForAuthCompletion(ctx); err != nil {
			log.Printf("认证方法 完成失败: %v", err)
			return err
		}
	}

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

		// 每10秒详细打印状态
		if i%10 == 0 {
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
	}

	if currentPrefs.RouteAll != options.AcceptRoutes {
		updatePrefs.RouteAll = options.AcceptRoutes
		needUpdate = true
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

// smartWaitForLogin 智能等待登录完成（修复版）

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
		if i%10 == 0 || i < 3 {
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
				if i%20 == 0 {
					log.Printf("状态Running但IP未分配，继续等待...")
				}
			}

		case "Starting":
			if i%20 == 0 {
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
					if i%30 == 0 { // 每60秒诊断一次
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

// logConnectionInfo 记录连接信息
func (c *SimpleClient) logConnectionInfo(status *ipnstate.Status) {
	if status.Self == nil {
		return
	}

	log.Printf("连接成功: 节点名=%s, 在线=%v, IP数量=%d, 对等节点=%d",
		status.Self.HostName, status.Self.Online, len(status.Self.TailscaleIPs), len(status.Peer))
}

// diagnoseNetworkIssues 诊断网络问题
func (c *SimpleClient) diagnoseNetworkIssues(ctx context.Context) {
	log.Println("诊断网络问题...")

	// 检查偏好设置
	prefs, err := c.localClient.GetPrefs(ctx)
	if err != nil {
		log.Printf("无法获取偏好设置: %v", err)
		return
	}

	log.Printf("当前配置: ControlURL=%s, Hostname=%s, WantRunning=%v, LoggedOut=%v",
		prefs.ControlURL, prefs.Hostname, prefs.WantRunning, prefs.LoggedOut)

	// 测试控制服务器连接
	if err := c.checkHeadscaleReachability(); err != nil {
		log.Printf("⚠️ 控制服务器连接问题: %v", err)
	} else {
		log.Println("✅ 控制服务器连接正常")
	}
}

// checkHeadscaleReachability 检查 Headscale 服务器可达性
func (c *SimpleClient) checkHeadscaleReachability() error {
	log.Println("检查 Headscale 服务器可达性...")

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

	// 尝试HTTP请求
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(controlURL)
	if err != nil {
		return fmt.Errorf("HTTP请求失败: %v", err)
	}
	defer resp.Body.Close()

	log.Printf("网络检查成功: TCP=%s, HTTP=%d", u.Host, resp.StatusCode)

	return nil
}

// 添加调试方法：直接验证认证密钥和最简单的登录尝试
func (c *SimpleClient) DebugAuthKey(ctx context.Context, authKey, controlURL string) {
	log.Println("调试认证密钥...")
	log.Printf("调试信息: 密钥长度=%d, 控制URL=%s", len(authKey), controlURL)
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
	maskedPrefs := c.createRoutePrefs(routes, nil, "")
	_, err := c.localClient.EditPrefs(ctx, maskedPrefs)
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
	routeAll := true
	maskedPrefs := c.createRoutePrefs(nil, &routeAll, "")
	_, err := c.localClient.EditPrefs(ctx, maskedPrefs)
	return err
}

// RejectRoutes 拒绝路由
func (c *SimpleClient) RejectRoutes(ctx context.Context) error {
	routeAll := false
	maskedPrefs := c.createRoutePrefs(nil, &routeAll, "")
	_, err := c.localClient.EditPrefs(ctx, maskedPrefs)
	return err
}

// SetHostname 设置主机名
func (c *SimpleClient) SetHostname(ctx context.Context, hostname string) error {
	maskedPrefs := c.createRoutePrefs(nil, nil, hostname)
	_, err := c.localClient.EditPrefs(ctx, maskedPrefs)
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
	log.Println("快速连接模式")

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
	log.Println("开始强制重新登录...")

	// 强制登出 - 使用辅助方法
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
