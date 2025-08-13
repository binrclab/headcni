package commands

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

type ConfigOptions struct {
	Namespace   string
	ReleaseName string
	Output      string
	Show        bool
	Validate    bool
}

func NewConfigCommand() *cobra.Command {
	opts := &ConfigOptions{}

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage HeadCNI configuration",
		Long: `Manage HeadCNI configuration in your Kubernetes cluster.

This command allows you to:
- Show current configuration
- Validate configuration
- Export configuration

Examples:
  # Show current configuration
  headcni config --show

  # Validate configuration
  headcni config --validate

  # Export configuration as JSON
  headcni config --show --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfig(opts)
		},
	}

	cmd.Flags().StringVar(&opts.Namespace, "namespace", "kube-system", "Kubernetes namespace")
	cmd.Flags().StringVar(&opts.ReleaseName, "release-name", "headcni", "Helm release name")
	cmd.Flags().StringVar(&opts.Output, "output", "table", "Output format (table, json, yaml)")
	cmd.Flags().BoolVar(&opts.Show, "show", false, "Show current configuration")
	cmd.Flags().BoolVar(&opts.Validate, "validate", false, "Validate configuration")

	return cmd
}

func runConfig(opts *ConfigOptions) error {
	// 显示 ASCII logo
	showLogo()
	
	fmt.Printf("⚙️  HeadCNI Configuration Management\n")
	fmt.Printf("Namespace: %s\n", opts.Namespace)
	fmt.Printf("Release Name: %s\n\n", opts.ReleaseName)

	// 检查集群连接
	if err := checkClusterConnection(); err != nil {
		return fmt.Errorf("cluster connection failed: %v", err)
	}

	// 检查是否已安装
	if err := checkInstallation(&UninstallOptions{Namespace: opts.Namespace, ReleaseName: opts.ReleaseName}); err != nil {
		return fmt.Errorf("HeadCNI is not installed: %v", err)
	}

	if opts.Show {
		if err := showConfig(opts); err != nil {
			return fmt.Errorf("failed to show config: %v", err)
		}
	}

	if opts.Validate {
		if err := validateConfig(opts); err != nil {
			return fmt.Errorf("config validation failed: %v", err)
		}
	}

	// 如果没有指定操作，默认显示配置
	if !opts.Show && !opts.Validate {
		if err := showConfig(opts); err != nil {
			return fmt.Errorf("failed to show config: %v", err)
		}
	}

	return nil
}

func showConfig(opts *ConfigOptions) error {
	fmt.Printf("📋 Current Configuration:\n")

	// 获取 ConfigMap
	cmd := exec.Command("kubectl", "get", "configmap", opts.ReleaseName+"-config", "-n", opts.Namespace, "-o", "json")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get configmap: %v", err)
	}

	var configMap map[string]interface{}
	if err := json.Unmarshal(output, &configMap); err != nil {
		return fmt.Errorf("failed to parse configmap: %v", err)
	}

	data := configMap["data"].(map[string]interface{})

	switch opts.Output {
	case "json":
		// 输出 JSON 格式
		jsonOutput, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal config to JSON: %v", err)
		}
		fmt.Printf("%s\n", string(jsonOutput))

	case "yaml":
		// 输出 YAML 格式
		fmt.Printf("data:\n")
		for key, value := range data {
			fmt.Printf("  %s: |\n", key)
			// 简单的 YAML 格式化
			lines := strings.Split(value.(string), "\n")
			for _, line := range lines {
				fmt.Printf("    %s\n", line)
			}
		}

	default:
		// 默认表格格式
		fmt.Printf("   ConfigMap: %s-config\n", opts.ReleaseName)
		fmt.Printf("   Namespace: %s\n", opts.Namespace)
		fmt.Printf("   Files:\n")
		for key := range data {
			fmt.Printf("     - %s\n", key)
		}

		// 显示主要的 conflist 配置
		if conflist, ok := data["10-headcni.conflist"]; ok {
			fmt.Printf("\n   Main CNI Configuration:\n")
			fmt.Printf("   %s\n", conflist.(string))
		}
	}

	return nil
}

func validateConfig(opts *ConfigOptions) error {
	fmt.Printf("🔍 Validating Configuration...\n")

	// 获取 ConfigMap
	cmd := exec.Command("kubectl", "get", "configmap", opts.ReleaseName+"-config", "-n", opts.Namespace, "-o", "json")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get configmap: %v", err)
	}

	var configMap map[string]interface{}
	if err := json.Unmarshal(output, &configMap); err != nil {
		return fmt.Errorf("failed to parse configmap: %v", err)
	}

	data := configMap["data"].(map[string]interface{})

	// 验证必需的配置文件
	requiredFiles := []string{"10-headcni.conflist"}
	for _, file := range requiredFiles {
		if _, ok := data[file]; !ok {
			return fmt.Errorf("required config file %s not found", file)
		}
		fmt.Printf("   ✅ %s found\n", file)
	}

	// 验证 conflist 格式
	if conflist, ok := data["10-headcni.conflist"]; ok {
		var cniConfig map[string]interface{}
		if err := json.Unmarshal([]byte(conflist.(string)), &cniConfig); err != nil {
			return fmt.Errorf("invalid CNI configuration format: %v", err)
		}

		// 检查必需字段
		requiredFields := []string{"cniVersion", "name", "type", "ipam"}
		for _, field := range requiredFields {
			if _, ok := cniConfig[field]; !ok {
				return fmt.Errorf("missing required field: %s", field)
			}
		}

		// 检查 IPAM 配置
		if ipam, ok := cniConfig["ipam"].(map[string]interface{}); ok {
			if ipamType, ok := ipam["type"].(string); ok {
				if ipamType != "host-local" && ipamType != "headcni-ipam" {
					return fmt.Errorf("unsupported IPAM type: %s", ipamType)
				}
				fmt.Printf("   ✅ IPAM type: %s\n", ipamType)
			}
		}

		fmt.Printf("   ✅ CNI configuration format is valid\n")
	}

	// 检查 DaemonSet 配置
	dsCmd := exec.Command("kubectl", "get", "daemonset", opts.ReleaseName, "-n", opts.Namespace, "-o", "json")
	dsOutput, err := dsCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get daemonset: %v", err)
	}

	var daemonSet map[string]interface{}
	if err := json.Unmarshal(dsOutput, &daemonSet); err != nil {
		return fmt.Errorf("failed to parse daemonset: %v", err)
	}

	// 检查 DaemonSet 状态
	status := daemonSet["status"].(map[string]interface{})
	desired := int(status["desiredNumberScheduled"].(float64))
	ready := int(status["numberReady"].(float64))

	if ready != desired {
		return fmt.Errorf("daemonset not ready: %d/%d pods ready", ready, desired)
	}

	fmt.Printf("   ✅ DaemonSet is ready (%d/%d pods)\n", ready, desired)
	fmt.Printf("\n✅ Configuration validation passed!\n")

	return nil
} 