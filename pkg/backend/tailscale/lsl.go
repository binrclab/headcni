package tailscale

import (
	"context"
	"fmt"
	"log"
	"net/netip"
	"time"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
)

// 修复版本的 UpWithOptions - 解决 Headscale 认证问题
func (c *SimpleClient) UpWithOptions(ctx context.Context, options ClientOptions) error {
	log.Printf("=== 开始修复版全自动Tailscale登录流程 ===")
	log.Printf("控制URL: %s", options.ControlURL)
	log.Printf("主机名: %s", options.Hostname)
	log.Printf("认证密钥: %s...", c.maskAuthKey(options.AuthKey))

	// 验证必要参数
	if err := c.validateOptions(options); err != nil {
		return fmt.Errorf("参数验证失败: %v", err)
	}

	// 关键修复1: 完全重置状态
	if err := c.completeReset(ctx); err != nil {
		log.Printf("重置状态警告: %v", err)
	}

	// 关键修复2: 分步骤精确设置
	if err := c.preciseSetup(ctx, options); err != nil {
		return fmt.Errorf("精确设置失败: %v", err)
	}

	// 关键修复3: 改进的认证流程
	if err := c.improvedAuthentication(ctx, options); err != nil {
		return fmt.Errorf("认证失败: %v", err)
	}

	// 关键修复4: 智能等待和验证
	if err := c.smartWaitForLogin(ctx); err != nil {
		return fmt.Errorf("登录失败: %v", err)
	}

	log.Println("=== 修复版登录流程完成 ===")
	return nil
}

// completeReset 完全重置状态
func (c *SimpleClient) completeReset(ctx context.Context) error {
	log.Println("步骤1: 完全重置连接状态")

	// 获取当前状态
	status, err := c.GetStatus(ctx)
	if err != nil {
		log.Printf("无法获取状态: %v", err)
		return nil
	}

	log.Printf("重置前状态: %s", status.BackendState)

	// 强制停止所有连接
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
	}

	// 等待状态稳定 - 关键：确保完全停止
	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Second)
		if status, err := c.GetStatus(ctx); err == nil {
			log.Printf("重置进度 %d/10: %s", i+1, status.BackendState)
			if status.BackendState == "Stopped" || status.BackendState == "NeedsLogin" {
				break
			}
		}
	}

	log.Println("状态重置完成")
	return nil
}

// preciseSetup 精确设置配置
func (c *SimpleClient) preciseSetup(ctx context.Context, options ClientOptions) error {
	log.Println("步骤2: 精确配置设置")

	// 关键修复：使用 GetPrefs 获取当前配置，然后精确修改
	currentPrefs, err := c.localClient.GetPrefs(ctx)
	if err != nil {
		log.Printf("无法获取当前配置，使用默认配置: %v", err)
		currentPrefs = ipn.NewPrefs()
	}

	// 克隆当前配置
	newPrefs := *currentPrefs

	// 精确设置必需的字段
	newPrefs.ControlURL = options.ControlURL
	newPrefs.WantRunning = false // 重要：先不启动
	newPrefs.LoggedOut = false

	if options.Hostname != "" {
		newPrefs.Hostname = options.Hostname
	}

	if options.AcceptRoutes {
		newPrefs.RouteAll = true
	}

	if len(options.AdvertiseRoutes) > 0 {
		var routes []netip.Prefix
		for _, route := range options.AdvertiseRoutes {
			if prefix, err := netip.ParsePrefix(route); err == nil {
				routes = append(routes, prefix)
			}
		}
		newPrefs.AdvertiseRoutes = routes
	}

	newPrefs.ShieldsUp = options.ShieldsUp

	// 应用配置 - 关键：精确指定哪些字段被设置
	maskedPrefs := &ipn.MaskedPrefs{
		Prefs:              newPrefs,
		ControlURLSet:      true,
		WantRunningSet:     true,
		LoggedOutSet:       true,
		HostnameSet:        options.Hostname != "",
		RouteAllSet:        options.AcceptRoutes,
		AdvertiseRoutesSet: len(options.AdvertiseRoutes) > 0,
		ShieldsUpSet:       true,
	}

	log.Printf("应用精确配置...")
	_, err = c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		return fmt.Errorf("精确配置失败: %v", err)
	}

	// 等待配置生效
	time.Sleep(3 * time.Second)

	// 验证配置
	updatedPrefs, err := c.localClient.GetPrefs(ctx)
	if err == nil {
		log.Printf("配置验证 - 控制URL: %s", updatedPrefs.ControlURL)
		log.Printf("配置验证 - WantRunning: %v", updatedPrefs.WantRunning)
	}

	log.Println("精确配置完成")
	return nil
}

// improvedAuthentication 改进的认证流程
func (c *SimpleClient) improvedAuthentication(ctx context.Context, options ClientOptions) error {
	log.Println("步骤3: 改进的认证流程")

	// 关键修复：不使用 Start，而是分步进行

	// 3.1 首先启用 WantRunning
	log.Println("3.1 启用运行状态")
	prefs := ipn.NewPrefs()
	prefs.WantRunning = true

	maskedPrefs := &ipn.MaskedPrefs{
		Prefs:          *prefs,
		WantRunningSet: true,
	}

	_, err := c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		return fmt.Errorf("启用运行状态失败: %v", err)
	}

	// 等待进入需要登录状态
	time.Sleep(2 * time.Second)

	// 3.2 检查是否进入 NeedsLogin 状态
	status, err := c.GetStatus(ctx)
	if err != nil {
		return fmt.Errorf("无法获取状态: %v", err)
	}

	log.Printf("启用运行后状态: %s", status.BackendState)

	if status.BackendState != "NeedsLogin" {
		log.Println("等待进入 NeedsLogin 状态...")
		for i := 0; i < 10; i++ {
			time.Sleep(1 * time.Second)
			if status, err := c.GetStatus(ctx); err == nil && status.BackendState == "NeedsLogin" {
				break
			}
		}
	}

	// 3.3 关键修复：正确的认证方式
	log.Println("3.3 使用 Start 方法进行认证")

	// 简洁的 Start 选项，避免使用可能不存在的字段
	startOptions := ipn.Options{
		AuthKey: options.AuthKey,
	}

	log.Printf("发送 Start 命令，使用认证密钥: %s...", c.maskAuthKey(options.AuthKey))
	err = c.localClient.Start(ctx, startOptions)
	if err != nil {
		return fmt.Errorf("Start 命令失败: %v", err)
	}

	log.Println("Start 命令发送成功，等待认证...")

	// 立即检查状态变化
	time.Sleep(2 * time.Second)
	status, err = c.GetStatus(ctx)
	if err == nil {
		log.Printf("Start 后状态: %s", status.BackendState)
		if status.BackendState == "NeedsLogin" {
			log.Println("⚠️ Start 后仍需要登录，可能是认证密钥问题")
		}
	}

	log.Println("认证请求已发送")
	return nil
}

// smartWaitForLogin 智能等待登录完成
func (c *SimpleClient) smartWaitForLogin(ctx context.Context) error {
	log.Println("步骤4: 智能等待登录完成")

	maxWaitSeconds := 120 // 减少到2分钟，更快失败
	checkInterval := 1 * time.Second
	consecutiveNeedsLogin := 0
	maxConsecutiveNeedsLogin := 30 // 30秒后如果还是 NeedsLogin 就报错

	log.Printf("开始智能等待，最多%d秒", maxWaitSeconds)

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

		// 每10秒打印一次状态
		if i%10 == 0 || i < 10 {
			log.Printf("等待进度 %d/%ds - 状态: %s", i+1, maxWaitSeconds, status.BackendState)
			if status.Self != nil {
				log.Printf("  节点信息 - 名称: %s, IP数量: %d, 在线: %v",
					status.Self.HostName, len(status.Self.TailscaleIPs), status.Self.Online)
			}
		}

		switch status.BackendState {
		case "Running":
			if c.isLoginComplete(status) {
				log.Printf("✅ 登录成功! 耗时: %d秒", i+1)
				// c.logSuccessDetails(status)
				return nil
			}
			log.Printf("Running 但信息不完整，继续等待...")

		case "NeedsLogin":
			consecutiveNeedsLogin++
			if consecutiveNeedsLogin >= maxConsecutiveNeedsLogin {
				return c.analyzeNeedsLoginFailure(status)
			}

		case "Starting":
			consecutiveNeedsLogin = 0 // 重置计数
			if i%5 == 0 {
				log.Println("正在启动...")
			}

		case "Stopped":
			return fmt.Errorf("连接意外停止")

		default:
			log.Printf("未知状态: %s", status.BackendState)
		}
	}

	// 超时分析
	finalStatus, _ := c.GetStatus(ctx)
	return c.analyzeTimeoutFailure(finalStatus)
}

// analyzeNeedsLoginFailure 分析 NeedsLogin 失败原因
func (c *SimpleClient) analyzeNeedsLoginFailure(status *ipnstate.Status) error {
	log.Println("❌ 持续 NeedsLogin 状态，分析原因:")

	reasons := []string{
		"认证密钥可能无效、过期或格式错误",
		"Headscale 服务器可能拒绝了认证请求",
		"网络连接到控制服务器可能有问题",
		"控制服务器 URL 可能不正确",
	}

	for i, reason := range reasons {
		log.Printf("  %d. %s", i+1, reason)
	}

	return fmt.Errorf("认证失败：30秒内一直处于 NeedsLogin 状态")
}

// analyzeTimeoutFailure 分析超时失败原因
func (c *SimpleClient) analyzeTimeoutFailure(status *ipnstate.Status) error {
	log.Printf("❌ 登录超时，最终状态: %s", status.BackendState)

	switch status.BackendState {
	case "NeedsLogin":
		return fmt.Errorf("登录超时：认证密钥无效或服务器拒绝")
	case "Starting":
		return fmt.Errorf("登录超时：启动过程卡住，可能是网络问题")
	case "Running":
		return fmt.Errorf("登录超时：状态为Running但IP信息不完整")
	default:
		return fmt.Errorf("登录超时：未知状态 %s", status.BackendState)
	}
}

// 添加调试方法：直接验证认证密钥和最简单的登录尝试
func (c *SimpleClient) DebugAuthKey(ctx context.Context, authKey, controlURL string) {
	log.Println("🔍 调试认证密钥...")
	log.Printf("密钥长度: %d", len(authKey))
	log.Printf("密钥前缀: %s", authKey[:min(20, len(authKey))])
	log.Printf("控制URL: %s", controlURL)

	// 检查当前偏好设置
	if prefs, err := c.GetPrefs(ctx); err == nil {
		log.Printf("当前控制URL: %s", prefs.ControlURL)
		log.Printf("当前WantRunning: %v", prefs.WantRunning)
		log.Printf("当前LoggedOut: %v", prefs.LoggedOut)
	}
}

// SimpleLogin 最简单的登录尝试 - 用于调试
func (c *SimpleClient) SimpleLogin(ctx context.Context, authKey, controlURL string) error {
	log.Println("🎯 尝试最简单的登录方式...")

	// 步骤1: 设置基础配置
	prefs := ipn.NewPrefs()
	prefs.ControlURL = controlURL
	prefs.WantRunning = false // 先不运行
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

	time.Sleep(2 * time.Second)

	// 步骤2: 启用运行
	prefs.WantRunning = true
	maskedPrefs.WantRunningSet = true

	_, err = c.localClient.EditPrefs(ctx, maskedPrefs)
	if err != nil {
		return fmt.Errorf("启用运行失败: %v", err)
	}

	time.Sleep(3 * time.Second)

	// 步骤3: 发送 Start 命令
	startOptions := ipn.Options{
		AuthKey: authKey,
	}

	log.Printf("发送 Start 命令...")
	err = c.localClient.Start(ctx, startOptions)
	if err != nil {
		return fmt.Errorf("Start 命令失败: %v", err)
	}

	// 步骤4: 简单等待
	log.Println("等待30秒看结果...")
	for i := 0; i < 30; i++ {
		time.Sleep(1 * time.Second)
		status, err := c.GetStatus(ctx)
		if err != nil {
			continue
		}

		if i%5 == 0 {
			log.Printf("第%d秒 - 状态: %s", i+1, status.BackendState)
		}

		if status.BackendState == "Running" && c.isLoginComplete(status) {
			log.Printf("✅ 简单登录成功! 耗时: %d秒", i+1)
			return nil
		}
	}

	return fmt.Errorf("简单登录失败")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
