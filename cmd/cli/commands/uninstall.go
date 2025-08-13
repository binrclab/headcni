package commands

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

type UninstallOptions struct {
	Namespace   string
	ReleaseName string
	Force       bool
	DryRun      bool
}

func NewUninstallCommand() *cobra.Command {
	opts := &UninstallOptions{}

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall HeadCNI from Kubernetes cluster",
		Long: `Uninstall HeadCNI CNI plugin from your Kubernetes cluster.

This command will:
1. Remove HeadCNI DaemonSet
2. Clean up CNI configuration
3. Remove related resources

Examples:
  # Basic uninstall
  headcni uninstall

  # Force uninstall
  headcni uninstall --force

  # Dry run
  headcni uninstall --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUninstall(opts)
		},
	}

	cmd.Flags().StringVar(&opts.Namespace, "namespace", "kube-system", "Kubernetes namespace")
	cmd.Flags().StringVar(&opts.ReleaseName, "release-name", "headcni", "Helm release name")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Force uninstall without confirmation")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be uninstalled without actually uninstalling")

	return cmd
}

func runUninstall(opts *UninstallOptions) error {
	// 显示 ASCII logo
	showLogo()

	fmt.Printf("🗑️  Uninstalling HeadCNI...\n")
	fmt.Printf("Namespace: %s\n", opts.Namespace)
	fmt.Printf("Release Name: %s\n", opts.ReleaseName)
	fmt.Printf("Force: %v\n", opts.Force)
	fmt.Printf("Dry Run: %v\n\n", opts.DryRun)

	// 检查集群连接
	if err := checkClusterConnection(); err != nil {
		return fmt.Errorf("cluster connection failed: %v", err)
	}

	// 检查是否已安装
	if err := checkInstallation(opts); err != nil {
		return fmt.Errorf("installation check failed: %v", err)
	}

	// 确认卸载（除非使用 --force）
	if !opts.Force && !opts.DryRun {
		fmt.Printf("⚠️  This will remove HeadCNI from your cluster.\n")
		fmt.Printf("Are you sure you want to continue? (y/N): ")

		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Printf("Uninstall cancelled.\n")
			return nil
		}
	}

	// 执行 Helm 卸载
	if err := executeHelmUninstall(opts); err != nil {
		return fmt.Errorf("helm uninstall failed: %v", err)
	}

	// 清理 CNI 配置
	if err := cleanupCNIConfig(opts); err != nil {
		return fmt.Errorf("CNI config cleanup failed: %v", err)
	}

	// 清理 Secret
	if err := cleanupSecret(opts); err != nil {
		return fmt.Errorf("secret cleanup failed: %v", err)
	}

	fmt.Printf("\n✅ HeadCNI uninstalled successfully!\n")
	fmt.Printf("\nNote: You may need to restart kubelet on your nodes to fully clean up CNI configuration.\n")

	return nil
}

func checkInstallation(opts *UninstallOptions) error {
	fmt.Printf("🔍 Checking installation...\n")

	cmd := exec.Command("kubectl", "get", "daemonset", opts.ReleaseName, "-n", opts.Namespace)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("HeadCNI is not installed in namespace %s", opts.Namespace)
	}

	fmt.Printf("✅ HeadCNI installation found\n")
	return nil
}

func executeHelmUninstall(opts *UninstallOptions) error {
	fmt.Printf("🚀 Executing Helm uninstall...\n")

	helmCmd := fmt.Sprintf("helm uninstall %s -n %s", opts.ReleaseName, opts.Namespace)
	if opts.DryRun {
		fmt.Printf("Would execute: %s\n", helmCmd)
		return nil
	}

	cmd := exec.Command("sh", "-c", helmCmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("helm uninstall command failed: %v", err)
	}

	fmt.Printf("✅ Helm uninstall completed\n")
	return nil
}

func cleanupCNIConfig(opts *UninstallOptions) error {
	fmt.Printf("🧹 Cleaning up CNI configuration...\n")

	// 删除 ConfigMap
	cmd := exec.Command("kubectl", "delete", "configmap", opts.ReleaseName+"-config", "-n", opts.Namespace, "--ignore-not-found=true")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete configmap: %v", err)
	}

	fmt.Printf("✅ CNI configuration cleaned up\n")
	return nil
}

func cleanupSecret(opts *UninstallOptions) error {
	fmt.Printf("🔐 Cleaning up secrets...\n")

	// 删除 auth key secret
	cmd := exec.Command("kubectl", "delete", "secret", opts.ReleaseName+"-auth", "-n", opts.Namespace, "--ignore-not-found=true")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete secret: %v", err)
	}

	fmt.Printf("✅ Secrets cleaned up\n")
	return nil
}
