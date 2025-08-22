package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Config 表示 HeadCNI 的完整配置
type Config struct {
	Daemon      DaemonConfig      `yaml:"daemon"`
	Headscale   HeadscaleConfig   `yaml:"headscale"`
	Tailscale   TailscaleConfig   `yaml:"tailscale"`
	Network     NetworkConfig     `yaml:"network"`
	IPAM        IPAMConfig        `yaml:"ipam"`
	DNS         DNSConfig         `yaml:"dns"`
	Monitoring  MonitoringConfig  `yaml:"monitoring"`
	Logging     LoggingConfig     `yaml:"logging"`
	Security    SecurityConfig    `yaml:"security"`
	Performance PerformanceConfig `yaml:"performance"`
	ConfigPath  string            `yaml:"configPath"`
}

// DaemonConfig 基础配置
type DaemonConfig struct {
	LogLevel    string `yaml:"logLevel"`
	HostNetwork bool   `yaml:"hostNetwork"`
}

// HeadscaleConfig HeadScale 配置
type HeadscaleConfig struct {
	URL     string `yaml:"url"`
	AuthKey string `yaml:"authKey"`
	Timeout string `yaml:"timeout"`
	Retries int    `yaml:"retries"`
}

// TailscaleConfig Tailscale 配置
type TailscaleConfig struct {
	Mode          string         `yaml:"mode"`
	URL           string         `yaml:"url"`
	Socket        SocketConfig   `yaml:"socket"`
	MTU           int            `yaml:"mtu"`
	AcceptDNS     bool           `yaml:"acceptDNS"`
	Hostname      HostnameConfig `yaml:"hostname"`
	User          string         `yaml:"user"`
	Tags          []string       `yaml:"tags"`
	InterfaceName string         `yaml:"interfaceName"`
}

// SocketConfig Socket 配置
type SocketConfig struct {
	Path string `yaml:"path"`
	Name string `yaml:"name"`
}

// HostnameConfig 主机名配置
type HostnameConfig struct {
	Prefix string `yaml:"prefix"`
	Type   string `yaml:"type"`
}

// NetworkConfig 网络配置
type NetworkConfig struct {
	PodCIDR             PodCIDRConfig `yaml:"podCIDR"`
	ServiceCIDR         string        `yaml:"serviceCIDR"`
	MTU                 int           `yaml:"mtu"`
	EnableIPv6          bool          `yaml:"enableIPv6"`
	EnableNetworkPolicy bool          `yaml:"enableNetworkPolicy"`
}

// PodCIDRConfig Pod CIDR 配置
type PodCIDRConfig struct {
	Base    string `yaml:"base"`
	PerNode string `yaml:"perNode"`
}

// IPAMConfig IPAM 配置
type IPAMConfig struct {
	Type       string         `yaml:"type"`
	Strategy   string         `yaml:"strategy"`
	GCInterval string         `yaml:"gcInterval"`
	Subnets    []SubnetConfig `yaml:"subnets"`
}

// SubnetConfig 子网配置
type SubnetConfig struct {
	Subnet  string `yaml:"subnet"`
	Gateway string `yaml:"gateway"`
}

// DNSConfig DNS 配置
type DNSConfig struct {
	MagicDNS MagicDNSConfig  `yaml:"magicDNS"`
	Custom   CustomDNSConfig `yaml:"custom"`
}

// MagicDNSConfig Magic DNS 配置
type MagicDNSConfig struct {
	Enabled       bool     `yaml:"enabled"`
	Nameservers   []string `yaml:"nameservers"`
	SearchDomains []string `yaml:"searchDomains"`
	Options       []string `yaml:"options"`
}

// CustomDNSConfig 自定义 DNS 配置
type CustomDNSConfig struct {
	Enabled       bool     `yaml:"enabled"`
	Nameservers   []string `yaml:"nameservers"`
	SearchDomains []string `yaml:"searchDomains"`
	Options       []string `yaml:"options"`
}

// MonitoringConfig 监控配置
type MonitoringConfig struct {
	Enabled bool   `yaml:"enabled"`
	Port    int    `yaml:"port"`
	Path    string `yaml:"path"`
}

// CustomMetricsConfig 自定义指标配置
type CustomMetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"`
}

// LoggingConfig 日志配置
type LoggingConfig struct {
	Level  string        `yaml:"level"`
	Format string        `yaml:"format"`
	Output string        `yaml:"output"`
	File   FileLogConfig `yaml:"file"`
}

// FileLogConfig 文件日志配置
type FileLogConfig struct {
	Enabled    bool   `yaml:"enabled"`
	Path       string `yaml:"path"`
	MaxSize    string `yaml:"maxSize"`
	MaxBackups int    `yaml:"maxBackups"`
	MaxAge     string `yaml:"maxAge"`
}

// SecurityConfig 安全配置
type SecurityConfig struct {
	TLS           TLSConfig           `yaml:"tls"`
	Auth          AuthConfig          `yaml:"auth"`
	NetworkPolicy NetworkPolicyConfig `yaml:"networkPolicy"`
}

// TLSConfig TLS 配置
type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"certFile"`
	KeyFile  string `yaml:"keyFile"`
	CAFile   string `yaml:"caFile"`
}

// AuthConfig 认证配置
type AuthConfig struct {
	Enabled bool   `yaml:"enabled"`
	Type    string `yaml:"type"`
	Token   string `yaml:"token"`
}

// NetworkPolicyConfig 网络策略配置
type NetworkPolicyConfig struct {
	Enabled bool     `yaml:"enabled"`
	Ingress []string `yaml:"ingress"`
	Egress  []string `yaml:"egress"`
}

// PerformanceConfig 性能配置
type PerformanceConfig struct {
	ConnectionPool ConnectionPoolConfig `yaml:"connectionPool"`
	Cache          CacheConfig          `yaml:"cache"`
	Concurrency    ConcurrencyConfig    `yaml:"concurrency"`
}

// ConnectionPoolConfig 连接池配置
type ConnectionPoolConfig struct {
	MaxConnections     int    `yaml:"maxConnections"`
	MaxIdleConnections int    `yaml:"maxIdleConnections"`
	ConnectionTimeout  string `yaml:"connectionTimeout"`
	IdleTimeout        string `yaml:"idleTimeout"`
}

// CacheConfig 缓存配置
type CacheConfig struct {
	Enabled bool   `yaml:"enabled"`
	Size    string `yaml:"size"`
	TTL     string `yaml:"ttl"`
}

// ConcurrencyConfig 并发配置
type ConcurrencyConfig struct {
	MaxWorkers int `yaml:"maxWorkers"`
	QueueSize  int `yaml:"queueSize"`
}

// LoadDefaultConfig 加载默认配置
func DefaultConfig() (*Config, error) {
	// 获取当前可执行文件所在目录
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to get executable path: %v", err)
	}

	// 构建默认配置文件路径
	defaultConfigPath := filepath.Join(filepath.Dir(exe), "config", "default.yaml")

	// 尝试加载默认配置文件
	if _, err := os.Stat(defaultConfigPath); err == nil {
		return LoadConfig(defaultConfigPath)
	}

	// 如果默认配置文件不存在，返回内置的默认配置
	return &Config{
		Daemon: DaemonConfig{
			LogLevel:    "info",
			HostNetwork: true,
		},
		Headscale: HeadscaleConfig{
			URL:     "https://hs.binrc.com",
			AuthKey: "",
			Timeout: "30s",
			Retries: 3,
		},
		Tailscale: TailscaleConfig{
			Mode: "daemon",
			URL:  "https://hs.binrc.com",
			Socket: SocketConfig{
				Path: "/var/run/headcni/headcni_tailscale.sock",
				Name: "headcni_tailscale.sock",
			},
			MTU:       1280,
			AcceptDNS: true,
			Hostname: HostnameConfig{
				Prefix: "headcni-pod",
				Type:   "hostname",
			},
			User:          "server",
			Tags:          []string{"tag:control-server", "tag:headcni"},
			InterfaceName: "headcni01",
		},
		Network: NetworkConfig{
			PodCIDR: PodCIDRConfig{
				Base:    "", // 将通过命令行参数或环境变量设置
				PerNode: "/24",
			},
			ServiceCIDR:         "", // 将通过命令行参数或环境变量设置
			MTU:                 1280,
			EnableIPv6:          false,
			EnableNetworkPolicy: true,
		},
		IPAM: IPAMConfig{
			Type:       "host-local",
			Strategy:   "sequential",
			GCInterval: "1h",
			Subnets:    []SubnetConfig{}, // 将根据 podCIDR 动态生成
		},
		DNS: DNSConfig{
			MagicDNS: MagicDNSConfig{
				Enabled: true,
				Nameservers: []string{
					"8.8.8.8",
					"8.8.4.4",
				},
				SearchDomains: []string{
					"cluster.local",
				},
				Options: []string{
					"ndots:5",
					"timeout:2",
				},
			},
			Custom: CustomDNSConfig{
				Enabled:       false,
				Nameservers:   []string{},
				SearchDomains: []string{},
				Options:       []string{},
			},
		},
		Monitoring: MonitoringConfig{
			Enabled: true,
			Port:    8080,
			Path:    "/metrics",
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
			Output: "stdout",
			File: FileLogConfig{
				Enabled:    false,
				Path:       "/var/log/headcni/headcni.log",
				MaxSize:    "100MB",
				MaxBackups: 3,
				MaxAge:     "7d",
			},
		},
		Security: SecurityConfig{
			TLS: TLSConfig{
				Enabled:  false,
				CertFile: "",
				KeyFile:  "",
				CAFile:   "",
			},
			Auth: AuthConfig{
				Enabled: false,
				Type:    "token",
				Token:   "",
			},
			NetworkPolicy: NetworkPolicyConfig{
				Enabled: true,
				Ingress: []string{},
				Egress:  []string{},
			},
		},
		Performance: PerformanceConfig{
			ConnectionPool: ConnectionPoolConfig{
				MaxConnections:     100,
				MaxIdleConnections: 10,
				ConnectionTimeout:  "30s",
				IdleTimeout:        "90s",
			},
			Cache: CacheConfig{
				Enabled: true,
				Size:    "100MB",
				TTL:     "1h",
			},
			Concurrency: ConcurrencyConfig{
				MaxWorkers: 10,
				QueueSize:  100,
			},
		},
	}, nil
}
