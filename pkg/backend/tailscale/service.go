package tailscale

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/binrclab/headcni/pkg/logging"
	"tailscale.com/tsnet"
)

// ServiceManager 管理 Tailscale 服务实例
type ServiceManager struct {
	services map[string]*Service
	mu       sync.RWMutex
}

// Service 表示一个 Tailscale 服务实例
type Service struct {
	Name       string
	ConfigDir  string
	SocketPath string
	StateFile  string
	AuthKey    string
	Hostname   string

	// TSNet模式
	TSNetServer *tsnet.Server

	// 独立tailscaled模式
	SystemTailscaledPID int
	SystemTailscaledCmd *exec.Cmd

	// 运行状态
	IsRunning bool
	StartTime time.Time
	LastError error

	// 配置选项
	Options ServiceOptions

	// 内部状态
	mu sync.RWMutex
}

// verifyRunning 验证服务是否真的在运行
func (s *Service) verifyRunning(ctx context.Context) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	switch s.Options.Mode {
	case ModeSystemTailscaled:
		// 检查系统 tailscaled 是否响应
		return s.verifySystemTailscaled(ctx)
	case ModeStandaloneTailscaled:
		// 检查独立 tailscaled 进程是否运行
		return s.verifyStandaloneTailscaled(ctx)
	case ModeTSNet:
		// 检查 TSNet 服务是否运行
		return s.verifyTSNet(ctx)
	default:
		return fmt.Errorf("unknown service mode: %v", s.Options.Mode)
	}
}

// verifySystemTailscaled 验证系统 tailscaled 是否运行
func (s *Service) verifySystemTailscaled(ctx context.Context) error {
	// 检查 socket 文件是否存在
	if s.Options.SocketPath != "" {
		if _, err := os.Stat(s.Options.SocketPath); os.IsNotExist(err) {
			return fmt.Errorf("socket file not found: %s", s.Options.SocketPath)
		}
	}

	// 可以添加更多验证逻辑，比如尝试连接 socket
	return nil
}

// verifyStandaloneTailscaled 验证独立 tailscaled 是否运行
func (s *Service) verifyStandaloneTailscaled(ctx context.Context) error {
	if s.SystemTailscaledPID <= 0 {
		return fmt.Errorf("no valid PID for standalone tailscaled")
	}

	// 检查进程是否真的在运行
	if process, err := os.FindProcess(s.SystemTailscaledPID); err != nil {
		return fmt.Errorf("failed to find process: %v", err)
	} else {
		// 使用更兼容的方式检查进程状态
		// 在 Linux 上，os.Signal(nil) 不被支持，所以我们使用其他方法
		if err := process.Signal(syscall.Signal(0)); err != nil {
			return fmt.Errorf("process not responding: %v", err)
		}
	}

	return nil
}

// verifyTSNet 验证 TSNet 服务是否运行
func (s *Service) verifyTSNet(ctx context.Context) error {
	if s.TSNetServer == nil {
		return fmt.Errorf("TSNet server not initialized")
	}

	// 检查 TSNet 服务状态
	// 这里可以添加更多具体的验证逻辑
	return nil
}

// ServiceMode 服务模式
type ServiceMode int

const (
	ModeSystemTailscaled     ServiceMode = iota // 使用系统tailscaled服务
	ModeStandaloneTailscaled                    // 自己启动系统级别的tailscaled
	ModeTSNet                                   // 直接使用TSNet
)

// ServiceOptions 服务配置选项
type ServiceOptions struct {
	SocketPath string // 套接字路径
	ConfigDir  string // 配置目录
	AuthKey    string // 认证密钥
	Hostname   string // 主机名
	ControlURL string // 控制服务器URL（默认使用Tailscale官方）
	Mode       ServiceMode
	Logf       func(format string, args ...interface{})
	StateFile  string // 状态文件路径
	Interface  string // 网络接口名称
}

// NewServiceManager 创建新的服务管理器
func NewServiceManager() *ServiceManager {
	sm := &ServiceManager{
		services: make(map[string]*Service),
	}

	return sm
}

// StartService 启动 Tailscale 服务
func (sm *ServiceManager) StartService(ctx context.Context, name string, options ServiceOptions) (*Service, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 检查服务是否已存在
	if service, exists := sm.services[name]; exists {
		// 如果服务正在运行，检查是否真的在运行
		if service.IsRunning {
			// 验证服务是否真的在运行
			if err := service.verifyRunning(ctx); err != nil {
				logging.Warnf("Service %s marked as running but verification failed: %v, will restart", name, err)
				service.IsRunning = false
			} else {
				return service, nil
			}
		}
		// 清理异常或停止的服务
		sm.cleanupService(service)
		// 从 map 中删除异常服务
		delete(sm.services, name)
	}

	// 创建服务配置目录
	configDir := options.ConfigDir
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %v", err)
	}

	service := &Service{
		Name:      options.Interface,
		ConfigDir: configDir,
		StateFile: options.StateFile,
		AuthKey:   options.AuthKey,
		Hostname:  options.Hostname,
		Options:   options,
		StartTime: time.Now(),
	}

	// 根据配置选择启动方式
	var err error
	switch options.Mode {
	case ModeSystemTailscaled:
		err = service.checkSystemTailscaled(ctx)
	case ModeStandaloneTailscaled:
		err = service.startWithStandaloneTailscaled(ctx)
	case ModeTSNet:
		err = service.startWithTSNet(ctx)
	default:
		err = service.startWithTSNet(ctx) // 默认使用TSNet
	}

	if err != nil {
		// 启动失败时，确保清理资源
		sm.cleanupService(service)
		return nil, fmt.Errorf("failed to start service: %v", err)
	}

	// 验证服务是否真的启动成功
	if err := service.verifyRunning(ctx); err != nil {
		// 验证失败时，清理资源并返回错误
		sm.cleanupService(service)
		return nil, fmt.Errorf("service started but verification failed: %v", err)
	}

	service.IsRunning = true
	sm.services[name] = service

	return service, nil
}

// checkSystemTailscaled 检查系统tailscaled服务是否已启动
func (s *Service) checkSystemTailscaled(ctx context.Context) error {
	if s.Options.Logf != nil {
		s.Options.Logf("检查系统级别tailscaled服务是否已启动")
	}

	// 检查系统tailscaled服务是否已启动
	cmd := exec.Command("systemctl", "status", "tailscaled")
	err := cmd.Run()
	if err != nil {
		if s.Options.Logf != nil {
			s.Options.Logf("系统级别tailscaled服务未启动: %v", err)
		}
		return fmt.Errorf("系统tailscaled服务未启动")
	}

	if s.Options.Logf != nil {
		s.Options.Logf("系统级别tailscaled服务已启动")
	}

	// 设置socket路径为系统默认路径
	s.SocketPath = "/var/run/tailscale/tailscaled.sock"

	return nil
}

// startWithTSNet 使用TSNet启动服务
func (s *Service) startWithTSNet(ctx context.Context) error {
	if s.Options.Logf != nil {
		s.Options.Logf("启动TSNet模式")
	}

	// 创建配置目录
	if err := os.MkdirAll(s.ConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %v", err)
	}

	// 创建TSNet服务器
	tsnetServer := &tsnet.Server{
		Dir:        s.ConfigDir,
		AuthKey:    s.AuthKey,
		Hostname:   s.Hostname,
		Ephemeral:  false, // 默认非临时节点
		ControlURL: s.Options.ControlURL,
		Logf:       s.Options.Logf,
	}

	s.TSNetServer = tsnetServer

	// 启动TSNet服务器
	if err := tsnetServer.Start(); err != nil {
		return fmt.Errorf("failed to start tsnet server: %v", err)
	}

	// 等待服务器准备就绪
	if err := s.waitForReady(ctx, 60*time.Second); err != nil {
		tsnetServer.Close()
		return fmt.Errorf("server not ready: %v", err)
	}

	s.SocketPath = s.getSocketPath()

	return nil
}

// waitForReady 等待服务器准备就绪
func (s *Service) waitForReady(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	attempts := 0
	maxAttempts := int(timeout / (3 * time.Second))

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for server: %v", ctx.Err())
		case <-ticker.C:
			attempts++

			localClient, err := s.TSNetServer.LocalClient()
			if err != nil {
				if attempts >= maxAttempts {
					return fmt.Errorf("failed to get local client after %d attempts: %v", attempts, err)
				}
				continue
			}

			status, err := localClient.Status(context.Background())
			if err != nil {
				if attempts >= maxAttempts {
					return fmt.Errorf("failed to get status after %d attempts: %v", attempts, err)
				}
				continue
			}

			// 检查登录状态
			if status.BackendState == "NeedsLogin" {
				if attempts >= maxAttempts/3 {
					return fmt.Errorf("server still needs login after %d attempts", attempts)
				}
				continue
			}

			// 检查是否已连接
			if status.BackendState == "Running" {
				if status.Self != nil && len(status.Self.TailscaleIPs) > 0 {
					return nil // 完全连接
				}
				if attempts >= maxAttempts*2/3 {
					return nil // 允许继续，即使没有IP
				}
			}

			if attempts >= maxAttempts*2/3 {
				return nil
			}
		}
	}
}

// getSocketPath 内部获取socket路径
func (s *Service) getSocketPath() string {
	if s.TSNetServer != nil {
		// TSNet服务器使用特殊的socket路径格式
		return "tsnet://" + s.Name
	}
	return s.SocketPath
}

// startWithStandaloneTailscaled 启动独立的系统tailscaled服务
func (s *Service) startWithStandaloneTailscaled(ctx context.Context) error {
	if s.Options.Logf != nil {
		s.Options.Logf("启动独立系统级别tailscaled模式")
	}

	// 创建配置目录
	if err := os.MkdirAll(s.ConfigDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %v", err)
	}

	// 设置socket路径
	s.SocketPath = s.Options.SocketPath

	// 启动tailscaled进程
	if err := s.startTailscaledProcess(); err != nil {
		return fmt.Errorf("failed to start tailscaled process: %v", err)
	}

	return nil
}

// startTailscaledProcess 启动tailscaled进程
func (s *Service) startTailscaledProcess() error {
	// 检查PID文件是否存在
	pidFile := filepath.Join(s.ConfigDir, "tailscaled.pid")

	// 如果PID文件存在，检查进程是否还在运行
	if _, err := os.Stat(pidFile); err == nil {
		if s.checkExistingProcess(pidFile) {
			if s.Options.Logf != nil {
				s.Options.Logf("发现现有tailscaled进程，继承管理 PID: %d", s.SystemTailscaledPID)
			}
			return nil
		}
	}

	// 启动新的tailscaled进程
	cmd := exec.Command("tailscaled",
		"--state", s.StateFile,
		"--socket", s.SocketPath,
		"--tun", fmt.Sprintf("%s", s.Name),
		"--port", "41645",
		"--verbose", "1",
		"--statedir", s.ConfigDir,
	)

	// 捕获输出用于调试
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// 在后台运行
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start tailscaled: %v", err)
	}

	s.SystemTailscaledPID = cmd.Process.Pid
	s.SystemTailscaledCmd = cmd

	// 写入PID文件
	if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", cmd.Process.Pid)), 0644); err != nil {
		if s.Options.Logf != nil {
			s.Options.Logf("警告：无法写入PID文件: %v", err)
		}
	}

	if s.Options.Logf != nil {
		s.Options.Logf("tailscaled process started with PID: %d", cmd.Process.Pid)
	}

	// 等待socket文件创建
	maxWait := 30 * time.Second
	checkInterval := 500 * time.Millisecond
	elapsed := time.Duration(0)

	for elapsed < maxWait {
		if _, err := os.Stat(s.SocketPath); err == nil {
			// socket文件已创建
			if s.Options.Logf != nil {
				s.Options.Logf("tailscaled socket created: %s", s.SocketPath)
			}
			return nil
		}
		time.Sleep(checkInterval)
		elapsed += checkInterval

		// 检查进程是否还在运行
		if cmd.Process == nil {
			// 进程已退出，检查输出
			if s.Options.Logf != nil {
				s.Options.Logf("tailscaled process exited. stdout: %s, stderr: %s", stdout.String(), stderr.String())
			}
			return fmt.Errorf("tailscaled process failed to start")
		}

		// 每5秒输出一次调试信息
		if elapsed%(5*time.Second) == 0 && s.Options.Logf != nil {
			s.Options.Logf("waiting for tailscaled socket... (elapsed: %v)", elapsed)
		}
	}

	// 超时，检查进程状态
	if cmd.Process != nil {
		if s.Options.Logf != nil {
			s.Options.Logf("tailscaled process still running but socket not created. stdout: %s, stderr: %s", stdout.String(), stderr.String())
		}
	}

	return fmt.Errorf("timeout waiting for tailscaled socket: %s", s.SocketPath)
}

// checkExistingProcess 检查现有进程
func (s *Service) checkExistingProcess(pidFile string) bool {
	// 读取PID文件
	pidData, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}

	pidStr := strings.TrimSpace(string(pidData))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return false
	}

	// 检查进程是否存在
	cmd := exec.Command("kill", "-0", fmt.Sprintf("%d", pid))
	if err := cmd.Run(); err != nil {
		return false
	}

	// 检查进程是否是tailscaled
	cmd = exec.Command("ps", "-p", fmt.Sprintf("%d", pid), "-o", "comm=")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	processName := strings.TrimSpace(string(output))
	if processName != "tailscaled" {
		return false
	}

	// 检查socket文件是否存在
	if _, err := os.Stat(s.SocketPath); err != nil {
		return false
	}

	// 检查网络接口是否存在
	interfaceName := fmt.Sprintf("%s", s.Name)
	cmd = exec.Command("ip", "link", "show", interfaceName)
	if err := cmd.Run(); err != nil {
		if s.Options.Logf != nil {
			s.Options.Logf("网络接口 %s 不存在", interfaceName)
		}
		return false
	}

	// 设置PID和socket路径
	s.SystemTailscaledPID = pid
	if s.Options.Logf != nil {
		s.Options.Logf("网络接口 %s 存在", interfaceName)
	}

	return true
}

// checkExistingTailscaled 检查是否已有tailscaled进程
func (s *Service) checkExistingTailscaled() bool {
	// 检查系统默认的tailscaled socket
	defaultSocket := "/var/run/tailscale/tailscaled.sock"
	if _, err := os.Stat(defaultSocket); err == nil {
		// 检查是否有进程在使用这个socket
		cmd := exec.Command("lsof", defaultSocket)
		if err := cmd.Run(); err == nil {
			// 有进程在使用，直接使用这个socket
			s.SocketPath = defaultSocket
			return true
		}
	}

	// 检查是否有其他tailscaled进程
	cmd := exec.Command("pgrep", "tailscaled")
	if err := cmd.Run(); err == nil {
		// 找到tailscaled进程，尝试使用默认socket
		s.SocketPath = "/var/run/tailscale/tailscaled.sock"
		return true
	}

	return false
}

// stopTailscaledProcess 停止tailscaled进程
func (s *Service) stopTailscaledProcess() error {
	if s.SystemTailscaledCmd != nil && s.SystemTailscaledCmd.Process != nil {
		return s.SystemTailscaledCmd.Process.Kill()
	}

	// 查找并停止tailscaled进程
	cmd := exec.Command("pkill", "-f", fmt.Sprintf("tailscaled.*headcni%s", s.Name))
	return cmd.Run()
}

// Stop 停止服务
func (s *Service) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.IsRunning {
		return nil
	}

	// 根据启动模式停止相应的服务
	switch s.Options.Mode {
	case ModeSystemTailscaled:
		// 使用系统tailscaled模式，不需要停止
		if s.Options.Logf != nil {
			s.Options.Logf("使用系统tailscaled，无需停止")
		}
	case ModeStandaloneTailscaled:
		// 停止独立tailscaled进程
		if err := s.stopTailscaledProcess(); err != nil {
			return fmt.Errorf("failed to stop tailscaled process: %v", err)
		}
		if s.Options.Logf != nil {
			s.Options.Logf("独立tailscaled已停止")
		}
	case ModeTSNet:
		// 停止TSNet模式
		if s.TSNetServer != nil {
			s.TSNetServer.Close()
		}
		if s.Options.Logf != nil {
			s.Options.Logf("TSNet服务已停止")
		}
	}

	s.IsRunning = false
	return nil
}

// GetSocketPath 获取socket路径
func (s *Service) GetSocketPath() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.SocketPath
}

// GetStatus 获取服务状态
func (s *Service) GetStatus(ctx context.Context) (map[string]interface{}, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := map[string]interface{}{
		"name":        s.Name,
		"is_running":  s.IsRunning,
		"start_time":  s.StartTime,
		"socket_path": s.SocketPath,
		"mode":        "tsnet",
	}

	switch s.Options.Mode {
	case ModeSystemTailscaled:
		status["mode"] = "system"
	case ModeStandaloneTailscaled:
		status["mode"] = "standalone"
		if s.SystemTailscaledPID > 0 {
			status["pid"] = s.SystemTailscaledPID
		}
	case ModeTSNet:
		status["mode"] = "tsnet"
	}

	if s.TSNetServer != nil {
		localClient, err := s.TSNetServer.LocalClient()
		if err == nil {
			if tsStatus, err := localClient.Status(ctx); err == nil {
				status["tailscale_state"] = tsStatus.BackendState
				if tsStatus.Self != nil {
					status["tailscale_ips"] = tsStatus.Self.TailscaleIPs
				}
			}
		}
	}

	return status, nil
}

// GetNetworkInterface 获取网络接口信息（仅系统tailscaled模式）
func (s *Service) GetNetworkInterface() (string, error) {
	if s.Options.Mode != ModeSystemTailscaled && s.Options.Mode != ModeStandaloneTailscaled {
		return "", fmt.Errorf("network interface only available in system tailscaled mode")
	}

	// 返回tailscaled创建的TUN接口名称
	return fmt.Sprintf("headcni%s", s.Name), nil
}

// StopService 停止 Tailscale 服务
func (sm *ServiceManager) StopService(ctx context.Context, name string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	service, exists := sm.services[name]
	if !exists {
		return fmt.Errorf("service %s not found", name)
	}

	return sm.stopService(ctx, service)
}

// stopService 内部停止服务方法
func (sm *ServiceManager) stopService(ctx context.Context, service *Service) error {
	if err := service.Stop(ctx); err != nil {
		return err
	}

	sm.cleanupService(service)
	delete(sm.services, service.Name)

	return nil
}

// cleanupService 清理服务资源
func (sm *ServiceManager) cleanupService(service *Service) {
	// 清理socket文件（如果存在）
	if service.SocketPath != "" && !strings.HasPrefix(service.SocketPath, "tsnet://") {
		os.Remove(service.SocketPath)
	}
}

// GetService 获取服务
func (sm *ServiceManager) GetService(name string) (*Service, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	service, exists := sm.services[name]
	return service, exists
}

// ListServices 列出所有服务
func (sm *ServiceManager) ListServices() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	names := make([]string, 0, len(sm.services))
	for name := range sm.services {
		names = append(names, name)
	}

	return names
}

// GetServiceStatus 获取服务状态
func (sm *ServiceManager) GetServiceStatus(name string) (string, error) {
	service, exists := sm.GetService(name)
	if !exists {
		return "not_found", nil
	}

	if !service.IsRunning {
		return "stopped", nil
	}

	// 检查服务是否真的在运行
	switch service.Options.Mode {
	case ModeSystemTailscaled:
		// 检查系统tailscaled服务状态
		cmd := exec.Command("systemctl", "status", "tailscaled")
		if err := cmd.Run(); err != nil {
			service.LastError = err
			return "disconnected", nil
		}
	case ModeStandaloneTailscaled:
		// 检查独立tailscaled进程是否存在
		if service.SystemTailscaledPID > 0 {
			cmd := exec.Command("kill", "-0", fmt.Sprintf("%d", service.SystemTailscaledPID))
			if err := cmd.Run(); err != nil {
				service.LastError = err
				return "disconnected", nil
			}
		}
	case ModeTSNet:
		if service.TSNetServer != nil {
			// 检查TSNet服务器状态
			localClient, err := service.TSNetServer.LocalClient()
			if err != nil {
				service.LastError = err
				return "disconnected", nil
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			if _, err := localClient.Status(ctx); err != nil {
				service.LastError = err
				return "disconnected", nil
			}
		}
	}

	return "running", nil
}

// RestartService 重启服务
func (sm *ServiceManager) RestartService(ctx context.Context, name string) error {
	service, exists := sm.GetService(name)
	if !exists {
		return fmt.Errorf("service %s not found", name)
	}

	// 保存配置
	options := service.Options

	// 停止服务
	if err := sm.StopService(ctx, name); err != nil {
		return fmt.Errorf("failed to stop service: %v", err)
	}

	// 等待一段时间
	time.Sleep(2 * time.Second)

	// 重新启动
	_, err := sm.StartService(ctx, name, options)
	return err
}

// StopAll 停止所有服务
func (sm *ServiceManager) StopAll(ctx context.Context) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var errors []error
	for _, service := range sm.services {
		if err := sm.stopService(ctx, service); err != nil {
			errors = append(errors, fmt.Errorf("failed to stop service %s: %v", service.Name, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("multiple errors stopping services: %v", errors)
	}

	return nil
}
