package commands

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/pterm/pterm"
)

// showLogo 显示 HeadCNI ASCII logo
func showLogo() {
	// 清除屏幕并显示标题
	fmt.Print("\033[H\033[2J")

	// 显示大标题
	pterm.DefaultHeader.WithBackgroundStyle(pterm.NewStyle(pterm.BgMagenta)).
		WithTextStyle(pterm.NewStyle(pterm.FgWhite, pterm.Bold)).
		Println("HeadCNI - Professional Kubernetes CNI Plugin")

	// 显示logo
	logo := `  dbbbbbbbbbbbbbbbbbbb    
  bbbbbdbbbbk       bd    
  bbbbbbbbbbbbb     bd    
  bbbbbdbbbbbbbb    bd    
  bbbb   bbbk       bd    
  bb        bbb  bbbbd    
  bb     dbbbbbddbbbbd    
  bb     bbbbbbbbbbbbd    
  bb        bbbddbbbbd    
  vbbbbbbbbbbbbbbdbbbP    
                                                      
Kubernetes CNI Plugin for Headscale/Tailscale    
================================================`

	pterm.FgMagenta.Println(logo)

	// 显示状态信息
	showSystemStatus()

	// 显示分隔线
	pterm.DefaultSection.Println("System Overview")
}

// showSystemStatus 显示系统状态
func showSystemStatus() {
	// 创建状态面板
	statusData := pterm.TableData{
		{"Component", "Status", "Details"},
	}

	// 检查Kubernetes连接
	kubeStatus := "❌ Disconnected"
	kubeDetails := "Cluster not accessible"
	if err := checkClusterConnection(); err == nil {
		kubeStatus = "✅ Connected"
		kubeDetails = "Cluster accessible"
	}
	statusData = append(statusData, []string{"Kubernetes", kubeStatus, kubeDetails})

	// 检查HeadCNI DaemonSet状态
	dsStatus := "❌ Not Found"
	dsDetails := "DaemonSet not deployed"
	if dsInfo := getDaemonSetInfo(); dsInfo != nil {
		if dsInfo.Ready == dsInfo.Desired {
			dsStatus = "✅ Ready"
			dsDetails = fmt.Sprintf("Pods: %d/%d", dsInfo.Ready, dsInfo.Desired)
		} else {
			dsStatus = "⏳ Pending"
			dsDetails = fmt.Sprintf("Pods: %d/%d", dsInfo.Ready, dsInfo.Desired)
		}
	}
	statusData = append(statusData, []string{"HeadCNI DaemonSet", dsStatus, dsDetails})

	// 检查Tailscale状态
	tsStatus := "❌ Disconnected"
	tsDetails := "Tailscale not connected"
	if tsInfo := getTailscaleInfo(); tsInfo != nil {
		tsStatus = "✅ Connected"
		tsDetails = fmt.Sprintf("IP: %s", tsInfo.IP)
	}
	statusData = append(statusData, []string{"Tailscale", tsStatus, tsDetails})

	// 显示版本信息
	statusData = append(statusData, []string{"Version", "ℹ️ Info", getVersion()})

	// 使用表格显示状态
	pterm.DefaultTable.WithHasHeader().WithData(statusData).Render()
}

// getVersion 获取版本信息
func getVersion() string {
	return "v1.0.0-dev"
}

// getQuickStatus 获取快速状态信息
func getQuickStatus() string {
	// 检查Kubernetes连接
	kubeStatus := "❌ Disconnected"
	if err := checkClusterConnection(); err == nil {
		kubeStatus = "✅ Connected"
	}

	// 检查HeadCNI DaemonSet状态
	dsStatus := "❌ Not Found"
	if dsInfo := getDaemonSetInfo(); dsInfo != nil {
		if dsInfo.Ready == dsInfo.Desired {
			dsStatus = fmt.Sprintf("✅ Ready (%d/%d)", dsInfo.Ready, dsInfo.Desired)
		} else {
			dsStatus = fmt.Sprintf("⏳ Pending (%d/%d)", dsInfo.Ready, dsInfo.Desired)
		}
	}

	// 检查Tailscale状态
	tsStatus := "❌ Disconnected"
	if tsInfo := getTailscaleInfo(); tsInfo != nil {
		tsStatus = fmt.Sprintf("✅ %s", tsInfo.IP)
	}

	status := fmt.Sprintf(`Version: %s
Kubernetes: %s
DaemonSet: %s
Tailscale: %s`,
		getVersion(), kubeStatus, dsStatus, tsStatus)

	return status
}

// getDaemonSetInfo 获取DaemonSet信息
func getDaemonSetInfo() *struct {
	Ready   int
	Desired int
} {
	cmd := exec.Command("kubectl", "get", "daemonset", "headcni", "-n", "kube-system", "-o", "json")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		return nil
	}

	status := result["status"].(map[string]interface{})
	ready := int(status["ready"].(float64))
	desired := int(status["desiredNumberScheduled"].(float64))

	return &struct {
		Ready   int
		Desired int
	}{
		Ready:   ready,
		Desired: desired,
	}
}

// getTailscaleInfo 获取Tailscale信息
func getTailscaleInfo() *struct {
	IP string
} {
	cmd := exec.Command("tailscale", "ip")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	ip := strings.TrimSpace(string(output))
	if ip == "" {
		return nil
	}

	return &struct {
		IP string
	}{
		IP: ip,
	}
}

// showTwoColumnLayoutWithColors 显示带颜色的两栏布局
func showTwoColumnLayoutWithColors(left, right string, width int) {
	if width == 0 {
		width = 45
	}

	// 分割左右内容为多行
	leftLines := strings.Split(left, "\n")
	rightLines := strings.Split(right, "\n")

	// 计算最大行数
	maxLines := len(leftLines)
	if len(rightLines) > maxLines {
		maxLines = len(rightLines)
	}

	// 打印两栏内容
	for i := 0; i < maxLines; i++ {
		leftContent := ""
		rightContent := ""

		if i < len(leftLines) {
			leftContent = leftLines[i]
		}
		if i < len(rightLines) {
			rightContent = rightLines[i]
		}

		// 格式化输出，保持颜色
		fmt.Printf("%-*s %s\n", width, leftContent, rightContent)
	}
}

// showTwoColumnLayout 显示两栏布局
func showTwoColumnLayout(left, right string, width int) {
	if width == 0 {
		width = 40
	}

	// 分割左右内容为多行
	leftLines := strings.Split(left, "\n")
	rightLines := strings.Split(right, "\n")

	// 计算最大行数
	maxLines := len(leftLines)
	if len(rightLines) > maxLines {
		maxLines = len(rightLines)
	}

	// 打印两栏内容
	for i := 0; i < maxLines; i++ {
		leftContent := ""
		rightContent := ""

		if i < len(leftLines) {
			leftContent = strings.TrimSpace(leftLines[i])
		}
		if i < len(rightLines) {
			rightContent = strings.TrimSpace(rightLines[i])
		}

		// 格式化输出
		fmt.Printf("%-*s │ %s\n", width, leftContent, rightContent)
	}
}

// showTable 显示表格
func showTable(headers []string, rows [][]string) {
	if len(headers) == 0 || len(rows) == 0 {
		return
	}

	// 创建表格数据
	tableData := pterm.TableData{headers}
	tableData = append(tableData, rows...)

	// 使用 pterm 显示表格
	pterm.DefaultTable.WithHasHeader().WithData(tableData).Render()
}

// showStatusCard 显示状态卡片
func showStatusCard(title string, items map[string]string) {
	// 创建面板数据
	var panelData []string
	for key, value := range items {
		statusIcon := "✅"
		if strings.Contains(strings.ToLower(value), "failed") ||
			strings.Contains(strings.ToLower(value), "error") ||
			strings.Contains(strings.ToLower(value), "not") {
			statusIcon = "❌"
		} else if strings.Contains(strings.ToLower(value), "pending") ||
			strings.Contains(strings.ToLower(value), "waiting") {
			statusIcon = "⏳"
		}

		panelData = append(panelData, fmt.Sprintf("%s %s: %s", statusIcon, key, value))
	}

	// 使用 pterm 显示面板
	pterm.DefaultBox.WithTitle(title).Println(strings.Join(panelData, "\n"))
}

// showProgressBar 显示进度条
func showProgressBar(current, total int, label string) {
	if total == 0 {
		return
	}

	percentage := float64(current) / float64(total) * 100
	barWidth := 30
	filled := int(float64(barWidth) * percentage / 100)

	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	fmt.Printf("\r%s [%s] %.1f%% (%d/%d)", label, bar, percentage, current, total)

	if current >= total {
		fmt.Println()
	}
}

// checkClusterConnection 检查 Kubernetes 集群连接
func checkClusterConnection() error {
	cmd := exec.Command("kubectl", "cluster-info")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cannot connect to Kubernetes cluster")
	}
	return nil
}

// showSuccessMessage 显示成功消息
func showSuccessMessage(message string) {
	fmt.Printf("\n✅ %s\n", message)
}

// showErrorMessage 显示错误消息
func showErrorMessage(message string) {
	fmt.Printf("\n❌ %s\n", message)
}

// showWarningMessage 显示警告消息
func showWarningMessage(message string) {
	fmt.Printf("\n⚠️  %s\n", message)
}

// showInfoMessage 显示信息消息
func showInfoMessage(message string) {
	fmt.Printf("\nℹ️  %s\n", message)
}

// showProgressMessage 显示进度消息
func showProgressMessage(message string) {
	fmt.Printf("🔄 %s\n", message)
}

// showStepMessage 显示步骤消息
func showStepMessage(step int, total int, message string) {
	fmt.Printf("📋 Step %d/%d: %s\n", step, total, message)
}

// showSectionHeader 显示章节标题
func showSectionHeader(title string) {
	pterm.DefaultSection.Println(title)
}

// showSubSectionHeader 显示子章节标题
func showSubSectionHeader(title string) {
	pterm.DefaultSection.WithLevel(2).Println(title)
}

// generateSimplifiedConfig 生成简化的 CNI 配置
func generateSimplifiedConfig() string {
	config := `{
  "cniVersion": "1.0.0",
  "name": "tailscale-cni",
  "type": "headcni",
  
  "pod_cidr": "10.244.0.0/24",
  "service_cidr": "10.96.0.0/16",
  
  "ipam": {
    "type": "headcni-ipam",
    "subnet": "10.244.0.0/24",
    "gateway": "10.244.0.1",
    "allocation_strategy": "sequential"
  },
  
  "dns": {
    "nameservers": ["10.96.0.10"],
    "search": ["default.svc.cluster.local", "svc.cluster.local", "cluster.local"],
    "options": ["ndots:5"]
  },
  
  "mtu": 1420,
  "enable_ipv6": false,
  "enable_network_policy": true
}`
	return config
}

// showConfigExplanation 显示配置说明
func showConfigExplanation() {
	pterm.DefaultSection.Println("CNI Configuration Explanation")

	explanation := pterm.TableData{
		{"Component", "Purpose", "Why Required"},
		{"DNS", "Service Discovery", "Pod 需要解析 Kubernetes 服务名和外部域名"},
		{"Service CIDR", "Virtual Network", "Kubernetes 服务使用虚拟 IP 进行负载均衡"},
		{"Pod CIDR", "Pod Network", "为每个 Pod 分配唯一 IP 地址"},
		{"Headscale URL", "Authentication", "CNI 插件向 Headscale 注册和获取网络信息"},
		{"Tailscale Socket", "Local Communication", "与本地 Tailscale 守护进程通信"},
	}

	pterm.DefaultTable.WithHasHeader().WithData(explanation).Render()

	pterm.Info.Println("💡 Tip: 敏感配置（如 headscale_url）可以通过环境变量设置，避免在配置文件中暴露")
}

// formatDuration 格式化持续时间
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
}

// isPodReady 检查Pod是否就绪
func isPodReady(podName, namespace string) bool {
	cmd := exec.Command("kubectl", "get", "pod", podName, "-n", namespace,
		"-o", "jsonpath={.status.conditions[?(@.type=='Ready')].status}")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "True"
}

// waitForPodReady 等待Pod就绪
func waitForPodReady(podName, namespace string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isPodReady(podName, namespace) {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout waiting for pod %s to be ready", podName)
}

// getPodIP 获取Pod IP
func getPodIP(podName, namespace string) (string, error) {
	cmd := exec.Command("kubectl", "get", "pod", podName, "-n", namespace,
		"-o", "jsonpath={.status.podIP}")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// getServiceIP 获取Service IP
func getServiceIP(serviceName, namespace string) (string, error) {
	cmd := exec.Command("kubectl", "get", "service", serviceName, "-n", namespace,
		"-o", "jsonpath={.spec.clusterIP}")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
