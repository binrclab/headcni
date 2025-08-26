package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// LoadConfigWithPriority loads configuration with priority order
// Priority: command line args > environment variables > config file > default constants
func LoadConfigWithPriority(cmd *cobra.Command) (*Config, error) {
	// 1. Create default configuration (lowest priority)
	cfg := &Config{}

	// 2. Load configuration file (medium priority - persistent settings)
	configFile, _ := cmd.Flags().GetString("config")
	cfg.ConfigPath = configFile
	if configFile != "" {
		if err := loadConfigFile(cfg, configFile); err != nil {
			return nil, errors.Wrap(err, "failed to load config file")
		}
	}

	// 3. Apply environment variable overrides (high priority - container deployment)
	applyEnvironmentOverrides(cfg)

	// 4. Apply command line argument overrides (highest priority - debug/temporary)
	applyCommandLineOverrides(cfg, cmd)

	return cfg, nil
}

// loadConfigFile loads configuration from file and merges with existing config
func loadConfigFile(cfg *Config, configFile string) error {
	loadedCfg, err := LoadConfig(configFile)
	if err != nil {
		return err
	}

	// Merge configuration file into default configuration
	mergeConfig(cfg, loadedCfg)
	return nil
}

// LoadConfig 从文件加载配置
func LoadConfig(configPath string) (*Config, error) {
	// 如果配置文件路径为空，使用默认配置
	if configPath == "" {
		return DefaultConfig()
	}

	// 读取配置文件
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %v", configPath, err)
	}

	// 解析 YAML
	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %v", configPath, err)
	}

	return &config, nil
}

// applyEnvironmentOverrides applies environment variable overrides to configuration
func applyEnvironmentOverrides(cfg *Config) {
	// HeadScale configuration
	if url := os.Getenv("HEADSCALE_URL"); url != "" {
		cfg.Headscale.URL = url
	}
	if authKey := os.Getenv("HEADSCALE_AUTH_KEY"); authKey != "" {
		cfg.Headscale.AuthKey = authKey
	}

	// Tailscale core configuration
	if mode := os.Getenv("TAILSCALE_MODE"); mode != "" {
		cfg.Tailscale.Mode = mode
	}
	if url := os.Getenv("TAILSCALE_URL"); url != "" {
		cfg.Tailscale.URL = url
	}
	if socket := os.Getenv("TAILSCALE_SOCKET_PATH"); socket != "" {
		cfg.Tailscale.Socket.Path = socket
	}
	if mtuStr := os.Getenv("TAILSCALE_MTU"); mtuStr != "" {
		if mtu, err := strconv.Atoi(mtuStr); err == nil {
			cfg.Tailscale.MTU = mtu
		}
	}
	if user := os.Getenv("TAILSCALE_USER"); user != "" {
		cfg.Tailscale.User = user
	}
	if tags := os.Getenv("TAILSCALE_TAGS"); tags != "" {
		cfg.Tailscale.Tags = strings.Split(tags, ",")
	}

	// Network core configuration
	if podCIDR := os.Getenv("POD_CIDR"); podCIDR != "" {
		cfg.Network.PodCIDR.Base = podCIDR
	}
	if serviceCIDR := os.Getenv("SERVICE_CIDR"); serviceCIDR != "" {
		cfg.Network.ServiceCIDR = serviceCIDR
	}

	// Monitoring configuration
	if monitoringEnabled := os.Getenv("MONITORING_ENABLED"); monitoringEnabled != "" {
		if enabled, err := strconv.ParseBool(monitoringEnabled); err == nil {
			cfg.Monitoring.Enabled = enabled
		}
	}
	if metricsPortStr := os.Getenv("METRICS_PORT"); metricsPortStr != "" {
		if port, err := strconv.Atoi(metricsPortStr); err == nil {
			cfg.Monitoring.Port = port
		}
	}

	// Logging configuration
	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		cfg.Daemon.LogLevel = logLevel
	}
}

// applyCommandLineOverrides applies command line argument overrides to configuration
func applyCommandLineOverrides(cfg *Config, cmd *cobra.Command) {
	// Core configuration parameters
	if tailscaleURL, _ := cmd.Flags().GetString("tailscale-url"); tailscaleURL != "" {
		cfg.Tailscale.URL = tailscaleURL
	}
	if podCIDR, _ := cmd.Flags().GetString("pod-cidr"); podCIDR != "" {
		cfg.Network.PodCIDR.Base = podCIDR
	}
	if serviceCIDR, _ := cmd.Flags().GetString("service-cidr"); serviceCIDR != "" {
		cfg.Network.ServiceCIDR = serviceCIDR
	}
	if logLevel, _ := cmd.Flags().GetString("log-level"); logLevel != "" {
		cfg.Daemon.LogLevel = logLevel
	}

	// Monitoring parameters
	if monitoringEnabled, _ := cmd.Flags().GetBool("monitoring-enabled"); monitoringEnabled {
		cfg.Monitoring.Enabled = monitoringEnabled
	}
	if metricsPort, _ := cmd.Flags().GetInt("metrics-port"); metricsPort > 0 {
		cfg.Monitoring.Port = metricsPort
	}
	if metricsPath, _ := cmd.Flags().GetString("metrics-path"); metricsPath != "" {
		cfg.Monitoring.Path = metricsPath
	}

	// Advanced parameters
	if headscaleURL, _ := cmd.Flags().GetString("headscale-url"); headscaleURL != "" {
		cfg.Headscale.URL = headscaleURL
	}
	if headscaleAuthKey, _ := cmd.Flags().GetString("headscale-auth-key"); headscaleAuthKey != "" {
		cfg.Headscale.AuthKey = headscaleAuthKey
	}
	if tailscaleMode, _ := cmd.Flags().GetString("tailscale-mode"); tailscaleMode != "" {
		cfg.Tailscale.Mode = tailscaleMode
	}
	if tailscaleUser, _ := cmd.Flags().GetString("tailscale-user"); tailscaleUser != "" {
		cfg.Tailscale.User = tailscaleUser
	}
	if tailscaleTags, _ := cmd.Flags().GetString("tailscale-tags"); tailscaleTags != "" {
		cfg.Tailscale.Tags = strings.Split(tailscaleTags, ",")
	}
	if enableIPv6, _ := cmd.Flags().GetBool("enable-ipv6"); enableIPv6 {
		cfg.Network.EnableIPv6 = enableIPv6
	}
	if enableNetworkPolicy, _ := cmd.Flags().GetBool("enable-network-policy"); enableNetworkPolicy {
		cfg.Network.EnableNetworkPolicy = enableNetworkPolicy
	}
	if magicDNSEnabled, _ := cmd.Flags().GetBool("magic-dns-enabled"); magicDNSEnabled {
		cfg.DNS.MagicDNS.Enabled = magicDNSEnabled
	}
}

// mergeConfig merges configuration, ensuring existing values are not overwritten
func mergeConfig(target, source *Config) {
	// HeadScale configuration
	if source.Headscale.URL != "" {
		target.Headscale.URL = source.Headscale.URL
	}
	if source.Headscale.AuthKey != "" {
		target.Headscale.AuthKey = source.Headscale.AuthKey
	}

	// Tailscale configuration
	if source.Tailscale.Mode != "" {
		target.Tailscale.Mode = source.Tailscale.Mode
	}
	if source.Tailscale.URL != "" {
		target.Tailscale.URL = source.Tailscale.URL
	}
	if source.Tailscale.Socket.Path != "" {
		target.Tailscale.Socket.Path = source.Tailscale.Socket.Path
	}
	if source.Tailscale.MTU > 0 {
		target.Tailscale.MTU = source.Tailscale.MTU
	}
	if source.Tailscale.AcceptDNS {
		target.Tailscale.AcceptDNS = source.Tailscale.AcceptDNS
	}
	if source.Tailscale.Hostname.Prefix != "" {
		target.Tailscale.Hostname.Prefix = source.Tailscale.Hostname.Prefix
	}
	if source.Tailscale.Hostname.Type != "" {
		target.Tailscale.Hostname.Type = source.Tailscale.Hostname.Type
	}
	if source.Tailscale.User != "" {
		target.Tailscale.User = source.Tailscale.User
	}
	if len(source.Tailscale.Tags) > 0 {
		target.Tailscale.Tags = source.Tailscale.Tags
	}

	// Network configuration
	if source.Network.PodCIDR.Base != "" {
		target.Network.PodCIDR.Base = source.Network.PodCIDR.Base
	}
	if source.Network.ServiceCIDR != "" {
		target.Network.ServiceCIDR = source.Network.ServiceCIDR
	}
	if source.Network.MTU > 0 {
		target.Network.MTU = source.Network.MTU
	}
	if source.Network.EnableIPv6 {
		target.Network.EnableIPv6 = source.Network.EnableIPv6
	}
	if source.Network.EnableNetworkPolicy {
		target.Network.EnableNetworkPolicy = source.Network.EnableNetworkPolicy
	}

	// IPAM configuration
	if source.IPAM.Type != "" {
		target.IPAM.Type = source.IPAM.Type
	}
	if source.IPAM.Strategy != "" {
		target.IPAM.Strategy = source.IPAM.Strategy
	}
	if source.IPAM.GCInterval != "" {
		target.IPAM.GCInterval = source.IPAM.GCInterval
	}

	// DNS configuration
	if source.DNS.MagicDNS.Enabled {
		target.DNS.MagicDNS.Enabled = source.DNS.MagicDNS.Enabled
	}
	if len(source.DNS.MagicDNS.Nameservers) > 0 {
		target.DNS.MagicDNS.Nameservers = source.DNS.MagicDNS.Nameservers
	}
	if len(source.DNS.MagicDNS.SearchDomains) > 0 {
		target.DNS.MagicDNS.SearchDomains = source.DNS.MagicDNS.SearchDomains
	}
	if len(source.DNS.MagicDNS.Options) > 0 {
		target.DNS.MagicDNS.Options = source.DNS.MagicDNS.Options
	}

	// Monitoring configuration
	if source.Monitoring.Enabled {
		target.Monitoring.Enabled = source.Monitoring.Enabled
	}
	if source.Monitoring.Port > 0 {
		target.Monitoring.Port = source.Monitoring.Port
	}
	if source.Monitoring.Path != "" {
		target.Monitoring.Path = source.Monitoring.Path
	}

	// Logging configuration
	if source.Daemon.LogLevel != "" {
		target.Daemon.LogLevel = source.Daemon.LogLevel
	}
}
