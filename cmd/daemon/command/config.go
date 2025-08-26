package command

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/binrclab/headcni/cmd/daemon/config"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func init() {
	configCmd.AddCommand(
		configValidateCmd,
		configShowCmd,
	)
	rootCmd.AddCommand(configCmd)
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configuration management",
	Long:  "Manage HeadCNI daemon configuration",
}

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate configuration file",
	Long:  "Validate the configuration file syntax and settings",
	RunE: func(cmd *cobra.Command, args []string) error {
		configFile, _ := cmd.Flags().GetString("config")
		if configFile == "" {
			return errors.New("config file path is required")
		}

		// 检查文件是否存在
		if _, err := os.Stat(configFile); os.IsNotExist(err) {
			return errors.Errorf("configuration file %s does not exist", configFile)
		}

		// 验证配置文件
		if err := validateConfigFile(configFile); err != nil {
			return errors.Wrapf(err, "configuration file %s is invalid", configFile)
		}

		cmd.Printf("Configuration file %s is valid\n", configFile)
		return nil
	},
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show current configuration",
	Long:  "Display the current configuration with all overrides applied",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load and display configuration
		cfg, err := config.LoadConfigWithPriority(cmd)
		if err != nil {
			return errors.Wrap(err, "failed to load config")
		}

		cmd.Println("Current configuration:")
		cmd.Printf("  - Tailscale URL: %s\n", cfg.Tailscale.URL)
		cmd.Printf("  - Tailscale Socket: %s\n", cfg.Tailscale.Socket.Path)
		cmd.Printf("  - Tailscale MTU: %d\n", cfg.Tailscale.MTU)
		cmd.Printf("  - Pod CIDR: %s\n", cfg.Network.PodCIDR.Base)
		cmd.Printf("  - Service CIDR: %s\n", cfg.Network.ServiceCIDR)
		cmd.Printf("  - Log Level: %s\n", cfg.Daemon.LogLevel)

		// 显示更多配置信息
		cmd.Printf("  - Headscale URL: %s\n", cfg.Headscale.URL)
		cmd.Printf("  - Network MTU: %d\n", cfg.Network.MTU)
		cmd.Printf("  - Enable IPv6: %t\n", cfg.Network.EnableIPv6)
		cmd.Printf("  - Enable Network Policy: %t\n", cfg.Network.EnableNetworkPolicy)
		cmd.Printf("  - IPAM Type: %s\n", cfg.IPAM.Type)
		cmd.Printf("  - IPAM Strategy: %s\n", cfg.IPAM.Strategy)

		return nil
	},
}

// validateConfigFile 验证配置文件
func validateConfigFile(configFile string) error {
	// 检查文件扩展名
	ext := filepath.Ext(configFile)
	if ext != ".yaml" && ext != ".yml" {
		return fmt.Errorf("unsupported file extension: %s (only .yaml and .yml are supported)", ext)
	}

	// 读取文件内容
	data, err := os.ReadFile(configFile)
	if err != nil {
		return errors.Wrap(err, "failed to read configuration file")
	}

	// 解析 YAML
	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return errors.Wrap(err, "failed to parse YAML configuration")
	}

	// 验证配置字段
	if err := validateConfigFields(&cfg); err != nil {
		return errors.Wrap(err, "configuration validation failed")
	}

	return nil
}

// validateConfigFields 验证配置字段
func validateConfigFields(cfg *config.Config) error {
	// 验证 Tailscale 配置
	if cfg.Tailscale.URL == "" {
		return fmt.Errorf("tailscale URL is required")
	}

	if cfg.Tailscale.Socket.Path == "" {
		return fmt.Errorf("tailscale socket path is required")
	}

	if cfg.Tailscale.MTU <= 0 {
		return fmt.Errorf("tailscale MTU must be greater than 0")
	}

	// 验证网络配置
	if cfg.Network.PodCIDR.Base == "" {
		return fmt.Errorf("pod CIDR is required")
	}

	if cfg.Network.ServiceCIDR == "" {
		return fmt.Errorf("service CIDR is required")
	}

	if cfg.Network.MTU <= 0 {
		return fmt.Errorf("network MTU must be greater than 0")
	}

	// 验证 Headscale 配置
	if cfg.Headscale.URL == "" {
		return fmt.Errorf("headscale URL is required")
	}

	if cfg.Headscale.AuthKey == "" {
		return fmt.Errorf("headscale auth key is required")
	}

	// 验证 IPAM 配置
	if cfg.IPAM.Type == "" {
		return fmt.Errorf("IPAM type is required")
	}

	if cfg.IPAM.Strategy == "" {
		return fmt.Errorf("IPAM strategy is required")
	}

	return nil
}
