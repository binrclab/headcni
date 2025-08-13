package commands

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

type InstallOptions struct {
	HeadscaleURL string
	AuthKey      string
	Namespace    string
	ReleaseName  string
	PodCIDR      string
	ServiceCIDR  string
	IPAMType     string
	ImageRepo    string
	ImageTag     string
	DryRun       bool
}

func NewInstallCommand() *cobra.Command {
	opts := &InstallOptions{}

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install HeadCNI to Kubernetes cluster",
		Long: `Install HeadCNI CNI plugin to your Kubernetes cluster.

This command will:
1. Check cluster connectivity
2. Create necessary resources
3. Deploy HeadCNI DaemonSet
4. Verify installation

Examples:
  # Basic installation
  headcni install --headscale-url https://headscale.company.com --auth-key YOUR_KEY

  # Custom configuration
  headcni install --headscale-url https://headscale.company.com --auth-key YOUR_KEY \
    --pod-cidr 10.42.0.0/16 --ipam-type headcni-ipam

  # Dry run
  headcni install --headscale-url https://headscale.company.com --auth-key YOUR_KEY --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInstall(opts)
		},
	}

	// 必需参数
	cmd.Flags().StringVar(&opts.HeadscaleURL, "headscale-url", "", "Headscale server URL (required)")
	cmd.Flags().StringVar(&opts.AuthKey, "auth-key", "", "Headscale auth key (required)")
	cmd.MarkFlagRequired("headscale-url")
	cmd.MarkFlagRequired("auth-key")

	// 可选参数
	cmd.Flags().StringVar(&opts.Namespace, "namespace", "kube-system", "Kubernetes namespace")
	cmd.Flags().StringVar(&opts.ReleaseName, "release-name", "headcni", "Helm release name")
	cmd.Flags().StringVar(&opts.PodCIDR, "pod-cidr", "10.244.0.0/16", "Pod CIDR")
	cmd.Flags().StringVar(&opts.ServiceCIDR, "service-cidr", "10.96.0.0/16", "Service CIDR")
	cmd.Flags().StringVar(&opts.IPAMType, "ipam-type", "host-local", "IPAM type (host-local or headcni-ipam)")
	cmd.Flags().StringVar(&opts.ImageRepo, "image-repo", "binrc/headcni", "Docker image repository")
	cmd.Flags().StringVar(&opts.ImageTag, "image-tag", "latest", "Docker image tag")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be installed without actually installing")

	return cmd
}

func runInstall(opts *InstallOptions) error {
	// 显示 ASCII logo
	showLogo()

	fmt.Printf("🚀 Installing HeadCNI...\n")
	fmt.Printf("Headscale URL: %s\n", opts.HeadscaleURL)
	fmt.Printf("Namespace: %s\n", opts.Namespace)
	fmt.Printf("Release Name: %s\n", opts.ReleaseName)
	fmt.Printf("Pod CIDR: %s\n", opts.PodCIDR)
	fmt.Printf("Service CIDR: %s\n", opts.ServiceCIDR)
	fmt.Printf("IPAM Type: %s\n", opts.IPAMType)
	fmt.Printf("Image: %s:%s\n", opts.ImageRepo, opts.ImageTag)
	fmt.Printf("Dry Run: %v\n\n", opts.DryRun)

	// 检查前置条件
	if err := checkPrerequisites(); err != nil {
		return fmt.Errorf("prerequisites check failed: %v", err)
	}

	// 检查集群连接
	if err := checkClusterConnection(); err != nil {
		return fmt.Errorf("cluster connection failed: %v", err)
	}

	// 创建命名空间
	if err := createNamespace(opts.Namespace, opts.DryRun); err != nil {
		return fmt.Errorf("failed to create namespace: %v", err)
	}

	// 创建 Secret
	if err := createSecret(opts, opts.DryRun); err != nil {
		return fmt.Errorf("failed to create secret: %v", err)
	}

	// 构建 Helm 命令
	helmCmd := buildHelmCommand(opts)
	if opts.DryRun {
		fmt.Printf("Would execute: %s\n", helmCmd)
		return nil
	}

	// 执行 Helm 安装
	if err := executeHelmInstall(helmCmd); err != nil {
		return fmt.Errorf("helm installation failed: %v", err)
	}

	// 等待部署完成
	if err := waitForDeployment(opts); err != nil {
		return fmt.Errorf("deployment wait failed: %v", err)
	}

	// 验证安装
	if err := verifyInstallation(opts); err != nil {
		return fmt.Errorf("installation verification failed: %v", err)
	}

	fmt.Printf("\n✅ HeadCNI installed successfully!\n")
	fmt.Printf("\nNext steps:\n")
	fmt.Printf("1. Check status: headcni status\n")
	fmt.Printf("2. Test connectivity: headcni connect-test\n")
	fmt.Printf("3. View logs: kubectl logs -l app.kubernetes.io/name=headcni -n %s\n", opts.Namespace)

	return nil
}

func checkPrerequisites() error {
	fmt.Printf("🔍 Checking prerequisites...\n")

	// 检查 kubectl
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH")
	}

	// 检查 helm
	if _, err := exec.LookPath("helm"); err != nil {
		return fmt.Errorf("helm not found in PATH")
	}

	fmt.Printf("✅ Prerequisites check passed\n")
	return nil
}

func createNamespace(namespace string, dryRun bool) error {
	fmt.Printf("📦 Creating namespace %s...\n", namespace)

	args := []string{"create", "namespace", namespace, "--dry-run=client", "-o", "yaml"}
	if !dryRun {
		args = append(args, "|", "kubectl", "apply", "-f", "-")
	}

	cmd := exec.Command("kubectl", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create namespace: %v", err)
	}

	fmt.Printf("✅ Namespace %s ready\n", namespace)
	return nil
}

func createSecret(opts *InstallOptions, dryRun bool) error {
	fmt.Printf("🔐 Creating auth key secret...\n")

	args := []string{"create", "secret", "generic", opts.ReleaseName + "-auth",
		"--from-literal=auth-key=" + opts.AuthKey,
		"-n", opts.Namespace,
		"--dry-run=client", "-o", "yaml"}
	if !dryRun {
		args = append(args, "|", "kubectl", "apply", "-f", "-")
	}

	cmd := exec.Command("kubectl", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create secret: %v", err)
	}

	fmt.Printf("✅ Auth key secret created\n")
	return nil
}

func buildHelmCommand(opts *InstallOptions) string {
	chartPath := "./chart"
	if _, err := os.Stat(chartPath); os.IsNotExist(err) {
		// 如果本地没有chart，使用远程chart
		chartPath = "binrc/headcni"
	}

	cmd := fmt.Sprintf("helm upgrade --install %s %s "+
		"--namespace %s "+
		"--set config.headscale.url=%s "+
		"--set config.headscale.authKey=%s "+
		"--set config.network.podCIDRBase=%s "+
		"--set config.network.serviceCIDR=%s "+
		"--set config.ipam.type=%s "+
		"--set image.repository=%s "+
		"--set image.tag=%s",
		opts.ReleaseName, chartPath,
		opts.Namespace,
		opts.HeadscaleURL,
		opts.AuthKey,
		opts.PodCIDR,
		opts.ServiceCIDR,
		opts.IPAMType,
		opts.ImageRepo,
		opts.ImageTag)

	return cmd
}

func executeHelmInstall(helmCmd string) error {
	fmt.Printf("🚀 Executing Helm installation...\n")
	fmt.Printf("Command: %s\n", helmCmd)

	cmd := exec.Command("sh", "-c", helmCmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("helm command failed: %v", err)
	}

	fmt.Printf("✅ Helm installation completed\n")
	return nil
}

func waitForDeployment(opts *InstallOptions) error {
	fmt.Printf("⏳ Waiting for deployment to be ready...\n")

	cmd := exec.Command("kubectl", "wait", "--for=condition=available",
		"--timeout=300s", "daemonset/"+opts.ReleaseName, "-n", opts.Namespace)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("deployment wait failed: %v", err)
	}

	fmt.Printf("✅ Deployment is ready\n")
	return nil
}

func verifyInstallation(opts *InstallOptions) error {
	fmt.Printf("🔍 Verifying installation...\n")

	// 检查 DaemonSet
	cmd := exec.Command("kubectl", "get", "daemonset", opts.ReleaseName, "-n", opts.Namespace)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to verify DaemonSet: %v", err)
	}

	// 检查 Pods
	cmd = exec.Command("kubectl", "get", "pods", "-l", "app.kubernetes.io/name=headcni", "-n", opts.Namespace)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to verify Pods: %v", err)
	}

	fmt.Printf("✅ Installation verification passed\n")
	return nil
}
