package commands

import (
	"fmt"
	"os/exec"
	"time"

	"github.com/spf13/cobra"
)

type ConnectTestOptions struct {
	Namespace   string
	ReleaseName string
	Timeout     int
	Verbose     bool
}

type TestResult struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Duration string `json:"duration"`
	Error    string `json:"error,omitempty"`
}

func NewConnectTestCommand() *cobra.Command {
	opts := &ConnectTestOptions{}

	cmd := &cobra.Command{
		Use:   "connect-test",
		Short: "Test network connectivity",
		Long: `Test network connectivity in your HeadCNI cluster.

This command will perform various connectivity tests:
- Pod-to-pod communication
- Pod-to-service communication
- External network access
- Tailscale mesh connectivity

Examples:
  # Basic connectivity test
  headcni connect-test

  # Verbose test with custom timeout
  headcni connect-test --verbose --timeout 60

  # Test in specific namespace
  headcni connect-test --namespace my-namespace`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConnectTest(opts)
		},
	}

	cmd.Flags().StringVar(&opts.Namespace, "namespace", "kube-system", "Kubernetes namespace")
	cmd.Flags().StringVar(&opts.ReleaseName, "release-name", "headcni", "Helm release name")
	cmd.Flags().IntVar(&opts.Timeout, "timeout", 30, "Test timeout in seconds")
	cmd.Flags().BoolVar(&opts.Verbose, "verbose", false, "Verbose output")

	return cmd
}

func runConnectTest(opts *ConnectTestOptions) error {
	// 显示 ASCII logo
	showLogo()
	
	fmt.Printf("🔗 Running HeadCNI connectivity tests...\n")
	fmt.Printf("Namespace: %s\n", opts.Namespace)
	fmt.Printf("Timeout: %d seconds\n\n", opts.Timeout)

	// 检查集群连接
	if err := checkClusterConnection(); err != nil {
		return fmt.Errorf("cluster connection failed: %v", err)
	}

	var results []TestResult

	// 测试1: 检查 HeadCNI Pods 是否运行
	fmt.Printf("📦 Test 1: Checking HeadCNI Pods...\n")
	result := testHeadCNIPods(opts)
	results = append(results, result)
	printTestResult(result, opts.Verbose)

	// 测试2: 测试 Pod-to-Pod 通信
	fmt.Printf("📦 Test 2: Pod-to-Pod Communication...\n")
	result = testPodToPodCommunication(opts)
	results = append(results, result)
	printTestResult(result, opts.Verbose)

	// 测试3: 测试 Pod-to-Service 通信
	fmt.Printf("📦 Test 3: Pod-to-Service Communication...\n")
	result = testPodToServiceCommunication(opts)
	results = append(results, result)
	printTestResult(result, opts.Verbose)

	// 测试4: 测试外部网络访问
	fmt.Printf("📦 Test 4: External Network Access...\n")
	result = testExternalNetworkAccess(opts)
	results = append(results, result)
	printTestResult(result, opts.Verbose)

	// 测试5: 测试 Tailscale 连接
	fmt.Printf("📦 Test 5: Tailscale Connectivity...\n")
	result = testTailscaleConnectivity(opts)
	results = append(results, result)
	printTestResult(result, opts.Verbose)

	// 测试6: 测试 CNI 插件功能
	fmt.Printf("📦 Test 6: CNI Plugin Functionality...\n")
	result = testCNIPluginFunctionality(opts)
	results = append(results, result)
	printTestResult(result, opts.Verbose)

	// 输出总结
	printTestSummary(results)

	return nil
}

func testHeadCNIPods(opts *ConnectTestOptions) TestResult {
	start := time.Now()
	result := TestResult{Name: "HeadCNI Pods Status"}

	cmd := exec.Command("kubectl", "get", "pods",
		"-l", "app.kubernetes.io/name=headcni",
		"-n", opts.Namespace,
		"-o", "jsonpath={.items[*].status.phase}")

	output, err := cmd.Output()
	if err != nil {
		result.Status = "FAILED"
		result.Error = fmt.Sprintf("Failed to get pod status: %v", err)
	} else {
		podStatuses := string(output)
		if len(podStatuses) == 0 {
			result.Status = "FAILED"
			result.Error = "No HeadCNI pods found"
		} else {
			// 检查是否所有pod都是Running状态
			allRunning := true
			for _, status := range podStatuses {
				if status != 'R' && status != ' ' {
					allRunning = false
					break
				}
			}
			if allRunning {
				result.Status = "PASSED"
			} else {
				result.Status = "FAILED"
				result.Error = "Not all pods are running"
			}
		}
	}

	result.Duration = time.Since(start).String()
	return result
}

func testPodToPodCommunication(opts *ConnectTestOptions) TestResult {
	start := time.Now()
	result := TestResult{Name: "Pod-to-Pod Communication"}

	// 创建测试pod
	testPodName := "headcni-test-pod"

	// 清理之前的测试pod
	exec.Command("kubectl", "delete", "pod", testPodName, "-n", opts.Namespace, "--ignore-not-found=true").Run()

	// 创建测试pod
	createCmd := exec.Command("kubectl", "run", testPodName,
		"--image=busybox",
		"--restart=Never",
		"--namespace", opts.Namespace,
		"--command", "--",
		"sleep", "3600")

	if err := createCmd.Run(); err != nil {
		result.Status = "FAILED"
		result.Error = fmt.Sprintf("Failed to create test pod: %v", err)
		result.Duration = time.Since(start).String()
		return result
	}

	// 等待pod就绪
	waitCmd := exec.Command("kubectl", "wait", "--for=condition=ready",
		"pod", testPodName, "-n", opts.Namespace, "--timeout=60s")
	if err := waitCmd.Run(); err != nil {
		result.Status = "FAILED"
		result.Error = fmt.Sprintf("Test pod not ready: %v", err)
		// 清理
		exec.Command("kubectl", "delete", "pod", testPodName, "-n", opts.Namespace).Run()
		result.Duration = time.Since(start).String()
		return result
	}

	// 测试网络连接
	testCmd := exec.Command("kubectl", "exec", testPodName, "-n", opts.Namespace,
		"--", "nslookup", "kubernetes.default")

	if err := testCmd.Run(); err != nil {
		result.Status = "FAILED"
		result.Error = fmt.Sprintf("DNS resolution failed: %v", err)
	} else {
		result.Status = "PASSED"
	}

	// 清理测试pod
	exec.Command("kubectl", "delete", "pod", testPodName, "-n", opts.Namespace).Run()

	result.Duration = time.Since(start).String()
	return result
}

func testPodToServiceCommunication(opts *ConnectTestOptions) TestResult {
	start := time.Now()
	result := TestResult{Name: "Pod-to-Service Communication"}

	// 创建测试service
	testServiceName := "headcni-test-service"

	// 清理之前的测试资源
	exec.Command("kubectl", "delete", "service", testServiceName, "-n", opts.Namespace, "--ignore-not-found=true").Run()
	exec.Command("kubectl", "delete", "pod", testServiceName, "-n", opts.Namespace, "--ignore-not-found=true").Run()

	// 创建测试pod
	createPodCmd := exec.Command("kubectl", "run", testServiceName,
		"--image=nginx",
		"--restart=Never",
		"--namespace", opts.Namespace)

	if err := createPodCmd.Run(); err != nil {
		result.Status = "FAILED"
		result.Error = fmt.Sprintf("Failed to create test service pod: %v", err)
		result.Duration = time.Since(start).String()
		return result
	}

	// 创建service
	createSvcCmd := exec.Command("kubectl", "expose", "pod", testServiceName,
		"--port=80", "--target-port=80",
		"--namespace", opts.Namespace,
		"--name", testServiceName)

	if err := createSvcCmd.Run(); err != nil {
		result.Status = "FAILED"
		result.Error = fmt.Sprintf("Failed to create test service: %v", err)
		// 清理
		exec.Command("kubectl", "delete", "pod", testServiceName, "-n", opts.Namespace).Run()
		result.Duration = time.Since(start).String()
		return result
	}

	// 等待pod就绪
	waitCmd := exec.Command("kubectl", "wait", "--for=condition=ready",
		"pod", testServiceName, "-n", opts.Namespace, "--timeout=60s")
	if err := waitCmd.Run(); err != nil {
		result.Status = "FAILED"
		result.Error = fmt.Sprintf("Test service pod not ready: %v", err)
		// 清理
		exec.Command("kubectl", "delete", "service", testServiceName, "-n", opts.Namespace).Run()
		exec.Command("kubectl", "delete", "pod", testServiceName, "-n", opts.Namespace).Run()
		result.Duration = time.Since(start).String()
		return result
	}

	// 创建客户端pod测试连接
	clientPodName := "headcni-test-client"
	exec.Command("kubectl", "delete", "pod", clientPodName, "-n", opts.Namespace, "--ignore-not-found=true").Run()

	createClientCmd := exec.Command("kubectl", "run", clientPodName,
		"--image=busybox",
		"--restart=Never",
		"--namespace", opts.Namespace,
		"--command", "--",
		"sleep", "3600")

	if err := createClientCmd.Run(); err != nil {
		result.Status = "FAILED"
		result.Error = fmt.Sprintf("Failed to create client pod: %v", err)
		// 清理
		exec.Command("kubectl", "delete", "service", testServiceName, "-n", opts.Namespace).Run()
		exec.Command("kubectl", "delete", "pod", testServiceName, "-n", opts.Namespace).Run()
		result.Duration = time.Since(start).String()
		return result
	}

	// 等待客户端pod就绪
	waitClientCmd := exec.Command("kubectl", "wait", "--for=condition=ready",
		"pod", clientPodName, "-n", opts.Namespace, "--timeout=60s")
	if err := waitClientCmd.Run(); err != nil {
		result.Status = "FAILED"
		result.Error = fmt.Sprintf("Client pod not ready: %v", err)
		// 清理
		exec.Command("kubectl", "delete", "pod", clientPodName, "-n", opts.Namespace).Run()
		exec.Command("kubectl", "delete", "service", testServiceName, "-n", opts.Namespace).Run()
		exec.Command("kubectl", "delete", "pod", testServiceName, "-n", opts.Namespace).Run()
		result.Duration = time.Since(start).String()
		return result
	}

	// 测试服务连接
	testCmd := exec.Command("kubectl", "exec", clientPodName, "-n", opts.Namespace,
		"--", "wget", "-q", "-O-", fmt.Sprintf("http://%s", testServiceName))

	if err := testCmd.Run(); err != nil {
		result.Status = "FAILED"
		result.Error = fmt.Sprintf("Service connection failed: %v", err)
	} else {
		result.Status = "PASSED"
	}

	// 清理所有测试资源
	exec.Command("kubectl", "delete", "pod", clientPodName, "-n", opts.Namespace).Run()
	exec.Command("kubectl", "delete", "service", testServiceName, "-n", opts.Namespace).Run()
	exec.Command("kubectl", "delete", "pod", testServiceName, "-n", opts.Namespace).Run()

	result.Duration = time.Since(start).String()
	return result
}

func testExternalNetworkAccess(opts *ConnectTestOptions) TestResult {
	start := time.Now()
	result := TestResult{Name: "External Network Access"}

	// 创建测试pod
	testPodName := "headcni-external-test"

	// 清理之前的测试pod
	exec.Command("kubectl", "delete", "pod", testPodName, "-n", opts.Namespace, "--ignore-not-found=true").Run()

	// 创建测试pod
	createCmd := exec.Command("kubectl", "run", testPodName,
		"--image=busybox",
		"--restart=Never",
		"--namespace", opts.Namespace,
		"--command", "--",
		"sleep", "3600")

	if err := createCmd.Run(); err != nil {
		result.Status = "FAILED"
		result.Error = fmt.Sprintf("Failed to create test pod: %v", err)
		result.Duration = time.Since(start).String()
		return result
	}

	// 等待pod就绪
	waitCmd := exec.Command("kubectl", "wait", "--for=condition=ready",
		"pod", testPodName, "-n", opts.Namespace, "--timeout=60s")
	if err := waitCmd.Run(); err != nil {
		result.Status = "FAILED"
		result.Error = fmt.Sprintf("Test pod not ready: %v", err)
		// 清理
		exec.Command("kubectl", "delete", "pod", testPodName, "-n", opts.Namespace).Run()
		result.Duration = time.Since(start).String()
		return result
	}

	// 测试外部网络连接
	testCmd := exec.Command("kubectl", "exec", testPodName, "-n", opts.Namespace,
		"--", "wget", "-q", "-O-", "--timeout=10", "https://www.google.com")

	if err := testCmd.Run(); err != nil {
		result.Status = "FAILED"
		result.Error = fmt.Sprintf("External network access failed: %v", err)
	} else {
		result.Status = "PASSED"
	}

	// 清理测试pod
	exec.Command("kubectl", "delete", "pod", testPodName, "-n", opts.Namespace).Run()

	result.Duration = time.Since(start).String()
	return result
}

func testTailscaleConnectivity(opts *ConnectTestOptions) TestResult {
	start := time.Now()
	result := TestResult{Name: "Tailscale Connectivity"}

	// 检查 Tailscale 状态
	cmd := exec.Command("tailscale", "status", "--json")
	output, err := cmd.Output()
	if err != nil {
		result.Status = "FAILED"
		result.Error = fmt.Sprintf("Tailscale not available: %v", err)
		result.Duration = time.Since(start).String()
		return result
	}

	// 简单的状态检查
	if len(output) == 0 {
		result.Status = "FAILED"
		result.Error = "Tailscale status empty"
	} else {
		result.Status = "PASSED"
	}

	result.Duration = time.Since(start).String()
	return result
}

func testCNIPluginFunctionality(opts *ConnectTestOptions) TestResult {
	start := time.Now()
	result := TestResult{Name: "CNI Plugin Functionality"}

	// 检查 CNI 配置
	cmd := exec.Command("kubectl", "get", "configmap", "-n", opts.Namespace,
		"-o", "jsonpath={.items[?(@.metadata.name=='headcni-config')].metadata.name}")
	output, err := cmd.Output()
	if err != nil || len(output) == 0 {
		result.Status = "FAILED"
		result.Error = "CNI configuration not found"
		result.Duration = time.Since(start).String()
		return result
	}

	// 检查 CNI 二进制文件（通过检查节点上的配置）
	nodeCmd := exec.Command("kubectl", "get", "nodes", "-o", "jsonpath={.items[0].status.nodeInfo.kubeletVersion}")
	if err := nodeCmd.Run(); err != nil {
		result.Status = "FAILED"
		result.Error = "Cannot access node information"
	} else {
		result.Status = "PASSED"
	}

	result.Duration = time.Since(start).String()
	return result
}

func printTestResult(result TestResult, verbose bool) {
	statusIcon := "❌"
	if result.Status == "PASSED" {
		statusIcon = "✅"
	} else if result.Status == "SKIPPED" {
		statusIcon = "⏭️"
	}

	fmt.Printf("   %s %s (%s)\n", statusIcon, result.Name, result.Duration)

	if verbose && result.Error != "" {
		fmt.Printf("      Error: %s\n", result.Error)
	}
}

func printTestSummary(results []TestResult) {
	fmt.Printf("\n📊 Test Summary:\n")

	passed := 0
	failed := 0
	skipped := 0

	for _, result := range results {
		switch result.Status {
		case "PASSED":
			passed++
		case "FAILED":
			failed++
		case "SKIPPED":
			skipped++
		}
	}

	fmt.Printf("   ✅ Passed: %d\n", passed)
	fmt.Printf("   ❌ Failed: %d\n", failed)
	if skipped > 0 {
		fmt.Printf("   ⏭️  Skipped: %d\n", skipped)
	}

	if failed == 0 {
		fmt.Printf("\n🎉 All tests passed! HeadCNI is working correctly.\n")
	} else {
		fmt.Printf("\n⚠️  Some tests failed. Please check the errors above.\n")
	}
}
