package daemon

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/binrclab/headcni/cmd/daemon/config"
	"github.com/binrclab/headcni/pkg/backend/tailscale"
	"github.com/binrclab/headcni/pkg/constants"
	"github.com/binrclab/headcni/pkg/headscale"
	"github.com/binrclab/headcni/pkg/logging"
	"github.com/binrclab/headcni/pkg/utils"
	"github.com/vishvananda/netlink"
	coreV1 "k8s.io/api/core/v1"
)

// TailscaleServiceState 服务状态（避免与 services.go 中的 ServiceState 冲突）
type TailscaleServiceState string

const (
	TailscaleServiceStateInitializing TailscaleServiceState = "initializing"
	TailscaleServiceStateRunning      TailscaleServiceState = "running"
	TailscaleServiceStateError        TailscaleServiceState = "error"
	TailscaleServiceStateRestarting   TailscaleServiceState = "restarting"
	TailscaleServiceStateStopped      TailscaleServiceState = "stopped"
)

type TailscaleEnv struct {
	isDaemon     bool
	configDir    string
	socketPath   string
	statePath    string
	pidPath      string
	hostNamePath string
	hostName     string
	tailscaleNic string
}

// TailscaleService 管理 Tailscale 服务进程，实现 Service 接口
type TailscaleService struct {
	preparer           *Preparer
	authKey            string
	authKeyExpiredTime time.Time
	hostname           string
	serviceName        string

	// 状态管理
	tailscaleEnv *TailscaleEnv
	state        TailscaleServiceState
	stateMu      sync.RWMutex
	lastError    error
	startTime    time.Time

	// 重试配置
	maxRetries    int
	retryInterval time.Duration
	retryCount    int

	// 健康检查
	healthCheckInterval time.Duration

	// 控制
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	mu        sync.Mutex // 保护 isRunning
	isRunning bool
}

// NewTailscaleService 创建新的 Tailscale 服务
func NewTailscaleService(
	preparer *Preparer,
) *TailscaleService {
	ctx, cancel := context.WithCancel(context.Background())
	return &TailscaleService{
		preparer:            preparer,
		serviceName:         constants.DefaultTailscaleServiceName,
		state:               TailscaleServiceStateInitializing,
		maxRetries:          5,
		retryInterval:       30 * time.Second,
		healthCheckInterval: 30 * time.Second,
		ctx:                 ctx,
		cancel:              cancel,
	}
}

// =============================================================================
// Service 接口实现
// =============================================================================

// Name 返回服务名称 (Service 接口)
func (tsm *TailscaleService) Name() string {
	return constants.ServiceNameTailscale
}

// =============================================================================
// 公共工具函数
// =============================================================================

// updateHealthStatus 更新健康状态（消除重复代码）
func (tsm *TailscaleService) updateHealthStatus(healthy bool, err error) {
	healthMgr := GetGlobalHealthManager()
	healthMgr.UpdateServiceStatus(tsm.Name(), healthy, err)
}

// updateHealthStatusWithLog 更新健康状态并记录日志
func (tsm *TailscaleService) updateHealthStatusWithLog(healthy bool, err error, format string, args ...interface{}) {
	if err != nil {
		logging.Errorf(format, args...)
	} else {
		logging.Infof(format, args...)
	}
	tsm.updateHealthStatus(healthy, err)
}

// getTailscaleEnv 获取 Tailscale 环境配置
func (tsm *TailscaleService) getTailscaleEnv() *TailscaleEnv {
	return tsm.tailscaleEnv
}

// setTailscaleEnv 设置 Tailscale 环境配置
func (tsm *TailscaleService) setTailscaleEnv(tailscaleEnv *TailscaleEnv) {
	tsm.tailscaleEnv = tailscaleEnv
}

// cleanupExpiredAuthKey 清理过期的认证密钥
func (tsm *TailscaleService) cleanupExpiredAuthKey() {
	if tsm.authKey != "" && !tsm.authKeyExpiredTime.IsZero() && tsm.authKeyExpiredTime.Before(time.Now()) {
		logging.Infof("清理过期的认证密钥 (过期时间: %v)", tsm.authKeyExpiredTime)
		tsm.authKey = ""
		tsm.authKeyExpiredTime = time.Time{}
	}
}

// validateAuthKey 验证认证密钥是否有效
func (tsm *TailscaleService) validateAuthKey() bool {
	if tsm.authKey == "" {
		return false
	}

	if tsm.authKeyExpiredTime.IsZero() {
		return false
	}

	return tsm.authKeyExpiredTime.After(time.Now())
}

// initTailscaleEnv 初始化 Tailscale 环境配置
func (tsm *TailscaleService) initTailscaleEnv(node *coreV1.Node) *TailscaleEnv {
	isHost := tsm.preparer.GetConfig().Tailscale.Mode == "host"
	if isHost {
		// 验证 host 模式的路径
		configDir := filepath.Dir(constants.DefaultTailscaleHostSocketPath)
		if configDir == "" || configDir == "." {
			logging.Warnf("Invalid config directory path derived from socket path: %s", constants.DefaultTailscaleHostSocketPath)
			configDir = "/var/run/headcni" // 使用默认路径作为后备
		}

		tailscaleEnv := &TailscaleEnv{
			isDaemon:     isHost,
			configDir:    configDir,
			socketPath:   constants.DefaultTailscaleHostSocketPath,
			statePath:    "",
			pidPath:      "",
			hostNamePath: "",
			hostName:     node.Name,
			tailscaleNic: "tailscale0",
		}
		logging.Infof("Initialized host mode environment - ConfigDir: %s, SocketPath: %s", configDir, constants.DefaultTailscaleHostSocketPath)
		return tailscaleEnv
	} else {
		// 验证 daemon 模式的路径
		stateDir := constants.DefaultTailscaleDaemonStateDir
		hostnamePath := filepath.Join(stateDir, "hostname")

		// 在 daemon 模式下，确保使用独特的接口名称
		interfaceName := tsm.preparer.GetConfig().Tailscale.InterfaceName
		if interfaceName == "" {
			// 使用默认的 headcni01 接口名称
			interfaceName = "headcni01"
		}
		configDir := filepath.Dir(tsm.preparer.GetConfig().Tailscale.Socket.Path)

		tailscaleEnv := &TailscaleEnv{
			isDaemon:     !isHost,
			configDir:    configDir,
			socketPath:   tsm.preparer.GetConfig().Tailscale.Socket.Path,
			statePath:    constants.DefaultTailscaleDaemonStateFile,
			pidPath:      filepath.Join(configDir, "tailscaled.pid"),
			hostNamePath: hostnamePath,
			hostName:     tsm.readHostNameInDomain(hostnamePath),
			tailscaleNic: interfaceName,
		}
		logging.Infof("Initialized daemon mode environment - ConfigDir: %s, StateDir: %s, SocketPath: %s, Interface: %s", configDir, stateDir, tsm.preparer.GetConfig().Tailscale.Socket.Path, interfaceName)
		return tailscaleEnv
	}
}

// readHostNameInDomain 从文件读取主机名，如果文件不存在则生成新的
func (tsm *TailscaleService) readHostNameInDomain(path string) string {
	if path == "" {
		return ""
	}

	// 生成主机名的辅助函数
	generateHostname := func() string {
		return tsm.preparer.GetConfig().Tailscale.Hostname.Prefix + "-" + utils.RandomBase32Low(5)
	}

	// 验证主机名格式的辅助函数
	isValidHostname := func(hostname string) bool {
		return strings.HasPrefix(hostname, tsm.preparer.GetConfig().Tailscale.Hostname.Prefix) &&
			len(hostname) <= 63 &&
			regexp.MustCompile("^[a-z0-9-]+$").MatchString(hostname)
	}

	// 尝试读取现有主机名
	if hostnameBytes, err := os.ReadFile(path); err == nil {
		hostname := string(hostnameBytes)
		if isValidHostname(hostname) {
			return hostname
		}
	}

	// 生成新主机名并写入文件
	hostname := generateHostname()
	os.WriteFile(path, []byte(hostname), 0644)
	return hostname
}

// Start 启动服务 (Service 接口)
func (tsm *TailscaleService) Start(ctx context.Context) error {
	tsm.mu.Lock()
	defer tsm.mu.Unlock()

	if tsm.isRunning {
		return nil
	}

	// 获取当前节点信息
	nodeName, err := tsm.preparer.GetK8sClient().GetCurrentNodeName()
	if err != nil {
		tsm.updateHealthStatus(false, err)
		return tsm.handleErrorWithLog(err, "Failed to get current node name: %w", err)
	}

	node, err := tsm.preparer.GetK8sClient().Nodes().Get(context.Background(), nodeName)
	if err != nil {
		tsm.updateHealthStatus(false, err)
		return tsm.handleErrorWithLog(err, "Failed to get current node: %w", err)
	}

	tsm.tailscaleEnv = tsm.initTailscaleEnv(node)
	tsm.hostname = node.Name

	// Headscale.AuthKey 是用于调用 Headscale API 的密钥，不是 Tailscale 登录密钥
	// 这里不需要设置 tsm.authKey，它会在需要时从 Headscale 获取

	// 根据配置模式选择启动方式
	mode := tsm.preparer.GetConfig().Tailscale.Mode
	var startErr error
	switch mode {
	case "host":
		startErr = tsm.startHostModeWithRoutes(node)
	case "daemon":
		startErr = tsm.startDaemonModeWithRoutes(node)
	default:
		startErr = fmt.Errorf("unknown tailscale mode: %s", mode)
	}

	if startErr != nil {
		tsm.updateHealthStatus(false, startErr)
		return fmt.Errorf("failed to start %s mode: %v", mode, startErr)
	}

	tsm.isRunning = true
	tsm.state = TailscaleServiceStateRunning
	tsm.startTime = time.Now()

	// 启动认证密钥过期监控
	go tsm.monitorAuthKeyExpiration(tsm.ctx)

	tsm.updateHealthStatus(true, nil)
	logging.Infof("Tailscale service started successfully for node: %s in %s mode", tsm.hostname, mode)
	return nil
}

func (tsm *TailscaleService) Reload(ctx context.Context) error {
	tsm.mu.Lock()
	defer tsm.mu.Unlock()

	logging.Infof("Reloading Tailscale service")

	if !tsm.isRunning {
		return fmt.Errorf("service is not running")
	}

	// 获取新的配置
	newConfig := tsm.preparer.GetConfig()
	if newConfig != nil && newConfig.ConfigPath != "" {
		if cfg, err := config.LoadConfig(newConfig.ConfigPath); err == nil {
			newConfig = cfg
		} else {
			logging.Warnf("Failed to reload config, using existing config: %v", err)
		}
	}

	// 使用通用配置检查函数
	if !tsm.checkConfigChanged() {
		logging.Infof("Tailscale configuration unchanged, no reload needed")
		return nil
	}

	logging.Infof("Tailscale configuration changed, performing reload")

	// 停止当前服务
	if err := tsm.Stop(ctx); err != nil {
		logging.Errorf("Failed to stop service during reload: %v", err)
	}

	// 重新启动服务
	if err := tsm.Start(ctx); err != nil {
		logging.Errorf("Failed to restart service during reload: %v", err)
		return err
	}

	logging.Infof("Tailscale service reloaded successfully")
	return nil
}

// IsRunning 检查服务是否正在运行 (Service 接口)
func (tsm *TailscaleService) IsRunning() bool {
	tsm.stateMu.RLock()
	defer tsm.stateMu.RUnlock()
	return tsm.state == TailscaleServiceStateRunning
}

// Stop 停止服务 (Service 接口)
func (tsm *TailscaleService) Stop(ctx context.Context) error {
	tsm.mu.Lock()
	defer tsm.mu.Unlock()

	if !tsm.isRunning {
		return nil
	}

	// 清理 IP 规则
	if cleanupErr := tsm.cleanupIPRules(); cleanupErr != nil {
		logging.Warnf("Failed to cleanup IP rules: %v", cleanupErr)
		// 不将清理失败作为主要错误返回，但记录警告
	}

	// 停止 Tailscale 服务
	var err error
	if stopErr := tsm.preparer.GetTailscaleService().StopService(context.Background(), tsm.serviceName); stopErr != nil {
		logging.Errorf("Failed to stop tailscale service: %v", stopErr)
		err = stopErr
	}

	// 取消上下文
	if tsm.cancel != nil {
		tsm.cancel()
	}

	tsm.isRunning = false
	tsm.state = TailscaleServiceStateStopped

	// 更新健康状态
	healthMgr := GetGlobalHealthManager()
	healthMgr.UpdateServiceStatus(tsm.Name(), false, err)

	logging.Infof("Tailscale service stopped")
	return err
}

// =============================================================================
// Host 模式相关函数
// =============================================================================

// startHostModeWithRoutes 启动 host 模式（监听主机 tailscaled + 路由管理）
func (tsm *TailscaleService) startHostModeWithRoutes(node *coreV1.Node) error {
	logging.Infof("Starting Tailscale service in host mode with route management")

	// 检查主机 tailscaled 是否运行
	if err := tsm.checkTailscaledHealth(); err != nil {
		return fmt.Errorf("host tailscaled health check failed: %v", err)
	}

	// 启动健康检查协程（包含等待就绪和路由设置）
	go tsm.hostModeHealthCheck(node)

	logging.Infof("Host mode started successfully")
	return nil
}

// [HOST] checkHostTailscaledHealth 检查主机 tailscaled 健康状态
func (tsm *TailscaleService) checkTailscaledHealth() error {
	socketPath := tsm.tailscaleEnv.socketPath
	// 检查 socket 文件是否存在
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		return fmt.Errorf("host tailscaled socket not found: %s", socketPath)
	}

	// 获取状态之后 判断状态是否为Running
	status, err := tsm.preparer.GetTailscaleClient().GetStatus(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get tailscale status: %v", err)
	}
	if status.BackendState != "Running" {
		return fmt.Errorf("host tailscaled not running")
	}

	logging.Infof("Host tailscaled health check passed")
	return nil
}

// [HOST] hostModeHealthCheck host 模式健康检查协程（包含等待就绪和路由设置）
func (tsm *TailscaleService) hostModeHealthCheck(node *coreV1.Node) {
	// 尝试初始设置
	hostReady := tsm.tryInitialSetup(node)

	// 开始定期健康检查
	logging.Infof("Starting periodic health checks...")
	ticker := time.NewTicker(tsm.healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-tsm.ctx.Done():
			return
		case <-ticker.C:
			// 执行健康检查
			if err := tsm.performHealthCheck(); err != nil {
				tsm.updateHealthStatusWithLog(false, err, "Host mode health check failed: %v", err)
				// 如果未就绪，尝试重新设置
				if !hostReady {
					hostReady = tsm.tryInitialSetup(node)
				}
			} else {
				tsm.updateHealthStatusWithLog(true, nil, "Host mode health check passed")
				// 如果健康检查成功但之前未就绪，现在尝试设置
				if !hostReady {
					hostReady = tsm.tryInitialSetup(node)
				}
			}
		}
	}
}

// tryInitialSetup 尝试初始设置（等待就绪 + 设置路由）
func (tsm *TailscaleService) tryInitialSetup(node *coreV1.Node) bool {
	// 等待主机 tailscaled 准备就绪
	if err := tsm.waitForHostReady(); err != nil {
		logging.Errorf("Host tailscaled not ready: %v", err)
		tsm.updateHealthStatus(false, err)
		return false
	}

	logging.Infof("Host tailscaled is ready, setting up routes...")
	if err := tsm.setupAndManageRoutes(node); err != nil {
		logging.Warnf("Route management failed: %v", err)
		return false
	}

	logging.Infof("Initial setup completed successfully")
	return true
}

// [HOST] waitForHostReady 等待主机 tailscaled 准备就绪
func (tsm *TailscaleService) waitForHostReady() error {
	condition := func() (bool, error) {
		status, err := tsm.preparer.GetTailscaleClient().GetStatus(context.Background())
		if err != nil {
			return false, err
		}

		// 检查是否处于运行状态
		switch status.BackendState {
		case "Running":
			// 验证是否有有效的 IP 地址
			if status.Self != nil && len(status.Self.TailscaleIPs) > 0 {
				logging.Infof("Host tailscaled is ready with IP: %v", status.Self.TailscaleIPs)
				return true, nil
			}
		case "NeedsLogin":
			return false, fmt.Errorf("host tailscaled needs login")
		}

		return false, nil
	}
	return tsm.waitForCondition(condition, 60*time.Second, 3*time.Second, 20, "host tailscaled")
}

// performHealthCheck 通用健康检查函数（合并 host 和 daemon 模式）
func (tsm *TailscaleService) performHealthCheck() error {

	if err := tsm.checkTailscaledHealth(); err != nil {
		return fmt.Errorf("host health check failed: %v", err)
	}

	// 2. 检查本地 Pod CIDR 应用状态
	if err := tsm.checkLocalPodCIDRApplied(); err != nil {
		logging.Warnf("Local Pod CIDR check failed: %v", err)
		// 不返回错误，继续运行
	}

	// 3. 检查 Headscale 路由状态
	if err := tsm.checkHeadscaleRoutes(); err != nil {
		logging.Warnf("Headscale routes check failed: %v", err)
		// 不返回错误，继续运行
	}

	return nil
}

// =============================================================================
// Daemon 模式相关函数
// =============================================================================

// [DAEMON] startDaemonModeWithRoutes 启动 daemon 模式（完整管理逻辑 + 路由管理）
func (tsm *TailscaleService) startDaemonModeWithRoutes(node *coreV1.Node) error {
	logging.Infof("Starting Tailscale service in daemon mode with route management")

	// 1. 检查配置目录下的文件状态
	if err := tsm.checkDaemonConfigFiles(tsm.tailscaleEnv.configDir); err != nil {
		return fmt.Errorf("daemon config files check failed: %v", err)
	}

	// 2. 启动守护进程监控协程
	go tsm.daemonModeTailscaledKeepAlive()

	// 3. 启动健康检查协程（包含等待就绪和路由设置）
	go tsm.daemonModeHealthCheck(node)

	logging.Infof("Daemon mode started successfully")
	return nil
}

// [DAEMON] daemonModeTailscaledKeepAlive 守护进程保活监控
func (tsm *TailscaleService) daemonModeTailscaledKeepAlive() error {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	logging.Infof("Starting Tailscale daemon keep-alive monitor")

	for {
		select {
		case <-ticker.C:
			if err := tsm.monitorAndMaintainTailscaled(); err != nil {
				logging.Warnf("Tailscale daemon maintenance failed: %v", err)
			}
		case <-tsm.ctx.Done():
			logging.Infof("Tailscale daemon keep-alive monitor stopped")
			return nil
		}
	}
}

// [DAEMON] monitorAndMaintainTailscaled 监控并维护 Tailscale 守护进程
func (tsm *TailscaleService) monitorAndMaintainTailscaled() error {
	// 检查系统状态
	socketExists, stateExists, processExists, interfaceExists := tsm.checkSystemState()

	// 根据状态决定采取的行动
	return tsm.determineAndExecuteAction(socketExists, stateExists, processExists, interfaceExists)
}

// [DAEMON] checkSystemState 检查系统状态（合并多个检查函数）
func (tsm *TailscaleService) checkSystemState() (socketExists, stateExists, processExists, interfaceExists bool) {
	// 检查 socket 文件
	if _, err := os.Stat(tsm.tailscaleEnv.socketPath); err == nil {
		socketExists = true
		logging.Debugf("Socket file exists: %s", tsm.tailscaleEnv.socketPath)
	}

	// 检查state文件
	if _, err := os.Stat(tsm.tailscaleEnv.statePath); err == nil {
		stateExists = true
		logging.Debugf("State file exists: %s", tsm.tailscaleEnv.statePath)
	}

	// 检查进程文件
	if pidData, err := os.ReadFile(tsm.tailscaleEnv.pidPath); err == nil {
		if pidStr := strings.TrimSpace(string(pidData)); pidStr != "" {
			if pid, err := strconv.Atoi(pidStr); err == nil {
				if process, err := os.FindProcess(pid); err == nil {
					if err := process.Signal(os.Signal(nil)); err == nil {
						processExists = true
						logging.Debugf("Process file exists and process is running (PID: %d)", pid)
					}
				}
			}
		}
	}

	// 检查网络接口
	interfaceName := tsm.tailscaleEnv.tailscaleNic
	if interfaceName == "" {
		logging.Debugf("No interface name specified, skipping interface check")
		return
	}
	if _, err := os.Stat(fmt.Sprintf("/sys/class/net/%s", interfaceName)); err == nil {
		interfaceExists = true
		logging.Debugf("Tailscale interface exists: %s", interfaceName)
	}

	return
}

// [DAEMON] determineAndExecuteAction 根据状态决定并执行相应的行动
func (tsm *TailscaleService) determineAndExecuteAction(socketExists, stateExists, processExists, interfaceExists bool) error {
	// 情况1: 从未运行过 - socket, state, pid 文件都没有
	if !socketExists && !stateExists && !processExists && !interfaceExists {
		logging.Infof("Tailscale daemon never started, starting fresh")
		return tsm.startFreshTailscaled()
	}

	// 情况2: 进程死了但 socket 和 state 文件还在 - 复用现有数据启动
	if socketExists && stateExists && !processExists {
		logging.Infof("Tailscale daemon process died but socket and state files exist, reusing existing data to restart")
		return tsm.restartWithExistingData()
	}

	// 情况3: socket 文件丢失但 state 文件存在 - 复用状态数据重新启动
	if !socketExists && stateExists && !processExists {
		logging.Infof("Socket file missing but state file exists, reusing state data to restart")
		return tsm.restartWithExistingData()
	}

	// 情况4: 需要重新认证 - 进程存在但状态异常
	if socketExists && stateExists && processExists && interfaceExists {
		if err := tsm.checkTailscaledHealth(); err != nil {
			logging.Infof("Tailscale daemon needs re-authentication: %v", err)
			return tsm.handleReAuthentication()
		}
		logging.Debugf("Tailscale daemon is running normally")
		return nil
	}

	// 情况5: 文件不存在但接口存在 - 清理接口后重新启动
	if !socketExists && !stateExists && !processExists && interfaceExists {
		logging.Infof("Files missing but interface exists, cleaning up interface and starting fresh")
		return tsm.cleanupInterfaceAndStartFresh()
	}

	// 情况6: 其他异常情况，需要清理和重建
	logging.Infof("Abnormal Tailscale daemon state detected, cleaning up and restarting")
	return tsm.cleanupAndRestartTailscaled()
}

// [DAEMON] startFreshTailscaled 启动全新的 Tailscale 守护进程
func (tsm *TailscaleService) startFreshTailscaled() error {
	logging.Infof("Starting fresh Tailscale daemon")

	// 清理可能存在的残留文件
	tsm.cleanupTailscaleFiles()

	// 启动新的 tailscaled 进程
	_, err := tsm.preparer.GetTailscaleService().StartService(context.Background(), tsm.serviceName, tailscale.ServiceOptions{
		Hostname:   tsm.tailscaleEnv.hostName,
		Interface:  tsm.tailscaleEnv.tailscaleNic,
		AuthKey:    "", // 空字符串表示使用现有认证
		ControlURL: tsm.preparer.GetConfig().Tailscale.URL,
		SocketPath: tsm.tailscaleEnv.socketPath,
		StateFile:  tsm.tailscaleEnv.statePath,
		ConfigDir:  tsm.tailscaleEnv.configDir, // 添加配置目录字段
		Mode:       tailscale.ModeStandaloneTailscaled,
	})
	if err != nil {
		return fmt.Errorf("failed to start tailscale service: %v", err)
	}
	return nil
}

// [DAEMON] cleanupAndRestartTailscaled 清理并重启 Tailscale 守护进程
func (tsm *TailscaleService) cleanupAndRestartTailscaled() error {
	logging.Infof("Cleaning up and restarting Tailscale daemon")

	// 清理所有文件
	tsm.cleanupTailscaleFiles()

	// 重启服务
	if err := tsm.preparer.GetTailscaleService().StopService(context.Background(), tsm.serviceName); err != nil {
		logging.Warnf("Failed to stop existing service: %v", err)
	}

	// 启动新服务
	return tsm.startFreshTailscaled()
}

// [DAEMON] cleanupTailscaleFiles 清理 Tailscale 相关文件
func (tsm *TailscaleService) cleanupTailscaleFiles() {
	if tsm.preparer.GetConfig().Tailscale.Mode == "host" {
		return
	}

	// 安全检查：确保不会清理系统文件
	files := []string{
		tsm.tailscaleEnv.socketPath,
		tsm.tailscaleEnv.statePath,
		tsm.tailscaleEnv.pidPath,
	}

	for _, file := range files {
		if file != "" {
			// 额外安全检查：确保不会删除系统文件
			if strings.Contains(file, "/var/lib/tailscale") ||
				strings.Contains(file, "/usr") ||
				strings.Contains(file, "/opt") ||
				strings.Contains(file, "/var/run/tailscale") {
				logging.Warnf("跳过系统文件清理: %s", file)
				continue
			}

			if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
				logging.Warnf("Failed to remove file %s: %v", file, err)
			} else {
				logging.Debugf("Cleaned up file: %s", file)
			}
		}
	}
}

// [DAEMON] handleReAuthentication 处理重新认证
func (tsm *TailscaleService) handleReAuthentication() error {
	logging.Infof("Handling Tailscale re-authentication")

	// 尝试自动登录
	if err := tsm.attemptLogin(); err != nil {
		return fmt.Errorf("re-authentication failed: %v", err)
	}

	return nil
}

// [DAEMON] restartWithExistingData 复用现有数据重启 Tailscale 守护进程
func (tsm *TailscaleService) restartWithExistingData() error {
	logging.Infof("Restarting Tailscale daemon with existing data")

	// 直接启动服务，复用现有的 socket、state、pid 文件
	_, err := tsm.preparer.GetTailscaleService().StartService(context.Background(), tsm.serviceName, tailscale.ServiceOptions{
		Hostname:   tsm.tailscaleEnv.hostName,
		Interface:  tsm.tailscaleEnv.tailscaleNic,
		AuthKey:    "", // 空字符串表示使用现有认证
		ControlURL: tsm.preparer.GetConfig().Tailscale.URL,
		SocketPath: tsm.tailscaleEnv.socketPath,
		StateFile:  tsm.tailscaleEnv.statePath,
		ConfigDir:  tsm.tailscaleEnv.configDir, // 添加配置目录字段
		Mode:       tailscale.ModeStandaloneTailscaled,
	})
	if err != nil {
		return fmt.Errorf("failed to restart with existing data: %v", err)
	}

	logging.Infof("Successfully restarted Tailscale daemon with existing data")
	return nil
}

// [DAEMON] cleanupInterfaceAndStartFresh 清理接口后重新启动
func (tsm *TailscaleService) cleanupInterfaceAndStartFresh() error {
	logging.Infof("Cleaning up Tailscale interface and starting fresh")

	// 清理 Tailscale 网络接口
	if err := tsm.cleanupTailscaleInterface(); err != nil {
		logging.Warnf("Failed to cleanup interface: %v", err)
	}

	// 启动全新的服务
	return tsm.startFreshTailscaled()
}

// [DAEMON] cleanupTailscaleInterface 清理 Tailscale 网络接口
func (tsm *TailscaleService) cleanupTailscaleInterface() error {
	if tsm.preparer.GetConfig().Tailscale.Mode == "host" {
		return nil
	}

	interfaceName := tsm.tailscaleEnv.tailscaleNic
	if interfaceName == "" {
		logging.Warnf("No interface name specified, skipping interface cleanup")
		return nil
	}

	// 安全检查：避免误删系统接口
	protectedInterfaces := []string{
		"tailscale0", // 主机 Tailscale 接口
		"eth0",       // 主要网络接口
		"ens",        // 现代 Linux 网络接口前缀
		"eno",        // 板载网络接口
		"enp",        // PCI 网络接口
		"lo",         // 回环接口
		"docker0",    // Docker 网桥
		"br-",        // Docker 网桥前缀
		"veth",       // 虚拟以太网接口
		"cali",       // Calico 接口
		"flannel",    // Flannel 接口
		"cni0",       // CNI 接口
		"weave",      // Weave 接口
	}

	// 检查是否为受保护的系统接口
	for _, protected := range protectedInterfaces {
		if strings.HasPrefix(interfaceName, protected) || interfaceName == protected {
			logging.Warnf("Skipping cleanup of protected system interface: %s", interfaceName)
			return nil
		}
	}

	// 在 Pod 环境中，ip 命令可能不可用，改用 netlink
	// 使用 netlink 删除网络接口
	links, err := netlink.LinkList()
	if err != nil {
		logging.Warnf("Failed to list network links: %v", err)
		return nil
	}

	// 查找并删除指定的接口
	for _, link := range links {
		if link.Attrs().Name == interfaceName {
			if err := netlink.LinkDel(link); err != nil {
				// 如果删除失败，记录警告但不返回错误
				logging.Warnf("Failed to delete interface %s: %v", interfaceName, err)
			} else {
				logging.Infof("Successfully cleaned up Tailscale interface: %s", interfaceName)
			}
			return nil
		}
	}

	logging.Debugf("Interface %s not found, nothing to clean up", interfaceName)
	return nil
}

// [DAEMON] checkDaemonConfigFiles 检查 daemon 模式配置文件
func (tsm *TailscaleService) checkDaemonConfigFiles(configDir string) error {
	logging.Infof("Checking daemon config files in: %s", configDir)

	// 验证配置目录路径
	if configDir == "" {
		return fmt.Errorf("config directory path is empty")
	}

	// 检查配置目录是否存在
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		logging.Infof("Config directory does not exist, creating: %s", configDir)
		if err := os.MkdirAll(configDir, 0755); err != nil {
			return fmt.Errorf("failed to create config directory '%s': %v", configDir, err)
		}
		logging.Infof("Successfully created config directory: %s", configDir)
	} else if err != nil {
		return fmt.Errorf("failed to check config directory '%s': %v", configDir, err)
	}

	return nil
}

// checkConfigChanged 检查配置是否发生变化（通用函数）
func (tsm *TailscaleService) checkConfigChanged() bool {
	newConfig := tsm.preparer.GetConfig()
	oldConfig := tsm.preparer.GetOldConfig()

	if oldConfig == nil || newConfig == nil {
		return false
	}

	// 检查关键配置是否变更
	return newConfig.Tailscale.Mode != oldConfig.Tailscale.Mode ||
		newConfig.Tailscale.Socket.Path != oldConfig.Tailscale.Socket.Path ||
		newConfig.Headscale.URL != oldConfig.Headscale.URL
}

// handleErrorWithLog 通用错误处理函数，消除重复的错误处理模式
func (tsm *TailscaleService) handleErrorWithLog(err error, format string, args ...interface{}) error {
	if err != nil {
		logging.Errorf(format, args...)
		tsm.lastError = err
		tsm.state = TailscaleServiceStateError
	}
	return err
}

// [DAEMON] daemonModeHealthCheck daemon 模式健康检查协程（包含等待就绪和路由设置）
func (tsm *TailscaleService) daemonModeHealthCheck(node *coreV1.Node) {
	// 尝试初始设置
	daemonReady := tsm.tryDaemonInitialSetup(node)

	// 开始定期健康检查
	logging.Infof("Starting periodic health checks...")
	ticker := time.NewTicker(tsm.healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-tsm.ctx.Done():
			return
		case <-ticker.C:
			// 执行健康检查
			if err := tsm.performHealthCheck(); err != nil {
				tsm.updateHealthStatusWithLog(false, err, "Daemon mode health check failed: %v", err)
				// 如果未就绪，尝试重新设置
				if !daemonReady {
					daemonReady = tsm.tryDaemonInitialSetup(node)
				}
			} else {
				tsm.updateHealthStatusWithLog(true, nil, "Daemon mode health check passed")
				// 如果健康检查成功但之前未就绪，现在尝试设置
				if !daemonReady {
					daemonReady = tsm.tryDaemonInitialSetup(node)
				}
			}
		}
	}
}

// tryDaemonInitialSetup 尝试 daemon 模式初始设置（等待就绪 + 设置路由）
func (tsm *TailscaleService) tryDaemonInitialSetup(node *coreV1.Node) bool {
	// 等待守护进程就绪
	if err := tsm.waitForDaemonReady(); err != nil {
		logging.Errorf("Daemon not ready: %v", err)
		tsm.updateHealthStatus(false, err)
		return false
	}

	logging.Infof("Daemon is ready, setting up routes...")
	if err := tsm.setupAndManageRoutes(node); err != nil {
		logging.Warnf("Route management failed: %v", err)
		return false
	}

	logging.Infof("Daemon initial setup completed successfully")
	return true
}

// [DAEMON] waitForDaemonReady 等待 daemon 模式 tailscaled 准备就绪
func (tsm *TailscaleService) waitForDaemonReady() error {
	condition := func() (bool, error) {
		status, err := tsm.preparer.GetTailscaleClient().GetStatus(context.Background())
		if err != nil {
			return false, err
		}

		// 检查连接状态
		switch status.BackendState {
		case "Running":
			// 验证是否有有效的 IP 地址
			if status.Self != nil && len(status.Self.TailscaleIPs) > 0 {
				logging.Infof("Daemon tailscaled is ready with IP: %v", status.Self.TailscaleIPs)
				return true, nil
			}
		case "NeedsLogin":
			logging.Infof("Daemon tailscaled needs login, attempting auto-login with auth key")
			// 尝试使用认证密钥登录
			if err := tsm.attemptLogin(); err != nil {
				logging.Warnf("Auto-login failed: %v", err)
			}
		}

		return false, nil
	}

	return tsm.waitForCondition(condition, 3*time.Minute, 5*time.Second, 36, "daemon tailscaled")
}

// =============================================================================
// Public 模式相关函数
// =============================================================================

// [PUBLIC] setupAndManageRoutes 设置和管理路由（两种模式共用）
func (tsm *TailscaleService) setupAndManageRoutes(node *coreV1.Node) error {
	logging.Infof("Setting up and managing routes for node: %s", node.Name)

	podLocalCIDR, err := tsm.preparer.GetK8sClient().Nodes().GetPodCIDR(node.Name)
	if err != nil {
		return fmt.Errorf("failed to get Pod CIDR for node %s: %w", node.Name, err)
	}
	if podLocalCIDR == "" {
		return fmt.Errorf("no Pod CIDR found for node %s", node.Name)
	}

	logging.Infof("Node %s Pod CIDR: %s", node.Name, podLocalCIDR)

	// 1. 获取 Tailscale IP 和节点密钥
	tailscaleIP, nodeKey, err := tsm.getTailscaleInfo()
	if err != nil {
		return fmt.Errorf("failed to get tailscale info: %v", err)
	}

	logging.Infof("Tailscale IP: %s, Node Key: %s...", tailscaleIP.String(), nodeKey[:min(10, len(nodeKey))])

	// 2. 设置客户端路由偏好
	if err := tsm.setupClientRoutePreferences(); err != nil {
		logging.Warnf("Failed to setup client route preferences: %v", err)
		// 不返回错误，继续执行
	}

	// 3. 配置路由通告（通过 manageHeadscaleRoutes 处理）
	if err := tsm.manageHeadscaleRoutes(podLocalCIDR, tailscaleIP.String()); err != nil {
		logging.Warnf("Failed to configure route advertisement: %v", err)
		// 不返回错误，继续执行
	}

	// 4. 等待路由同步到 Headscale
	if err := tsm.waitForRouteSync(podLocalCIDR); err != nil {
		logging.Warnf("Route sync failed: %v", err)
		// 不返回错误，继续执行
	}

	// 5. 管理 Headscale 路由（批准路由）
	if err := tsm.manageHeadscaleRoutes(podLocalCIDR, tailscaleIP.String()); err != nil {
		logging.Warnf("Failed to manage headscale routes: %v", err)
		// 不返回错误，继续执行
	}

	// 6. 上传 Tailscale 信息到节点注解
	if err := tsm.uploadTailscaleInfo(tailscaleIP, nodeKey); err != nil {
		logging.Warnf("Failed to upload tailscale info: %v", err)
		// 不返回错误，继续执行
	}

	// 启动规则监控和维护
	go tsm.monitorAndMaintainRules()

	logging.Infof("Route setup completed")
	return nil
}

func isSameNetwork(ip1, ip2 net.IP) bool {
	// 如果都是IPv4，检查前2个字节是否相同 (相当于/16网段)
	if ip1.To4() != nil && ip2.To4() != nil {
		ip1v4 := ip1.To4()
		ip2v4 := ip2.To4()
		return ip1v4[0] == ip2v4[0] && ip1v4[1] == ip2v4[1]
	}
	return false
}

func (tsm *TailscaleService) addIPRuleInHost() error {
	//ip rule add from <tailscale_ip> lookup 53 priority 153
	//ip rule add to <pod_local_cidr> table main priority 152
	tailscaleIP, err := tsm.preparer.GetTailscaleClient().GetIP(context.Background())
	if err != nil {
		logging.Warnf("Failed to get tailscale ip: %v", err)
		return err
	}

	nodeName, err := tsm.preparer.GetK8sClient().GetCurrentNodeName()
	if err != nil {
		tsm.updateHealthStatus(false, err)
		return tsm.handleErrorWithLog(err, "Failed to get current node name: %w", err)
	}
	podLocalCIDR, err := tsm.preparer.GetK8sClient().Nodes().GetPodCIDR(nodeName)
	if err != nil {
		logging.Warnf("Failed to get pod local cidr: %v", err)
		return err
	}
	// 将 podLocalCIDR 转换为 *net.IPNet
	_, podLocalCIDRNet, err := net.ParseCIDR(podLocalCIDR)
	if err != nil {
		logging.Warnf("Failed to parse pod local cidr: %v", err)
		return err
	}

	// 检查当前规则列表
	rules, err := netlink.RuleList(netlink.FAMILY_V4)
	if err != nil {
		logging.Warnf("Failed to get rules: %v", err)
		return err
	}

	// 检查机器上是否有tailscale0的ip
	localIP, err := tsm.preparer.GetTailscaleClient().GetLocalIP(context.Background())
	if err == nil {
		if localIP.String() != tailscaleIP.String() {
			if err := tsm.manageRule(rules, localIP, nil, 52, 3152, "from"); err != nil {
				logging.Warnf("Failed to add local IP rule: %v", err)
			}
		}
	}

	// 添加两个规则（并行执行，互不影响）
	if err := tsm.manageRule(rules, tailscaleIP, nil, 53, 3153, "from"); err != nil {
		logging.Warnf("Failed to add tailscale IP rule: %v", err)
	}

	if err := tsm.manageRule(rules, netip.Addr{}, podLocalCIDRNet, 254, 3151, "to"); err != nil {
		logging.Warnf("Failed to add pod CIDR rule: %v", err)
	}

	return nil
}

// 通用的规则管理函数
func (tsm *TailscaleService) manageRule(existingRules []netlink.Rule, srcIP netip.Addr, dstNet *net.IPNet, table, priority int, ruleType string) error {
	// 检查是否已存在完全匹配的规则
	for _, rule := range existingRules {
		if tsm.isRuleMatch(rule, srcIP, dstNet, table, priority, ruleType) {
			logging.Infof("%s rule already exists: %s %s lookup %d priority %d",
				strings.Title(ruleType), ruleType, tsm.getRuleDescription(srcIP, dstNet), table, priority)
			return nil
		}
	}

	// 删除同网段的旧规则
	if err := tsm.deleteOldRules(existingRules, srcIP, dstNet, table, priority, ruleType); err != nil {
		return err
	}

	// 添加新规则
	return tsm.addNewRule(srcIP, dstNet, table, priority, ruleType)
}

// 检查规则是否匹配
func (tsm *TailscaleService) isRuleMatch(rule netlink.Rule, srcIP netip.Addr, dstNet *net.IPNet, table, priority int, ruleType string) bool {
	if rule.Priority != priority || rule.Table != table {
		return false
	}

	if ruleType == "from" {
		return rule.Src != nil &&
			srcIP.IsValid() &&
			rule.Src.IP.Equal(srcIP.AsSlice()) &&
			rule.Src.Mask.String() == net.CIDRMask(32, 32).String() &&
			rule.Dst == nil
	} else { // to rule
		return rule.Dst != nil &&
			dstNet != nil &&
			rule.Dst.IP.Equal(dstNet.IP) &&
			rule.Dst.Mask.String() == dstNet.Mask.String() &&
			rule.Src == nil
	}
}

// 删除旧规则
func (tsm *TailscaleService) deleteOldRules(existingRules []netlink.Rule, srcIP netip.Addr, dstNet *net.IPNet, table, priority int, ruleType string) error {
	var rulesToDelete []*netlink.Rule

	for _, rule := range existingRules {
		if rule.Priority == priority && rule.Table == table {
			if ruleType == "from" && rule.Src != nil && rule.Dst == nil {
				if isSameNetwork(rule.Src.IP, srcIP.AsSlice()) && !rule.Src.IP.Equal(srcIP.AsSlice()) {
					ruleCopy := rule
					rulesToDelete = append(rulesToDelete, &ruleCopy)
					logging.Infof("Found old from rule in same network to delete: from %s lookup %d priority %d",
						rule.Src.IP.String(), table, priority)
				}
			} else if ruleType == "to" && rule.Dst != nil && rule.Src == nil {
				if rule.Dst.String() == dstNet.String() {
					ruleCopy := rule
					rulesToDelete = append(rulesToDelete, &ruleCopy)
					logging.Infof("Found old to rule to delete: to %s table main priority %d",
						rule.Dst.String(), priority)
				}
			}
		}
	}

	// 删除旧规则
	for _, rule := range rulesToDelete {
		if err := netlink.RuleDel(rule); err != nil {
			logging.Warnf("Failed to delete old %s rule %v: %v", ruleType, rule, err)
		} else {
			logging.Infof("Deleted old %s rule: %s %s lookup %d priority %d",
				ruleType, ruleType, tsm.getRuleDescription(srcIP, dstNet), table, priority)
		}
	}

	return nil
}

// 添加新规则
func (tsm *TailscaleService) addNewRule(srcIP netip.Addr, dstNet *net.IPNet, table, priority int, ruleType string) error {
	newRule := netlink.NewRule()
	newRule.Table = table
	newRule.Priority = priority

	if ruleType == "from" {
		ip := net.IP(srcIP.AsSlice()).To4()
		if ip == nil {
			return fmt.Errorf("invalid IPv4 address: %s", srcIP)
		}
		newRule.Src = &net.IPNet{
			IP:   ip,
			Mask: net.CIDRMask(32, 32),
		}
	} else { // to rule
		newRule.Dst = &net.IPNet{
			IP:   dstNet.IP,
			Mask: dstNet.Mask,
		}
	}

	var tableName string
	if table == 254 {
		tableName = "main"
	} else {
		tableName = fmt.Sprintf("%d", table)
	}
	logging.Infof("Attempting to add %s rule: %s %s lookup %s priority %d",
		ruleType, ruleType, tsm.getRuleDescription(srcIP, dstNet), tableName, priority)

	if err := netlink.RuleAdd(newRule); err != nil {
		logging.Warnf("Failed to add %s rule: %v", ruleType, err)
		return err
	}

	logging.Infof("Successfully added %s rule: %s %s lookup %s priority %d",
		ruleType, ruleType, tsm.getRuleDescription(srcIP, dstNet), tableName, priority)
	return nil
}

// cleanupIPRules 清理之前添加的 IP 规则
func (tsm *TailscaleService) cleanupIPRules() error {
	logging.Infof("Cleaning up IP rules...")

	// 获取当前规则列表
	rules, err := netlink.RuleList(netlink.FAMILY_V4)
	if err != nil {
		logging.Warnf("Failed to get rules for cleanup: %v", err)
		return err
	}

	// 清理我们添加的规则（优先级 3151, 3152, 3153）
	prioritiesToClean := []int{3151, 3152, 3153}

	for _, priority := range prioritiesToClean {
		for _, rule := range rules {
			if rule.Priority == priority {
				if err := netlink.RuleDel(&rule); err != nil {
					logging.Warnf("Failed to delete rule with priority %d: %v", priority, err)
				} else {
					logging.Infof("Successfully deleted rule with priority %d", priority)
				}
			}
		}
	}

	logging.Infof("IP rules cleanup completed")
	return nil
}

// 获取规则描述
func (tsm *TailscaleService) getRuleDescription(srcIP netip.Addr, dstNet *net.IPNet) string {
	if srcIP.IsValid() {
		return srcIP.String()
	}
	if dstNet != nil {
		return dstNet.String()
	}
	return ""
}

// monitorAndMaintainRules 持续监控和维护 IP 规则
func (tsm *TailscaleService) monitorAndMaintainRules() {
	if err := tsm.addIPRuleInHost(); err != nil {
		logging.Warnf("Failed first time to add ip rule in host: %v", err)
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := tsm.addIPRuleInHost(); err != nil {
				logging.Warnf("Failed to add ip rule in host: %v", err)
			}
		case <-tsm.ctx.Done():
			return
		}
	}
}

// getTailscaleInfo 获取 Tailscale IP 和节点密钥
// [PUBLIC] getTailscaleInfo 获取 Tailscale 信息
func (tsm *TailscaleService) getTailscaleInfo() (net.IP, string, error) {
	// 获取 Tailscale 状态
	ipnState, err := tsm.preparer.GetTailscaleClient().GetStatus(context.Background())
	if err != nil {
		return nil, "", fmt.Errorf("failed to get tailscale status: %v", err)
	}

	// 获取 Tailscale IP
	tailscaleIP, err := tsm.preparer.GetTailscaleClient().GetIP(context.Background())
	if err != nil {
		return nil, "", fmt.Errorf("failed to get tailscale ip: %v", err)
	}
	ip := net.ParseIP(tailscaleIP.String())

	// 获取 Node Key
	var nodeKey string
	if ipnState.Self != nil {
		nodeKey = ipnState.Self.PublicKey.String()
	}

	return ip, nodeKey, nil
}

// [PUBLIC] attemptLogin 尝试自动登录
func (tsm *TailscaleService) attemptLogin() error {
	logging.Infof("Starting automatic login process for node: %s", tsm.hostname)

	// 策略1: 优先尝试使用现有的认证信息（"auto"模式）
	logging.Infof("策略1: 尝试使用现有认证信息（auto模式）")
	if err := tsm.tryLoginWithExistingCredentials(); err == nil {
		logging.Infof("✅ 使用现有认证信息登录成功")
		return nil
	}
	logging.Infof("现有认证信息登录失败，将尝试其他策略")

	// 策略2: 检查是否有有效的 authKey（从 Headscale 获取的）
	tsm.cleanupExpiredAuthKey() // 清理过期的密钥

	if tsm.validateAuthKey() {
		logging.Infof("策略2: 尝试使用存储的认证密钥")
		if err := tsm.tryLoginWithAuthKey(); err == nil {
			logging.Infof("✅ 使用认证密钥登录成功")
			return nil
		}
		logging.Warnf("认证密钥登录失败")
	} else {
		logging.Infof("没有有效的存储认证密钥")
	}

	// 策略3: 从 Headscale 获取新的认证密钥
	logging.Infof("策略3: 从 Headscale 获取新的认证密钥")
	return tsm.refreshAuthKeyFromHeadscale()
}

// tryLoginWithExistingCredentials 尝试使用现有认证信息登录
func (tsm *TailscaleService) tryLoginWithExistingCredentials() error {
	logging.Infof("尝试使用现有认证信息登录")

	// 首先检查当前状态
	status, err := tsm.preparer.GetTailscaleClient().GetStatus(context.Background())
	if err != nil {
		return fmt.Errorf("无法获取当前状态: %v", err)
	}

	logging.Infof("当前状态: %s, 是否有NodeKey: %v", status.BackendState, status.HaveNodeKey)

	// 如果已经有有效的连接，直接返回成功
	if status.BackendState == "Running" && status.Self != nil && len(status.Self.TailscaleIPs) > 0 {
		logging.Infof("✅ 已经处于运行状态且有有效IP: %v", status.Self.TailscaleIPs)
		return nil
	}

	// 如果有NodeKey但未运行，尝试启用运行状态
	if status.HaveNodeKey {
		logging.Infof("检测到现有NodeKey，尝试启用运行状态")

		// 使用 "auto" 模式尝试连接
		err := tsm.preparer.GetTailscaleClient().UpWithOptions(context.Background(), tailscale.ClientOptions{
			AcceptDNS:    tsm.preparer.GetConfig().Tailscale.AcceptDNS,
			AuthKey:      "auto", // 使用已保存的认证信息
			Hostname:     tsm.tailscaleEnv.hostName,
			ControlURL:   tsm.preparer.GetConfig().Tailscale.URL,
			AcceptRoutes: true,
			ShieldsUp:    false,
		})

		if err == nil {
			logging.Infof("✅ 使用auto模式成功启用现有认证")
			return nil
		}

		logging.Debugf("auto模式启用失败: %v", err)
		return err
	}

	// 如果没有现有认证信息，返回错误
	return fmt.Errorf("没有可用的现有认证信息")
}

// tryLoginWithAuthKey 尝试使用认证密钥登录
func (tsm *TailscaleService) tryLoginWithAuthKey() error {
	logging.Infof("尝试使用认证密钥登录")

	// 验证认证密钥是否仍然有效
	if !tsm.validateAuthKey() {
		return fmt.Errorf("认证密钥已过期或无效")
	}

	err := tsm.preparer.GetTailscaleClient().UpWithOptions(context.Background(), tailscale.ClientOptions{
		AcceptDNS:    tsm.preparer.GetConfig().Tailscale.AcceptDNS,
		AuthKey:      tsm.authKey,
		Hostname:     tsm.tailscaleEnv.hostName,
		ControlURL:   tsm.preparer.GetConfig().Tailscale.URL,
		AcceptRoutes: true,
		ShieldsUp:    false,
	})

	if err == nil {
		logging.Infof("✅ 使用认证密钥登录成功")
		return nil
	}

	logging.Debugf("认证密钥登录失败: %v", err)
	return err
}

// refreshAuthKeyFromHeadscale 从 Headscale 获取新的认证密钥
func (tsm *TailscaleService) refreshAuthKeyFromHeadscale() error {
	logging.Infof("从 Headscale 刷新认证密钥")

	// 获取当前节点信息以确定用户
	node, err := tsm.preparer.GetK8sClient().GetCurrentNode()
	if err != nil {
		return fmt.Errorf("无法获取当前节点: %v", err)
	}

	// 从节点标签或注解中获取用户信息，如果没有则使用默认用户
	user := tsm.preparer.GetConfig().Tailscale.User
	if user == "" {
		user = "default" // 默认用户
	}

	// Headscale 要求 tag 必须以 "tag:" 开头
	aclTags := make([]string, 0)
	for _, tag := range tsm.preparer.GetConfig().Tailscale.Tags {
		if !strings.HasPrefix(tag, "tag:") {
			tag = "tag:" + tag
		}
		aclTags = append(aclTags, tag)
	}

	// 添加节点标签，确保格式正确
	if node.Name != "" {
		nodeTag := fmt.Sprintf("tag:node:%s", node.Name)
		aclTags = append(aclTags, nodeTag)
	}

	logging.Infof("为用户创建预授权密钥: %s, 标签: %v", user, aclTags)

	// 创建预授权密钥请求
	preAuthKeyReq := &headscale.CreatePreAuthKeyRequest{
		User:       user,
		Reusable:   false, // 一次性使用
		Ephemeral:  false, // 非临时节点
		AclTags:    aclTags,
		Expiration: time.Now().Add(24 * time.Hour), // 24小时有效期
	}

	// 从 Headscale 创建新的预授权密钥，带重试机制
	var preAuthResp *headscale.CreatePreAuthKeyResponse
	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		var err error
		preAuthResp, err = tsm.preparer.GetHeadscaleClient().CreatePreAuthKey(context.Background(), preAuthKeyReq)
		if err == nil {
			break
		}

		if attempt == maxRetries {
			return fmt.Errorf("从 Headscale 创建预授权密钥失败，尝试 %d 次后失败: %v", maxRetries, err)
		}

		logging.Warnf("尝试 %d 失败: %v, 5秒后重试...", attempt, err)
		time.Sleep(5 * time.Second)
	}

	if preAuthResp.PreAuthKey.Key == "" {
		return fmt.Errorf("从 Headscale 接收到空的预授权密钥")
	}

	// 更新本地的 authKey
	tsm.authKey = preAuthResp.PreAuthKey.Key
	tsm.authKeyExpiredTime = preAuthResp.PreAuthKey.Expiration
	logging.Infof("✅ 成功从 Headscale 刷新认证密钥，过期时间: %v", tsm.authKeyExpiredTime)

	// 使用新的认证密钥尝试登录
	return tsm.preparer.GetTailscaleClient().UpWithOptions(context.Background(), tailscale.ClientOptions{
		AuthKey:      tsm.authKey,
		Hostname:     tsm.tailscaleEnv.hostName,
		ControlURL:   tsm.preparer.GetConfig().Tailscale.URL,
		AcceptDNS:    tsm.preparer.GetConfig().Tailscale.AcceptDNS,
		AcceptRoutes: true,
		ShieldsUp:    false,
	})
}

// [PUBLIC] checkLocalPodCIDRApplied 检查本地 Pod CIDR 是否已应用
func (tsm *TailscaleService) checkLocalPodCIDRApplied() error {
	node, err := tsm.preparer.GetK8sClient().GetCurrentNode()
	if err != nil {
		return fmt.Errorf("failed to get current node: %v", err)
	}

	podLocalCIDR, err := tsm.preparer.GetK8sClient().Nodes().GetPodCIDR(node.Name)
	if err != nil {
		return fmt.Errorf("failed to get Pod CIDR for node %s: %w", node.Name, err)
	}
	if podLocalCIDR == "" {
		return fmt.Errorf("no Pod CIDR found for node %s", node.Name)
	}

	// 检查并应用路由
	if err := tsm.ensureTailscaleRoute(podLocalCIDR); err != nil {
		return fmt.Errorf("failed to ensure Tailscale route: %v", err)
	}

	return nil
}

// ensureTailscaleRoute 确保 Tailscale 路由存在（检查并应用）
// [PUBLIC] ensureTailscaleRoute 确保 Tailscale 路由存在
func (tsm *TailscaleService) ensureTailscaleRoute(podLocalCIDR string) error {
	// 获取 Tailscale 客户端
	tailscaleClient := tsm.preparer.GetTailscaleClient()
	if tailscaleClient == nil {
		return fmt.Errorf("tailscale client not available")
	}

	// 获取 Tailscale 偏好设置
	prefs, err := tailscaleClient.GetPrefs(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get Tailscale preferences: %v", err)
	}

	// 检查是否已在通告路由中
	for _, advertiseRoute := range prefs.AdvertiseRoutes {
		if advertiseRoute.String() == podLocalCIDR {
			logging.Debugf("Pod CIDR %s already exists in Tailscale advertised routes", podLocalCIDR)
			return nil
		}
	}

	// 如果不存在，则应用路由
	logging.Infof("Pod CIDR %s not found in Tailscale routes, applying...", podLocalCIDR)
	return tsm.applyTailscaleRoute(podLocalCIDR)
}

// applyTailscaleRoute 在 Tailscale 中应用指定 CIDR 的路由（合并现有路由）
// [PUBLIC] applyTailscaleRoute 应用 Tailscale 路由
func (tsm *TailscaleService) applyTailscaleRoute(podLocalCIDR string) error {
	logging.Infof("Applying Tailscale route for CIDR: %s", podLocalCIDR)

	// 获取 Tailscale 客户端
	tailscaleClient := tsm.preparer.GetTailscaleClient()
	if tailscaleClient == nil {
		return fmt.Errorf("Tailscale client not available")
	}

	// 解析新的 CIDR
	newPrefix, err := netip.ParsePrefix(podLocalCIDR)
	if err != nil {
		return fmt.Errorf("invalid CIDR format %s: %v", podLocalCIDR, err)
	}

	// 获取当前已通告的路由
	prefs, err := tailscaleClient.GetPrefs(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get current preferences: %v", err)
	}

	// 合并现有路由和新路由
	mergedRoutes := make([]netip.Prefix, len(prefs.AdvertiseRoutes)+1)
	copy(mergedRoutes, prefs.AdvertiseRoutes)
	mergedRoutes[len(prefs.AdvertiseRoutes)] = newPrefix

	logging.Infof("Merging routes: existing %d routes + new route %s = total %d routes",
		len(prefs.AdvertiseRoutes), podLocalCIDR, len(mergedRoutes))

	// 应用合并后的路由
	if err := tailscaleClient.AdvertiseRoutes(context.Background(), mergedRoutes...); err != nil {
		return fmt.Errorf("failed to advertise merged routes: %v", err)
	}

	logging.Infof("Successfully applied Tailscale route for CIDR: %s (merged with existing routes)", podLocalCIDR)
	return nil
}

// [PUBLIC] checkHeadscaleRoutes 检查 Headscale 路由状态
func (tsm *TailscaleService) checkHeadscaleRoutes() error {
	// 获取当前节点信息
	node, err := tsm.preparer.GetK8sClient().GetCurrentNode()
	if err != nil {
		return fmt.Errorf("failed to get current node: %w", err)
	}

	podLocalCIDR, err := tsm.preparer.GetK8sClient().Nodes().GetPodCIDR(node.Name)
	if err != nil {
		return fmt.Errorf("failed to get Pod CIDR for node %s: %w", node.Name, err)
	}

	// 检查 Headscale 路由
	routes, err := tsm.preparer.GetHeadscaleClient().GetRoutes(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get headscale routes: %v", err)
	}

	// 查找本地 Pod CIDR 路由
	for _, route := range routes.Routes {
		if route.Prefix == podLocalCIDR {
			if !route.Enabled {
				// 启用路由
				if err := tsm.preparer.GetHeadscaleClient().EnableRoute(context.Background(), route.ID); err != nil {
					return fmt.Errorf("failed to enable route %s: %v", route.Prefix, err)
				}
				logging.Infof("Enabled route for local Pod CIDR: %s", podLocalCIDR)
			}
			return nil
		}
	}

	return fmt.Errorf("route for local Pod CIDR not found: %s", podLocalCIDR)
}

// [PUBLIC] manageHeadscaleRoutes 管理 Headscale 路由（合并配置和管理的功能）
func (tsm *TailscaleService) manageHeadscaleRoutes(podLocalCIDR, tailscaleIP string) error {
	logging.Infof("Managing Headscale routes for CIDR: %s, IP: %s", podLocalCIDR, tailscaleIP)

	// 获取所有路由
	routes, err := tsm.preparer.GetHeadscaleClient().GetRoutes(context.Background())
	if err != nil {
		return fmt.Errorf("failed to get routes: %v", err)
	}

	// 处理每个路由
	for _, route := range routes.Routes {
		if route.Prefix == podLocalCIDR {
			if route.Enabled {
				logging.Infof("Route %s is already enabled for our node", route.Prefix)
			} else {
				logging.Infof("Enabling route %s for our node", route.Prefix)
				if err := tsm.preparer.GetHeadscaleClient().EnableRoute(context.Background(), route.ID); err != nil {
					logging.Warnf("Failed to enable route %s: %v", route.ID, err)
				}
			}
		} else {
			logging.Infof("Enabling route %s for our node", route.Prefix)
			if err := tsm.preparer.GetHeadscaleClient().EnableRoute(context.Background(), route.ID); err != nil {
				logging.Warnf("Failed to enable route %s: %v", route.ID, err)
			}
		}
	}

	return nil
}

// uploadTailscaleInfo 上传 Tailscale 信息到节点注解
// [PUBLIC] uploadTailscaleInfo 上传 Tailscale 信息到 Headscale
func (tsm *TailscaleService) uploadTailscaleInfo(tailscaleIP net.IP, nodeKey string) error {
	node, err := tsm.preparer.GetK8sClient().GetCurrentNode()
	if err != nil {
		return fmt.Errorf("failed to get current node: %v", err)
	}
	podLocalCIDR, err := tsm.preparer.GetK8sClient().Nodes().GetPodCIDR(node.Name)
	if err != nil {
		return fmt.Errorf("failed to get Pod CIDR for node %s: %w", node.Name, err)
	}

	// 使用 k8s 包的注解功能
	annotations := map[string]string{
		constants.HeadcniTailscaleIPAnnotationKey: tailscaleIP.String(),
		constants.HeadcniNodeKeyAnnotationKey:     nodeKey,
		constants.HeadcniPodCIDRAnnotationKey:     podLocalCIDR,
	}

	return tsm.preparer.GetK8sClient().Nodes().UpdateAnnotations(node.Name, annotations)
}

// [PUBLIC] GetState 获取服务状态
func (tsm *TailscaleService) GetState() TailscaleServiceState {
	tsm.stateMu.RLock()
	defer tsm.stateMu.RUnlock()
	return tsm.state
}

// [PUBLIC] GetServiceInfo 获取服务信息
func (tsm *TailscaleService) GetServiceInfo() map[string]interface{} {
	tsm.stateMu.RLock()
	defer tsm.stateMu.RUnlock()

	return map[string]interface{}{
		"state":       string(tsm.state),
		"hostname":    tsm.hostname,
		"serviceName": tsm.serviceName,
		"startTime":   tsm.startTime,
		"lastError":   tsm.lastError,
		"retryCount":  tsm.retryCount,
	}
}

// [PUBLIC] getCurrentNodeID 获取当前节点 ID
func (tsm *TailscaleService) getCurrentNodeID() (string, error) {
	// 获取当前节点的 Tailscale IP
	tailscaleIP, err := tsm.preparer.GetTailscaleClient().GetIP(context.Background())
	if err != nil {
		return "", fmt.Errorf("failed to get tailscale IP: %v", err)
	}

	// 从 Headscale 获取所有节点，找到匹配的节点
	nodes, err := tsm.preparer.GetHeadscaleClient().ListNodes(context.Background(), "")
	if err != nil {
		return "", fmt.Errorf("failed to list nodes: %v", err)
	}

	for _, node := range nodes.Nodes {
		for _, nodeIP := range node.IPAddresses {
			if nodeIP == tailscaleIP.String() {
				return node.ID, nil
			}
		}
	}

	return "", fmt.Errorf("node with IP %s not found in Headscale", tailscaleIP.String())
}

// [PUBLIC] setupClientRoutePreferences 设置客户端路由偏好
func (tsm *TailscaleService) setupClientRoutePreferences() error {
	logging.Infof("Setting up client route preferences")

	// 设置接受路由
	if err := tsm.preparer.GetTailscaleClient().AcceptRoutes(context.Background()); err != nil {
		return fmt.Errorf("failed to accept routes: %v", err)
	}

	logging.Infof("Client route preferences set successfully")
	return nil
}

// waitForRouteSync 等待路由同步到 Headscale
// [PUBLIC] waitForRouteSync 等待路由同步
func (tsm *TailscaleService) waitForRouteSync(podLocalCIDR string) error {
	// 获取当前节点 ID
	nodeID, err := tsm.getCurrentNodeID()
	if err != nil {
		return fmt.Errorf("failed to get current node ID: %v", err)
	}

	condition := func() (bool, error) {
		// 检查路由是否已同步
		allRoutes, err := tsm.preparer.GetHeadscaleClient().GetRoutes(context.Background())
		if err != nil {
			return false, err
		}

		// 查找我们的路由
		for _, route := range allRoutes.Routes {
			if route.Node.ID == nodeID && route.Prefix == podLocalCIDR {
				logging.Infof("Route %s synced to Headscale (Advertised: %v)", podLocalCIDR, route.Advertised)
				return true, nil
			}
		}

		return false, nil
	}

	return tsm.waitForCondition(condition, 75*time.Second, 5*time.Second, 15, fmt.Sprintf("route %s to sync to Headscale", podLocalCIDR))
}

// waitForCondition 通用等待条件函数，消除重复的等待逻辑
func (tsm *TailscaleService) waitForCondition(
	condition func() (bool, error),
	timeout time.Duration,
	interval time.Duration,
	maxRetries int,
	description string,
) error {
	logging.Infof("Waiting for %s...", description)

	timeoutCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for i := 0; i < maxRetries; i++ {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("%s timeout after %d attempts", description, i+1)
		case <-ticker.C:
			ready, err := condition()
			if err != nil {
				logging.Debugf("%s not ready (attempt %d/%d): %v", description, i+1, maxRetries, err)
				continue
			}

			if ready {
				logging.Infof("%s is ready after %d attempts", description, i+1)
				return nil
			}

			logging.Debugf("%s not ready yet (attempt %d/%d)", description, i+1, maxRetries)
		}
	}

	return fmt.Errorf("%s not ready after %d attempts", description, maxRetries)
}

// min 返回两个整数中较小的一个
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// =============================================================================
// Utility Functions
// =============================================================================

// validateAndEnsureDir 验证并确保目录路径有效，如果无效则使用默认路径
func validateAndEnsureDir(path string, defaultPath string) string {
	if path == "" || path == "." || path == "/" {
		logging.Warnf("Invalid directory path: %s, using default: %s", path, defaultPath)
		return defaultPath
	}

	// 检查路径是否包含无效字符
	if strings.Contains(path, "..") || strings.Contains(path, "//") {
		logging.Warnf("Suspicious directory path: %s, using default: %s", path, defaultPath)
		return defaultPath
	}

	return path
}

// =============================================================================
// Tailscale Service Implementation
// =============================================================================

// monitorAuthKeyExpiration 监控认证密钥过期时间，在即将过期时自动刷新
func (tsm *TailscaleService) monitorAuthKeyExpiration(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour) // 每小时检查一次
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tsm.checkAndRefreshAuthKeyIfNeeded()
		}
	}
}

// checkAndRefreshAuthKeyIfNeeded 检查并在需要时刷新认证密钥
func (tsm *TailscaleService) checkAndRefreshAuthKeyIfNeeded() {
	if !tsm.validateAuthKey() {
		return
	}

	// 如果密钥在 2 小时内过期，提前刷新
	expiresIn := time.Until(tsm.authKeyExpiredTime)
	if expiresIn > 0 && expiresIn < 2*time.Hour {
		logging.Infof("Auth key expires in %v, refreshing early", expiresIn)

		// 在后台刷新密钥，避免阻塞主流程
		go func() {
			if err := tsm.refreshAuthKeyFromHeadscale(); err != nil {
				logging.Errorf("Failed to refresh auth key in background: %v", err)
			}
		}()
	}
}
