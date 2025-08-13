package commands

import (
	"fmt"
	"os/exec"
)

// showLogo 显示 HeadCNI ASCII logo
func showLogo() {
	logo := `
██╗  ██╗███████╗ █████╗ ██████╗  ██████╗███╗   ██╗██╗
██║  ██║██╔════╝██╔══██╗██╔══██╗██╔════╝████╗  ██║██║
███████║█████╗  ███████║██║  ██║██║     ██╔██╗ ██║██║
██╔══██║██╔══╝  ██╔══██║██║  ██║██║     ██║╚██╗██║██║
██║  ██║███████╗██║  ██║██████╔╝╚██████╗██║ ╚████║██║
╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚═════╝  ╚═════╝╚═╝  ╚═══╝╚═╝
                                                      
    Kubernetes CNI Plugin for Headscale/Tailscale    
    ================================================
`
	fmt.Print(logo)
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
