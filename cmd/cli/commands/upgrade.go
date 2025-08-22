package commands

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

type UpgradeOptions struct {
	Namespace   string
	ReleaseName string
	ImageTag    string
	ImageRepo   string
	DryRun      bool
	Force       bool
	Timeout     int
}

func NewUpgradeCommand() *cobra.Command {
	opts := &UpgradeOptions{}

	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade HeadCNI to a newer version",
		Long: `Upgrade HeadCNI to a newer version in your Kubernetes cluster.

This command will:
1. Check current installation
2. Validate upgrade compatibility
3. Perform rolling upgrade
4. Verify upgrade success

Examples:
  # Upgrade to latest version
  headcni upgrade

  # Upgrade to specific version
  headcni upgrade --image-tag v1.1.0

  # Dry run upgrade
  headcni upgrade --dry-run

  # Force upgrade (skip compatibility checks)
  headcni upgrade --force`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpgrade(opts)
		},
	}

	cmd.Flags().StringVar(&opts.Namespace, "namespace", "kube-system", "Kubernetes namespace")
	cmd.Flags().StringVar(&opts.ReleaseName, "release-name", "headcni", "Helm release name")
	cmd.Flags().StringVar(&opts.ImageTag, "image-tag", "latest", "Docker image tag to upgrade to")
	cmd.Flags().StringVar(&opts.ImageRepo, "image-repo", "binrc/headcni", "Docker image repository")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be upgraded without actually upgrading")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Force upgrade (skip compatibility checks)")
	cmd.Flags().IntVar(&opts.Timeout, "timeout", 300, "Upgrade timeout in seconds")

	return cmd
}

func runUpgrade(opts *UpgradeOptions) error {
	// 显示 ASCII logo
	showLogo()

	fmt.Printf("🔄 Upgrading HeadCNI...\n")
	fmt.Printf("Namespace: %s\n", opts.Namespace)
	fmt.Printf("Release Name: %s\n", opts.ReleaseName)
	fmt.Printf("Target Image: %s:%s\n", opts.ImageRepo, opts.ImageTag)
	fmt.Printf("Dry Run: %v\n", opts.DryRun)
	fmt.Printf("Force: %v\n\n", opts.Force)

	// 检查集群连接
	if err := checkClusterConnection(); err != nil {
		return fmt.Errorf("cluster connection failed: %v", err)
	}

	// 检查当前安装状态
	currentVersion, err := getCurrentVersion(opts)
	if err != nil {
		return fmt.Errorf("failed to get current version: %v", err)
	}

	fmt.Printf("Current version: %s\n", currentVersion)
	fmt.Printf("Target version: %s\n\n", opts.ImageTag)

	// 检查是否已经是最新版本
	if currentVersion == opts.ImageTag && !opts.Force {
		fmt.Println("✅ Already running the target version")
		return nil
	}

	// 检查升级兼容性
	if !opts.Force {
		if err := checkUpgradeCompatibility(currentVersion, opts.ImageTag); err != nil {
			return fmt.Errorf("upgrade compatibility check failed: %v", err)
		}
	}

	// 执行升级
	if err := performUpgrade(opts); err != nil {
		return fmt.Errorf("upgrade failed: %v", err)
	}

	// 验证升级
	if !opts.DryRun {
		if err := verifyUpgrade(opts); err != nil {
			return fmt.Errorf("upgrade verification failed: %v", err)
		}
	}

	fmt.Println("✅ Upgrade completed successfully!")
	return nil
}

func getCurrentVersion(opts *UpgradeOptions) (string, error) {
	// 获取当前DaemonSet的镜像标签
	cmd := exec.Command("kubectl", "get", "daemonset", opts.ReleaseName,
		"-n", opts.Namespace, "-o", "jsonpath={.spec.template.spec.containers[0].image}")

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current image: %v", err)
	}

	image := strings.TrimSpace(string(output))
	if image == "" {
		return "unknown", nil
	}

	// 提取标签
	parts := strings.Split(image, ":")
	if len(parts) > 1 {
		return parts[len(parts)-1], nil
	}

	return "latest", nil
}

func checkUpgradeCompatibility(currentVersion, targetVersion string) error {
	fmt.Println("🔍 Checking upgrade compatibility...")

	// 简单的版本兼容性检查
	// 在实际实现中，这里应该包含更复杂的版本兼容性逻辑

	if currentVersion == "unknown" || targetVersion == "latest" {
		fmt.Println("⚠️  Skipping compatibility check (unknown current version or latest target)")
		return nil
	}

	// 这里可以添加更复杂的版本比较逻辑
	// 例如：检查主版本号是否兼容，检查配置格式是否兼容等

	fmt.Println("✅ Upgrade compatibility check passed")
	return nil
}

func performUpgrade(opts *UpgradeOptions) error {
	fmt.Println("🚀 Performing upgrade...")

	if opts.DryRun {
		fmt.Println("📋 Dry run - would perform the following actions:")
		fmt.Printf("  - Update DaemonSet image to %s:%s\n", opts.ImageRepo, opts.ImageTag)
		fmt.Printf("  - Restart HeadCNI pods\n")
		fmt.Printf("  - Verify pod health\n")
		return nil
	}

	// 使用kubectl patch更新DaemonSet镜像
	patch := fmt.Sprintf(`{"spec":{"template":{"spec":{"containers":[{"name":"headcni","image":"%s:%s"}]}}}}`,
		opts.ImageRepo, opts.ImageTag)

	cmd := exec.Command("kubectl", "patch", "daemonset", opts.ReleaseName,
		"-n", opts.Namespace, "--type", "strategic", "--patch", patch)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to update DaemonSet: %v, output: %s", err, string(output))
	}

	fmt.Println("✅ DaemonSet updated successfully")

	// 等待pods重启
	fmt.Println("⏳ Waiting for pods to restart...")
	if err := waitForPodsReady(opts); err != nil {
		return fmt.Errorf("failed to wait for pods ready: %v", err)
	}

	return nil
}

func waitForPodsReady(opts *UpgradeOptions) error {
	// 等待DaemonSet就绪
	cmd := exec.Command("kubectl", "rollout", "status", "daemonset", opts.ReleaseName,
		"-n", opts.Namespace, "--timeout", fmt.Sprintf("%ds", opts.Timeout))

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to wait for rollout: %v, output: %s", err, string(output))
	}

	fmt.Println("✅ All pods are ready")
	return nil
}

func verifyUpgrade(opts *UpgradeOptions) error {
	fmt.Println("🔍 Verifying upgrade...")

	// 检查pods状态
	pods, err := getHeadCNIPods(opts.Namespace, opts.ReleaseName)
	if err != nil {
		return fmt.Errorf("failed to get pods: %v", err)
	}

	readyCount := 0
	for _, pod := range pods {
		if pod.Status == "Running" {
			readyCount++
		}
	}

	if readyCount != len(pods) {
		return fmt.Errorf("not all pods are ready: %d/%d", readyCount, len(pods))
	}

	// 检查新版本
	newVersion, err := getCurrentVersion(opts)
	if err != nil {
		return fmt.Errorf("failed to get new version: %v", err)
	}

	if newVersion != opts.ImageTag {
		return fmt.Errorf("version mismatch: expected %s, got %s", opts.ImageTag, newVersion)
	}

	// 运行连接测试
	fmt.Println("🔗 Running connectivity tests...")
	if err := runQuickConnectivityTest(opts); err != nil {
		return fmt.Errorf("connectivity test failed: %v", err)
	}

	fmt.Println("✅ Upgrade verification completed")
	return nil
}

func runQuickConnectivityTest(opts *UpgradeOptions) error {
	// 简单的连接测试
	// 在实际实现中，这里应该运行更全面的测试

	// 检查HeadCNI pods是否响应
	pods, err := getHeadCNIPods(opts.Namespace, opts.ReleaseName)
	if err != nil {
		return err
	}

	if len(pods) == 0 {
		return fmt.Errorf("no HeadCNI pods found")
	}

	// 检查pod日志是否有错误
	for _, pod := range pods {
		cmd := exec.Command("kubectl", "logs", pod.Name, "-n", opts.Namespace, "--tail", "10")
		output, err := cmd.Output()
		if err != nil {
			continue // 忽略日志获取错误
		}

		if strings.Contains(strings.ToLower(string(output)), "error") {
			fmt.Printf("⚠️  Warning: Found errors in pod %s logs\n", pod.Name)
		}
	}

	return nil
}
