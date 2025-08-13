package commands

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

type ConfigOptions struct {
	Namespace   string
	ReleaseName string
	Output      string
	Show        bool
	Validate    bool
	Action      string
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
			return runConfig(cmd, args)
		},
	}

	cmd.Flags().StringVar(&opts.Namespace, "namespace", "kube-system", "Kubernetes namespace")
	cmd.Flags().StringVar(&opts.ReleaseName, "release-name", "headcni", "Helm release name")
	cmd.Flags().StringVar(&opts.Output, "output", "table", "Output format (table, json, yaml)")
	cmd.Flags().BoolVar(&opts.Show, "show", false, "Show current configuration")
	cmd.Flags().BoolVar(&opts.Validate, "validate", false, "Validate configuration")

	return cmd
}

func runConfig(cmd *cobra.Command, args []string) error {
	showLogo()

	opts := &ConfigOptions{
		Namespace:   "kube-system",
		ReleaseName: "headcni",
	}

	// 解析命令行参数
	if len(args) > 0 {
		opts.Action = args[0]
	}

	switch opts.Action {
	case "show":
		return showConfig(opts)
	case "validate":
		return validateConfig(opts)
	case "export":
		return exportConfig(opts)
	case "explain":
		return explainConfig(opts)
	default:
		return showConfigHelp()
	}
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

// exportConfig 导出配置
func exportConfig(opts *ConfigOptions) error {
	pterm.Info.Println("📤 Exporting configuration...")

	// 获取配置并导出
	cmd := exec.Command("kubectl", "get", "configmap", opts.ReleaseName+"-config", "-n", opts.Namespace, "-o", "json")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to export config: %v", err)
	}

	fmt.Println(string(output))
	return nil
}

// showConfigHelp 显示配置帮助
func showConfigHelp() error {
	pterm.DefaultSection.Println("HeadCNI Configuration Management")

	help := `Available commands:
  show     - Show current configuration
  validate - Validate configuration
  export   - Export configuration as JSON
  explain  - Explain configuration parameters

Examples:
  headcni config show
  headcni config validate
  headcni config export
  headcni config explain`

	pterm.DefaultBox.Println(help)
	return nil
}

// explainConfig 解释配置参数
func explainConfig(opts *ConfigOptions) error {
	showConfigExplanation()

	// 显示简化配置示例
	pterm.DefaultSection.Println("Simplified Configuration Example")
	pterm.DefaultBox.WithTitle("10-headcni-ipam.conflist").Println(generateSimplifiedConfig())

	// 显示环境变量配置
	pterm.DefaultSection.Println("Environment Variables Configuration")
	envConfig := `# 敏感配置通过环境变量设置
HEADSCALE_URL=https://headscale.company.com
TAILSCALE_SOCKET=/var/run/tailscale/tailscaled.sock

# 网络配置
POD_CIDR=10.244.0.0/24
SERVICE_CIDR=10.96.0.0/16
MTU=1420

# IPAM 配置
IPAM_TYPE=headcni-ipam
ALLOCATION_STRATEGY=sequential`

	pterm.DefaultBox.WithTitle("headcni.env").Println(envConfig)

	return nil
}
