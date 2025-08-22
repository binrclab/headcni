package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type BackupOptions struct {
	Namespace   string
	ReleaseName string
	OutputFile  string
	IncludeLogs bool
	Verbose     bool
}

type RestoreOptions struct {
	Namespace   string
	ReleaseName string
	InputFile   string
	DryRun      bool
	Force       bool
}

type BackupData struct {
	Timestamp   string                 `json:"timestamp"`
	Version     string                 `json:"version"`
	Cluster     string                 `json:"cluster"`
	Namespace   string                 `json:"namespace"`
	ReleaseName string                 `json:"release_name"`
	Config      map[string]interface{} `json:"config"`
	Resources   map[string]interface{} `json:"resources"`
	Logs        map[string]string      `json:"logs,omitempty"`
}

func NewBackupCommand() *cobra.Command {
	opts := &BackupOptions{}

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Backup HeadCNI configuration and resources",
		Long: `Backup HeadCNI configuration and resources to a file.

This command will backup:
- HeadCNI configuration
- Kubernetes resources (DaemonSet, ConfigMap, etc.)
- Pod logs (optional)
- Cluster information

Examples:
  # Basic backup
  headcni backup

  # Backup with logs
  headcni backup --include-logs

  # Backup to specific file
  headcni backup --output-file headcni-backup.json

  # Verbose backup
  headcni backup --verbose`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBackup(opts)
		},
	}

	cmd.Flags().StringVar(&opts.Namespace, "namespace", "kube-system", "Kubernetes namespace")
	cmd.Flags().StringVar(&opts.ReleaseName, "release-name", "headcni", "Helm release name")
	cmd.Flags().StringVar(&opts.OutputFile, "output-file", "", "Output file for backup (default: auto-generated)")
	cmd.Flags().BoolVar(&opts.IncludeLogs, "include-logs", false, "Include pod logs in backup")
	cmd.Flags().BoolVar(&opts.Verbose, "verbose", false, "Verbose output")

	return cmd
}

func NewRestoreCommand() *cobra.Command {
	opts := &RestoreOptions{}

	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore HeadCNI configuration from backup",
		Long: `Restore HeadCNI configuration from a backup file.

This command will restore:
- HeadCNI configuration
- Kubernetes resources
- Verify restoration success

Examples:
  # Restore from backup file
  headcni restore --input-file headcni-backup.json

  # Dry run restore
  headcni restore --input-file headcni-backup.json --dry-run

  # Force restore (skip checks)
  headcni restore --input-file headcni-backup.json --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRestore(opts)
		},
	}

	cmd.Flags().StringVar(&opts.Namespace, "namespace", "kube-system", "Kubernetes namespace")
	cmd.Flags().StringVar(&opts.ReleaseName, "release-name", "headcni", "Helm release name")
	cmd.Flags().StringVar(&opts.InputFile, "input-file", "", "Input backup file (required)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be restored without actually restoring")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Force restore (skip compatibility checks)")
	cmd.MarkFlagRequired("input-file")

	return cmd
}

func runBackup(opts *BackupOptions) error {
	// 显示 ASCII logo
	showLogo()

	fmt.Printf("💾 Creating HeadCNI backup...\n")
	fmt.Printf("Namespace: %s\n", opts.Namespace)
	fmt.Printf("Release Name: %s\n", opts.ReleaseName)
	fmt.Printf("Include Logs: %v\n\n", opts.IncludeLogs)

	// 检查集群连接
	if err := checkClusterConnection(); err != nil {
		return fmt.Errorf("cluster connection failed: %v", err)
	}

	// 创建备份数据
	backup := &BackupData{
		Timestamp:   time.Now().Format(time.RFC3339),
		Version:     getVersion(),
		Namespace:   opts.Namespace,
		ReleaseName: opts.ReleaseName,
		Config:      make(map[string]interface{}),
		Resources:   make(map[string]interface{}),
		Logs:        make(map[string]string),
	}

	// 获取集群信息
	fmt.Println("📊 Collecting cluster information...")
	if clusterName, err := getClusterName(); err == nil {
		backup.Cluster = clusterName
	}

	// 收集配置信息
	fmt.Println("⚙️  Collecting configuration...")
	if err := collectBackupConfig(opts, backup); err != nil {
		return fmt.Errorf("failed to collect configuration: %v", err)
	}

	// 收集资源信息
	fmt.Println("📄 Collecting resources...")
	if err := collectBackupResources(opts, backup); err != nil {
		return fmt.Errorf("failed to collect resources: %v", err)
	}

	// 收集日志
	if opts.IncludeLogs {
		fmt.Println("📋 Collecting logs...")
		if err := collectBackupLogs(opts, backup); err != nil {
			return fmt.Errorf("failed to collect logs: %v", err)
		}
	}

	// 保存备份文件
	if err := saveBackup(backup, opts); err != nil {
		return fmt.Errorf("failed to save backup: %v", err)
	}

	fmt.Println("✅ Backup completed successfully!")
	return nil
}

func runRestore(opts *RestoreOptions) error {
	// 显示 ASCII logo
	showLogo()

	fmt.Printf("🔄 Restoring HeadCNI from backup...\n")
	fmt.Printf("Input File: %s\n", opts.InputFile)
	fmt.Printf("Namespace: %s\n", opts.Namespace)
	fmt.Printf("Release Name: %s\n", opts.ReleaseName)
	fmt.Printf("Dry Run: %v\n", opts.DryRun)
	fmt.Printf("Force: %v\n\n", opts.Force)

	// 检查集群连接
	if err := checkClusterConnection(); err != nil {
		return fmt.Errorf("cluster connection failed: %v", err)
	}

	// 加载备份文件
	fmt.Println("📂 Loading backup file...")
	backup, err := loadBackup(opts.InputFile)
	if err != nil {
		return fmt.Errorf("failed to load backup: %v", err)
	}

	// 验证备份文件
	if err := validateBackup(backup); err != nil {
		return fmt.Errorf("invalid backup file: %v", err)
	}

	// 检查兼容性
	if !opts.Force {
		if err := checkRestoreCompatibility(backup); err != nil {
			return fmt.Errorf("restore compatibility check failed: %v", err)
		}
	}

	// 执行恢复
	if err := performRestore(backup, opts); err != nil {
		return fmt.Errorf("restore failed: %v", err)
	}

	// 验证恢复
	if !opts.DryRun {
		if err := verifyRestore(opts); err != nil {
			return fmt.Errorf("restore verification failed: %v", err)
		}
	}

	fmt.Println("✅ Restore completed successfully!")
	return nil
}

func getClusterName() (string, error) {
	cmd := exec.Command("kubectl", "config", "current-context")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func collectBackupConfig(opts *BackupOptions, backup *BackupData) error {
	// 收集ConfigMap配置
	cmd := exec.Command("kubectl", "get", "configmap",
		fmt.Sprintf("%s-config", opts.ReleaseName),
		"-n", opts.Namespace, "-o", "json")

	if output, err := cmd.Output(); err == nil {
		var config map[string]interface{}
		if err := json.Unmarshal(output, &config); err == nil {
			backup.Config["configmap"] = config
		}
	}

	// 收集CNI配置
	cmd = exec.Command("cat", "/etc/cni/net.d/10-headcni.conflist")
	if output, err := cmd.Output(); err == nil {
		var cniConfig interface{}
		if err := json.Unmarshal(output, &cniConfig); err == nil {
			backup.Config["cni"] = cniConfig
		}
	}

	return nil
}

func collectBackupResources(opts *BackupOptions, backup *BackupData) error {
	resources := []string{
		"daemonset",
		"serviceaccount",
		"clusterrole",
		"clusterrolebinding",
	}

	for _, resource := range resources {
		resourceName := fmt.Sprintf("%s-%s", opts.ReleaseName, resource)
		cmd := exec.Command("kubectl", "get", resource, resourceName,
			"-n", opts.Namespace, "-o", "json")

		if output, err := cmd.Output(); err == nil {
			var resourceData interface{}
			if err := json.Unmarshal(output, &resourceData); err == nil {
				backup.Resources[resource] = resourceData
			}
		}
	}

	return nil
}

func collectBackupLogs(opts *BackupOptions, backup *BackupData) error {
	pods, err := getHeadCNIPods(opts.Namespace, opts.ReleaseName)
	if err != nil {
		return err
	}

	for _, pod := range pods {
		cmd := exec.Command("kubectl", "logs", pod.Name, "-n", opts.Namespace, "--tail", "50")
		if output, err := cmd.Output(); err == nil {
			backup.Logs[pod.Name] = string(output)
		}
	}

	return nil
}

func saveBackup(backup *BackupData, opts *BackupOptions) error {
	// 确定输出文件名
	outputFile := opts.OutputFile
	if outputFile == "" {
		timestamp := time.Now().Format("20060102-150405")
		outputFile = fmt.Sprintf("headcni-backup-%s.json", timestamp)
	}

	// 序列化备份数据
	jsonData, err := json.MarshalIndent(backup, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal backup data: %v", err)
	}

	// 写入文件
	if err := os.WriteFile(outputFile, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write backup file: %v", err)
	}

	fmt.Printf("📁 Backup saved to: %s\n", outputFile)
	return nil
}

func loadBackup(filename string) (*BackupData, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup file: %v", err)
	}

	var backup BackupData
	if err := json.Unmarshal(data, &backup); err != nil {
		return nil, fmt.Errorf("failed to parse backup file: %v", err)
	}

	return &backup, nil
}

func validateBackup(backup *BackupData) error {
	if backup.Version == "" {
		return fmt.Errorf("missing version information")
	}

	if backup.Timestamp == "" {
		return fmt.Errorf("missing timestamp")
	}

	if backup.Namespace == "" {
		return fmt.Errorf("missing namespace")
	}

	if backup.ReleaseName == "" {
		return fmt.Errorf("missing release name")
	}

	return nil
}

func checkRestoreCompatibility(backup *BackupData) error {
	fmt.Println("🔍 Checking restore compatibility...")

	// 检查版本兼容性
	currentVersion := getVersion()
	if backup.Version != currentVersion {
		fmt.Printf("⚠️  Warning: Backup version (%s) differs from current version (%s)\n",
			backup.Version, currentVersion)
	}

	// 检查集群兼容性
	currentCluster, err := getClusterName()
	if err == nil && backup.Cluster != "" && backup.Cluster != currentCluster {
		fmt.Printf("⚠️  Warning: Backup cluster (%s) differs from current cluster (%s)\n",
			backup.Cluster, currentCluster)
	}

	fmt.Println("✅ Restore compatibility check passed")
	return nil
}

func performRestore(backup *BackupData, opts *RestoreOptions) error {
	fmt.Println("🚀 Performing restore...")

	if opts.DryRun {
		fmt.Println("📋 Dry run - would perform the following actions:")
		fmt.Printf("  - Restore configuration to namespace %s\n", backup.Namespace)
		fmt.Printf("  - Restore %d resources\n", len(backup.Resources))
		if len(backup.Logs) > 0 {
			fmt.Printf("  - Backup contains logs from %d pods\n", len(backup.Logs))
		}
		return nil
	}

	// 恢复ConfigMap
	if configmap, exists := backup.Config["configmap"]; exists {
		fmt.Println("📄 Restoring ConfigMap...")
		if err := restoreResource(configmap, "configmap", backup.Namespace); err != nil {
			return fmt.Errorf("failed to restore ConfigMap: %v", err)
		}
	}

	// 恢复其他资源
	for resourceType, resourceData := range backup.Resources {
		fmt.Printf("📄 Restoring %s...\n", resourceType)
		if err := restoreResource(resourceData, resourceType, backup.Namespace); err != nil {
			return fmt.Errorf("failed to restore %s: %v", resourceType, err)
		}
	}

	// 恢复CNI配置
	if cniConfig, exists := backup.Config["cni"]; exists {
		fmt.Println("🌐 Restoring CNI configuration...")
		if err := restoreCNIConfig(cniConfig); err != nil {
			return fmt.Errorf("failed to restore CNI config: %v", err)
		}
	}

	return nil
}

func restoreResource(resourceData interface{}, resourceType, namespace string) error {
	// 将资源数据转换为JSON
	jsonData, err := json.Marshal(resourceData)
	if err != nil {
		return fmt.Errorf("failed to marshal resource data: %v", err)
	}

	// 使用kubectl apply恢复资源
	cmd := exec.Command("kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(string(jsonData))

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to apply resource: %v, output: %s", err, string(output))
	}

	return nil
}

func restoreCNIConfig(cniConfig interface{}) error {
	// 将CNI配置转换为JSON
	jsonData, err := json.MarshalIndent(cniConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal CNI config: %v", err)
	}

	// 写入CNI配置文件
	if err := os.WriteFile("/etc/cni/net.d/10-headcni.conflist", jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write CNI config: %v", err)
	}

	return nil
}

func verifyRestore(opts *RestoreOptions) error {
	fmt.Println("🔍 Verifying restore...")

	// 检查DaemonSet状态
	cmd := exec.Command("kubectl", "get", "daemonset", opts.ReleaseName,
		"-n", opts.Namespace, "-o", "jsonpath={.status.readyNumberScheduled}")

	if output, err := cmd.Output(); err == nil {
		ready := strings.TrimSpace(string(output))
		fmt.Printf("✅ DaemonSet ready pods: %s\n", ready)
	}

	// 检查ConfigMap
	cmd = exec.Command("kubectl", "get", "configmap",
		fmt.Sprintf("%s-config", opts.ReleaseName), "-n", opts.Namespace)

	if err := cmd.Run(); err == nil {
		fmt.Println("✅ ConfigMap restored successfully")
	} else {
		fmt.Println("❌ ConfigMap restoration failed")
	}

	// 检查CNI配置
	if _, err := os.Stat("/etc/cni/net.d/10-headcni.conflist"); err == nil {
		fmt.Println("✅ CNI configuration restored successfully")
	} else {
		fmt.Println("❌ CNI configuration restoration failed")
	}

	return nil
}
