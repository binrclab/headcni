package tailscale

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
)

// ==================== 核心接口定义 ====================

// TailscaleClient 客户端接口 - 通过socket与tailscaled交互
type TailscaleClient interface {
	// 基本连接管理
	GetStatus(ctx context.Context) (*ipnstate.Status, error)
	GetIP(ctx context.Context) (netip.Addr, error)
	IsConnected(ctx context.Context) bool
	Up(ctx context.Context, authKey string) error
	Down(ctx context.Context) error

	// 路由管理
	AdvertiseRoutes(ctx context.Context, routes ...netip.Prefix) error
	AdvertiseRoute(ctx context.Context, routes ...string) error
	AcceptRoutes(ctx context.Context) error

	// 网络操作
	Ping(ctx context.Context, target string) error
	GetPeers(ctx context.Context) (map[string]*ipnstate.PeerStatus, error)

	// 配置管理
	GetPrefs(ctx context.Context) (*ipn.Prefs, error)
	SetHostname(ctx context.Context, hostname string) error
	SetTimeout(timeout time.Duration)

	// 连接检查
	CheckConnectivity(ctx context.Context) error
}

// TailscaleServer 服务端接口 - 管理tailscaled服务
type TailscaleServer interface {
	// 服务管理
	StartService(ctx context.Context, name string, options ServiceOptions) (*Service, error)
	StopService(ctx context.Context, name string) error
	GetService(name string) (*Service, bool)
	ListServices() []string
	GetServiceStatus(name string) (string, error)

	// 服务生命周期
	StopAll(ctx context.Context) error
}

// ==================== 共享类型定义 ====================
// 注意：Config, BackendStatus, ServiceOptions, Route, RouteStatus 类型已在其他文件中定义
// 这里只定义接口和常量，避免重复定义

// ==================== 核心常量定义 ====================

const (
	// 默认配置值
	DefaultTimeout             = 30 * time.Second
	DefaultRetryAttempts       = 3
	DefaultHealthCheckInterval = 30 * time.Second
	DefaultBaseDir             = "/tmp/tailscale"
	DefaultSocketPath          = "/var/run/headcni/tailscale/tailscaled.sock"

	// 服务状态
	ServiceStatusRunning      = "running"
	ServiceStatusStopped      = "stopped"
	ServiceStatusDisconnected = "disconnected"

	// 连接状态
	BackendStateRunning    = "Running"
	BackendStateNeedsLogin = "NeedsLogin"
	BackendStateStopped    = "Stopped"
)

// ==================== 核心错误定义 ====================

var (
	ErrBackendNotStarted   = fmt.Errorf("backend not started")
	ErrServiceNotFound     = fmt.Errorf("service not found")
	ErrInvalidCIDRFormat   = fmt.Errorf("invalid CIDR format")
	ErrTailscaleNotRunning = fmt.Errorf("tailscale not running")
	ErrNoTailscaleIP       = fmt.Errorf("no tailscale IP assigned")
	ErrConnectivityFailed  = fmt.Errorf("connectivity check failed")
	ErrConfigRequired      = fmt.Errorf("config cannot be nil")
)

func GetServiceMode(mode string) ServiceMode {
	if mode == "host" {
		return ModeSystemTailscaled
	}
	return ModeStandaloneTailscaled
}

func GetTailscaleNic(mode ServiceMode) string {
	if mode == ModeSystemTailscaled {
		return "tailscale0"
	}
	return "headnic01"
}
