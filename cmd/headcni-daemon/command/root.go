package command

import (
	"context"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap/zapcore"
	"k8s.io/klog/v2"

	"github.com/binrclab/headcni/cmd/headcni-daemon/config"
	"github.com/binrclab/headcni/pkg/daemon"
	"github.com/binrclab/headcni/pkg/logging"
)

// CommandStats 命令执行统计
type CommandStats struct {
	StartTime time.Time
	EndTime   time.Time
	Success   bool
	Error     error
}

var (
	rootCmd = &cobra.Command{
		Use:   "headcni-daemon",
		Short: "HeadCNI daemon for Kubernetes CNI plugin",
		Long: `HeadCNI daemon is a Kubernetes CNI plugin that provides networking
using Tailscale for secure, encrypted communication between nodes.

The daemon runs on each Kubernetes node and manages:
- Tailscale network connectivity
- CNI plugin configuration
- Node information synchronization
- Network interface management

Configuration priority (highest to lowest):
1. Command line flags (debug/temporary overrides)
2. Environment variables (container deployment)
3. Configuration file (persistent settings)
4. Default constants (fallback values)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			stats := &CommandStats{StartTime: time.Now()}
			defer func() {
				stats.EndTime = time.Now()
				logCommandStats(stats)
			}()

			err := runDaemon(cmd)
			stats.Success = err == nil
			stats.Error = err
			return err
		},
	}
)

// init initializes the command structure
func init() {
	// Configuration file path
	rootCmd.Flags().String("config", "", "Path to configuration file (YAML format)")

	// Core configuration parameters
	rootCmd.Flags().String("tailscale-url", "", "Tailscale server URL (debug override)")
	rootCmd.Flags().String("tailscale-socket", "", "Tailscale socket path (debug override)")
	rootCmd.Flags().Int("tailscale-mtu", 0, "Tailscale MTU (debug override)")
	rootCmd.Flags().String("pod-cidr", "", "Pod CIDR (debug override)")
	rootCmd.Flags().String("service-cidr", "", "Service CIDR (debug override)")
	rootCmd.Flags().String("log-level", "", "Log level (debug override)")

	// Monitoring parameters
	rootCmd.Flags().Bool("monitoring-enabled", false, "Enable monitoring (debug override)")
	rootCmd.Flags().Int("metrics-port", 0, "Metrics server port (debug override)")
	rootCmd.Flags().String("metrics-path", "", "Metrics server path (debug override)")

	// Advanced parameters
	rootCmd.Flags().String("headscale-url", "", "Headscale server URL (advanced debug)")
	rootCmd.Flags().String("headscale-auth-key", "", "Headscale API key (advanced debug)")
	rootCmd.Flags().String("tailscale-mode", "", "Tailscale mode (advanced debug)")
	rootCmd.Flags().String("tailscale-user", "", "Tailscale user (advanced debug)")
	rootCmd.Flags().String("tailscale-tags", "", "Tailscale tags (advanced debug)")
	rootCmd.Flags().Int("network-mtu", 0, "Network MTU (advanced debug)")
	rootCmd.Flags().Bool("enable-ipv6", false, "Enable IPv6 (advanced debug)")
	rootCmd.Flags().Bool("enable-network-policy", false, "Enable network policy (advanced debug)")
	rootCmd.Flags().String("ipam-type", "", "IPAM type (advanced debug)")
	rootCmd.Flags().String("ipam-strategy", "", "IP allocation strategy (advanced debug)")
	rootCmd.Flags().Bool("magic-dns-enabled", false, "Enable Magic DNS (advanced debug)")

	// 添加超时参数
	rootCmd.Flags().Duration("timeout", 0, "Command timeout (0 = no timeout)")
}

// NewHeadCNIDaemonCommand creates the main HeadCNI daemon command
func NewHeadCNIDaemonCommand() *cobra.Command {
	return rootCmd
}

// runDaemon runs the daemon with the given command
func runDaemon(cmd *cobra.Command) error {
	// 设置日志级别
	if err := setupLogLevel(cmd); err != nil {
		return fmt.Errorf("failed to setup log level: %v", err)
	}

	// 检查超时设置
	timeout, _ := cmd.Flags().GetDuration("timeout")
	var ctx context.Context
	var cancel context.CancelFunc

	if timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
		defer cancel()
	} else {
		ctx = context.Background()
	}

	// Load configuration with priority
	cfg, err := config.LoadConfigWithPriority(cmd)
	if err != nil {
		return fmt.Errorf("failed to load config with priority: %v", err)
	}

	// 直接使用 daemon.New 初始化
	d, cleanup, err := daemon.InitDaemon(cfg)
	if err != nil {
		return fmt.Errorf("failed to create daemon: %v", err)
	}
	defer cleanup()

	// 启动守护进程
	if err := d.StartWithContext(ctx); err != nil {
		return fmt.Errorf("failed to start daemon: %v", err)
	}
	defer func() {
		if err := d.Stop(); err != nil {
			logging.Errorf("Failed to stop daemon: %v", err)
		}
	}()

	// 等待上下文取消
	<-ctx.Done()

	return nil
}

// setupLogLevel 设置日志级别
func setupLogLevel(cmd *cobra.Command) error {
	logLevel, _ := cmd.Flags().GetString("log-level")
	if logLevel == "" {
		// 从环境变量获取
		logLevel = os.Getenv("LOG_LEVEL")
	}

	if logLevel == "" {
		logLevel = "info" // 默认级别
	}

	// 根据日志级别初始化 zap logger
	var zapLevel zapcore.LevelEnabler
	switch logLevel {
	case "debug":
		zapLevel = zapcore.DebugLevel
	case "info":
		zapLevel = zapcore.InfoLevel
	case "warn":
		zapLevel = zapcore.WarnLevel
	case "error":
		zapLevel = zapcore.ErrorLevel
	default:
		return fmt.Errorf("unsupported log level: %s", logLevel)
	}

	// 初始化日志配置
	logFile := os.Getenv("LOG_FILE")
	if logFile == "" {
		logFile = "/var/log/headcni/headcni-daemon.log"
	}

	// 确保日志目录存在
	logDir := path.Dir(logFile)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory %s: %v", logDir, err)
	}

	// 使用自定义的 logging 包初始化日志
	config := logging.DefaultConfig().
		WithLogFile(logFile).
		WithLevel(zapLevel).
		WithConsole(true) // 在开发环境中同时输出到控制台

	if err := logging.Init(config); err != nil {
		return fmt.Errorf("failed to initialize logging: %v", err)
	}

	logging.Infof("Log level set to: %s", logLevel)
	return nil
}

// logCommandStats 记录命令执行统计
func logCommandStats(stats *CommandStats) {
	duration := stats.EndTime.Sub(stats.StartTime)
	if stats.Success {
		klog.Infof("Command completed successfully in %v", duration)
	} else {
		klog.Errorf("Command failed after %v: %v", duration, stats.Error)
	}
}
