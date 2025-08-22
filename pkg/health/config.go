package health

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// ConfigFile 配置文件结构
type ConfigFile struct {
	HealthChecker *Config `json:"healthChecker"`
}

// LoadConfigFromFile 从文件加载配置
func LoadConfigFromFile(filepath string) (*Config, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}

	var configFile ConfigFile
	if err := json.Unmarshal(data, &configFile); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %v", err)
	}

	if configFile.HealthChecker == nil {
		configFile.HealthChecker = DefaultConfig()
	}

	return configFile.HealthChecker, nil
}

// SaveConfigToFile 保存配置到文件
func SaveConfigToFile(config *Config, filepath string) error {
	configFile := &ConfigFile{
		HealthChecker: config,
	}

	data, err := json.MarshalIndent(configFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %v", err)
	}

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %v", err)
	}

	return nil
}

// ValidateConfig 验证配置
func ValidateConfig(config *Config) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	if config.HealthCheckInterval < time.Second {
		return fmt.Errorf("health check interval must be at least 1 second")
	}

	if config.HealthCheckTimeout < time.Second {
		return fmt.Errorf("health check timeout must be at least 1 second")
	}

	if config.ReadinessTimeout < time.Second {
		return fmt.Errorf("readiness timeout must be at least 1 second")
	}

	if config.LivenessTimeout < time.Second {
		return fmt.Errorf("liveness timeout must be at least 1 second")
	}

	if config.MaxConsecutiveFailures < 1 {
		return fmt.Errorf("max consecutive failures must be at least 1")
	}

	if config.RecoveryTimeout < time.Second*10 {
		return fmt.Errorf("recovery timeout must be at least 10 seconds")
	}

	if config.TailscaleRestartTimeout < time.Second*5 {
		return fmt.Errorf("tailscale restart timeout must be at least 5 seconds")
	}

	return nil
}

// MergeConfig 合并配置
func MergeConfig(base, override *Config) *Config {
	if base == nil {
		return override
	}
	if override == nil {
		return base
	}

	merged := *base

	if override.Port != "" {
		merged.Port = override.Port
	}
	if override.HealthCheckInterval != 0 {
		merged.HealthCheckInterval = override.HealthCheckInterval
	}
	if override.HealthCheckTimeout != 0 {
		merged.HealthCheckTimeout = override.HealthCheckTimeout
	}
	if override.ReadinessTimeout != 0 {
		merged.ReadinessTimeout = override.ReadinessTimeout
	}
	if override.LivenessTimeout != 0 {
		merged.LivenessTimeout = override.LivenessTimeout
	}
	if override.MaxConsecutiveFailures != 0 {
		merged.MaxConsecutiveFailures = override.MaxConsecutiveFailures
	}
	if override.RecoveryTimeout != 0 {
		merged.RecoveryTimeout = override.RecoveryTimeout
	}
	if override.TailscaleRestartTimeout != 0 {
		merged.TailscaleRestartTimeout = override.TailscaleRestartTimeout
	}
	merged.EnableMetrics = override.EnableMetrics

	return &merged
}
