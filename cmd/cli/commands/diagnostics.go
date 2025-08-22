package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type DiagnosticsOptions struct {
	Namespace   string
	ReleaseName string
	OutputDir   string
	IncludeLogs bool
	IncludeYAML bool
	Verbose     bool
}

type DiagnosticInfo struct {
	Timestamp string                 `json:"timestamp"`
	Version   string                 `json:"version"`
	Cluster   ClusterInfo            `json:"cluster"`
	HeadCNI   HeadCNIInfo            `json:"headcni"`
	Tailscale TailscaleInfo          `json:"tailscale"`
	Network   NetworkInfo            `json:"network"`
	Resources map[string]interface{} `json:"resources"`
	Logs      map[string]string      `json:"logs,omitempty"`
}

type ClusterInfo struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	NodeCount    int    `json:"node_count"`
	PodCount     int    `json:"pod_count"`
	ServiceCount int    `json:"service_count"`
}

type HeadCNIInfo struct {
	Installed bool     `json:"installed"`
	Version   string   `json:"version"`
	Pods      []string `json:"pods"`
	Status    string   `json:"status"`
	Config    string   `json:"config"`
}

type TailscaleInfo struct {
	Connected bool   `json:"connected"`
	IP        string `json:"ip"`
	Status    string `json:"status"`
}

type NetworkInfo struct {
	CNIInstalled bool     `json:"cni_installed"`
	CNIVersion   string   `json:"cni_version"`
	Interfaces   []string `json:"interfaces"`
	Routes       []string `json:"routes"`
}

func NewDiagnosticsCommand() *cobra.Command {
	opts := &DiagnosticsOptions{}

	cmd := &cobra.Command{
		Use:   "diagnostics",
		Short: "Collect diagnostic information for troubleshooting",
		Long: `Collect comprehensive diagnostic information for troubleshooting HeadCNI issues.

This command will gather:
- Cluster information
- HeadCNI deployment status
- Tailscale connectivity
- Network configuration
- Resource manifests
- Logs (optional)

Examples:
  # Basic diagnostics
  headcni diagnostics

  # Include logs and YAML manifests
  headcni diagnostics --include-logs --include-yaml

  # Save to specific directory
  headcni diagnostics --output-dir ./diagnostics

  # Verbose output
  headcni diagnostics --verbose`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiagnostics(opts)
		},
	}

	cmd.Flags().StringVar(&opts.Namespace, "namespace", "kube-system", "Kubernetes namespace")
	cmd.Flags().StringVar(&opts.ReleaseName, "release-name", "headcni", "Helm release name")
	cmd.Flags().StringVar(&opts.OutputDir, "output-dir", "", "Output directory for diagnostic files")
	cmd.Flags().BoolVar(&opts.IncludeLogs, "include-logs", false, "Include pod logs in diagnostics")
	cmd.Flags().BoolVar(&opts.IncludeYAML, "include-yaml", false, "Include YAML manifests in diagnostics")
	cmd.Flags().BoolVar(&opts.Verbose, "verbose", false, "Verbose output")

	return cmd
}

func runDiagnostics(opts *DiagnosticsOptions) error {
	// 显示 ASCII logo
	showLogo()

	fmt.Printf("🔍 Collecting HeadCNI diagnostics...\n")
	fmt.Printf("Namespace: %s\n", opts.Namespace)
	fmt.Printf("Release Name: %s\n", opts.ReleaseName)
	fmt.Printf("Include Logs: %v\n", opts.IncludeLogs)
	fmt.Printf("Include YAML: %v\n\n", opts.IncludeYAML)

	// 检查集群连接
	if err := checkClusterConnection(); err != nil {
		return fmt.Errorf("cluster connection failed: %v", err)
	}

	// 收集诊断信息
	diagnostics := &DiagnosticInfo{
		Timestamp: time.Now().Format(time.RFC3339),
		Version:   getVersion(),
		Resources: make(map[string]interface{}),
		Logs:      make(map[string]string),
	}

	// 收集集群信息
	fmt.Println("📊 Collecting cluster information...")
	clusterInfo, err := collectClusterInfo()
	if err != nil {
		fmt.Printf("⚠️  Warning: Failed to collect cluster info: %v\n", err)
	} else {
		diagnostics.Cluster = clusterInfo
	}

	// 收集HeadCNI信息
	fmt.Println("🔧 Collecting HeadCNI information...")
	headcniInfo, err := collectHeadCNIInfo(opts)
	if err != nil {
		fmt.Printf("⚠️  Warning: Failed to collect HeadCNI info: %v\n", err)
	} else {
		diagnostics.HeadCNI = headcniInfo
	}

	// 收集Tailscale信息
	fmt.Println("🔗 Collecting Tailscale information...")
	tailscaleInfo, err := collectTailscaleInfo()
	if err != nil {
		fmt.Printf("⚠️  Warning: Failed to collect Tailscale info: %v\n", err)
	} else {
		diagnostics.Tailscale = tailscaleInfo
	}

	// 收集网络信息
	fmt.Println("🌐 Collecting network information...")
	networkInfo, err := collectNetworkInfo()
	if err != nil {
		fmt.Printf("⚠️  Warning: Failed to collect network info: %v\n", err)
	} else {
		diagnostics.Network = networkInfo
	}

	// 收集资源清单
	if opts.IncludeYAML {
		fmt.Println("📄 Collecting resource manifests...")
		if err := collectResourceManifests(opts, diagnostics); err != nil {
			fmt.Printf("⚠️  Warning: Failed to collect resource manifests: %v\n", err)
		}
	}

	// 收集日志
	if opts.IncludeLogs {
		fmt.Println("📋 Collecting pod logs...")
		if err := collectPodLogs(opts, diagnostics); err != nil {
			fmt.Printf("⚠️  Warning: Failed to collect pod logs: %v\n", err)
		}
	}

	// 保存诊断信息
	if err := saveDiagnostics(diagnostics, opts); err != nil {
		return fmt.Errorf("failed to save diagnostics: %v", err)
	}

	fmt.Println("✅ Diagnostics collection completed!")
	return nil
}

func collectClusterInfo() (ClusterInfo, error) {
	info := ClusterInfo{}

	// 获取集群名称
	cmd := exec.Command("kubectl", "config", "current-context")
	if output, err := cmd.Output(); err == nil {
		info.Name = strings.TrimSpace(string(output))
	}

	// 获取集群版本
	cmd = exec.Command("kubectl", "version", "--short", "--client=false")
	if output, err := cmd.Output(); err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, "Server Version") {
				parts := strings.Split(line, " ")
				if len(parts) >= 3 {
					info.Version = parts[2]
				}
				break
			}
		}
	}

	// 获取节点数量
	cmd = exec.Command("kubectl", "get", "nodes", "--no-headers")
	if output, err := cmd.Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		info.NodeCount = len(lines)
	}

	// 获取Pod数量
	cmd = exec.Command("kubectl", "get", "pods", "--all-namespaces", "--no-headers")
	if output, err := cmd.Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		info.PodCount = len(lines)
	}

	// 获取Service数量
	cmd = exec.Command("kubectl", "get", "services", "--all-namespaces", "--no-headers")
	if output, err := cmd.Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		info.ServiceCount = len(lines)
	}

	return info, nil
}

func collectHeadCNIInfo(opts *DiagnosticsOptions) (HeadCNIInfo, error) {
	info := HeadCNIInfo{}

	// 检查DaemonSet是否存在
	cmd := exec.Command("kubectl", "get", "daemonset", opts.ReleaseName, "-n", opts.Namespace)
	if err := cmd.Run(); err == nil {
		info.Installed = true
	} else {
		info.Installed = false
		return info, nil
	}

	// 获取版本信息
	cmd = exec.Command("kubectl", "get", "daemonset", opts.ReleaseName,
		"-n", opts.Namespace, "-o", "jsonpath={.spec.template.spec.containers[0].image}")
	if output, err := cmd.Output(); err == nil {
		image := strings.TrimSpace(string(output))
		parts := strings.Split(image, ":")
		if len(parts) > 1 {
			info.Version = parts[len(parts)-1]
		}
	}

	// 获取Pod列表
	pods, err := getHeadCNIPods(opts.Namespace, opts.ReleaseName)
	if err == nil {
		for _, pod := range pods {
			info.Pods = append(info.Pods, pod.Name)
		}
	}

	// 获取状态
	cmd = exec.Command("kubectl", "get", "daemonset", opts.ReleaseName,
		"-n", opts.Namespace, "-o", "jsonpath={.status.conditions[0].type}")
	if output, err := cmd.Output(); err == nil {
		info.Status = strings.TrimSpace(string(output))
	}

	// 获取配置
	cmd = exec.Command("kubectl", "get", "configmap", fmt.Sprintf("%s-config", opts.ReleaseName),
		"-n", opts.Namespace, "-o", "jsonpath={.data.config}")
	if output, err := cmd.Output(); err == nil {
		info.Config = strings.TrimSpace(string(output))
	}

	return info, nil
}

func collectTailscaleInfo() (TailscaleInfo, error) {
	info := TailscaleInfo{}

	// 检查Tailscale状态
	cmd := exec.Command("tailscale", "status")
	if err := cmd.Run(); err == nil {
		info.Connected = true
		info.Status = "Connected"
	} else {
		info.Connected = false
		info.Status = "Disconnected"
		return info, nil
	}

	// 获取Tailscale IP
	cmd = exec.Command("tailscale", "ip")
	if output, err := cmd.Output(); err == nil {
		info.IP = strings.TrimSpace(string(output))
	}

	return info, nil
}

func collectNetworkInfo() (NetworkInfo, error) {
	info := NetworkInfo{}

	// 检查CNI插件
	cmd := exec.Command("ls", "/opt/cni/bin/headcni")
	if err := cmd.Run(); err == nil {
		info.CNIInstalled = true
	} else {
		info.CNIInstalled = false
	}

	// 获取CNI版本
	cmd = exec.Command("/opt/cni/bin/headcni", "--version")
	if output, err := cmd.Output(); err == nil {
		info.CNIVersion = strings.TrimSpace(string(output))
	}

	// 获取网络接口
	cmd = exec.Command("ip", "link", "show")
	if output, err := cmd.Output(); err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, ":") {
				parts := strings.Split(line, ":")
				if len(parts) >= 2 {
					iface := strings.TrimSpace(parts[1])
					if iface != "" && !strings.Contains(iface, "lo") {
						info.Interfaces = append(info.Interfaces, iface)
					}
				}
			}
		}
	}

	// 获取路由表
	cmd = exec.Command("ip", "route", "show")
	if output, err := cmd.Output(); err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line != "" {
				info.Routes = append(info.Routes, line)
			}
		}
	}

	return info, nil
}

func collectResourceManifests(opts *DiagnosticsOptions, diagnostics *DiagnosticInfo) error {
	resources := []string{
		"daemonset",
		"configmap",
		"serviceaccount",
		"clusterrole",
		"clusterrolebinding",
	}

	for _, resource := range resources {
		cmd := exec.Command("kubectl", "get", resource,
			fmt.Sprintf("%s-%s", opts.ReleaseName, resource),
			"-n", opts.Namespace, "-o", "yaml")

		if output, err := cmd.Output(); err == nil {
			diagnostics.Resources[resource] = string(output)
		}
	}

	return nil
}

func collectPodLogs(opts *DiagnosticsOptions, diagnostics *DiagnosticInfo) error {
	pods, err := getHeadCNIPods(opts.Namespace, opts.ReleaseName)
	if err != nil {
		return err
	}

	for _, pod := range pods {
		cmd := exec.Command("kubectl", "logs", pod.Name, "-n", opts.Namespace, "--tail", "100")
		if output, err := cmd.Output(); err == nil {
			diagnostics.Logs[pod.Name] = string(output)
		}
	}

	return nil
}

func saveDiagnostics(diagnostics *DiagnosticInfo, opts *DiagnosticsOptions) error {
	// 确定输出目录
	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = fmt.Sprintf("headcni-diagnostics-%s", time.Now().Format("20060102-150405"))
	}

	// 创建输出目录
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %v", err)
	}

	// 保存JSON格式的诊断信息
	jsonFile := filepath.Join(outputDir, "diagnostics.json")
	jsonData, err := json.MarshalIndent(diagnostics, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal diagnostics to JSON: %v", err)
	}

	if err := os.WriteFile(jsonFile, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write diagnostics file: %v", err)
	}

	// 生成摘要报告
	summaryFile := filepath.Join(outputDir, "summary.txt")
	summary := generateSummary(diagnostics)
	if err := os.WriteFile(summaryFile, []byte(summary), 0644); err != nil {
		return fmt.Errorf("failed to write summary file: %v", err)
	}

	fmt.Printf("📁 Diagnostics saved to: %s\n", outputDir)
	fmt.Printf("📄 JSON report: %s\n", jsonFile)
	fmt.Printf("📋 Summary: %s\n", summaryFile)

	return nil
}

func generateSummary(diagnostics *DiagnosticInfo) string {
	var summary strings.Builder

	summary.WriteString("HeadCNI Diagnostics Summary\n")
	summary.WriteString("==========================\n\n")

	summary.WriteString(fmt.Sprintf("Timestamp: %s\n", diagnostics.Timestamp))
	summary.WriteString(fmt.Sprintf("CLI Version: %s\n\n", diagnostics.Version))

	// 集群信息
	summary.WriteString("Cluster Information:\n")
	summary.WriteString(fmt.Sprintf("  Name: %s\n", diagnostics.Cluster.Name))
	summary.WriteString(fmt.Sprintf("  Version: %s\n", diagnostics.Cluster.Version))
	summary.WriteString(fmt.Sprintf("  Nodes: %d\n", diagnostics.Cluster.NodeCount))
	summary.WriteString(fmt.Sprintf("  Pods: %d\n", diagnostics.Cluster.PodCount))
	summary.WriteString(fmt.Sprintf("  Services: %d\n\n", diagnostics.Cluster.ServiceCount))

	// HeadCNI信息
	summary.WriteString("HeadCNI Information:\n")
	summary.WriteString(fmt.Sprintf("  Installed: %v\n", diagnostics.HeadCNI.Installed))
	if diagnostics.HeadCNI.Installed {
		summary.WriteString(fmt.Sprintf("  Version: %s\n", diagnostics.HeadCNI.Version))
		summary.WriteString(fmt.Sprintf("  Status: %s\n", diagnostics.HeadCNI.Status))
		summary.WriteString(fmt.Sprintf("  Pods: %d\n", len(diagnostics.HeadCNI.Pods)))
	}
	summary.WriteString("\n")

	// Tailscale信息
	summary.WriteString("Tailscale Information:\n")
	summary.WriteString(fmt.Sprintf("  Connected: %v\n", diagnostics.Tailscale.Connected))
	if diagnostics.Tailscale.Connected {
		summary.WriteString(fmt.Sprintf("  IP: %s\n", diagnostics.Tailscale.IP))
		summary.WriteString(fmt.Sprintf("  Status: %s\n", diagnostics.Tailscale.Status))
	}
	summary.WriteString("\n")

	// 网络信息
	summary.WriteString("Network Information:\n")
	summary.WriteString(fmt.Sprintf("  CNI Installed: %v\n", diagnostics.Network.CNIInstalled))
	summary.WriteString(fmt.Sprintf("  CNI Version: %s\n", diagnostics.Network.CNIVersion))
	summary.WriteString(fmt.Sprintf("  Interfaces: %d\n", len(diagnostics.Network.Interfaces)))
	summary.WriteString(fmt.Sprintf("  Routes: %d\n", len(diagnostics.Network.Routes)))

	return summary.String()
}
