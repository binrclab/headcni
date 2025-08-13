package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

type StatusOptions struct {
	Namespace   string
	ReleaseName string
	Output      string
	ShowLogs    bool
}

type ClusterStatus struct {
	Nodes     []NodeStatus    `json:"nodes"`
	DaemonSet DaemonSetStatus `json:"daemonset"`
	Pods      []PodStatus     `json:"pods"`
	CNI       CNIStatus       `json:"cni"`
	Tailscale TailscaleStatus `json:"tailscale"`
}

type NodeStatus struct {
	Name     string `json:"name"`
	Ready    bool   `json:"ready"`
	CNIReady bool   `json:"cni_ready"`
	IP       string `json:"ip"`
}

type DaemonSetStatus struct {
	Name      string `json:"name"`
	Desired   int    `json:"desired"`
	Current   int    `json:"current"`
	Ready     int    `json:"ready"`
	Available int    `json:"available"`
	UpToDate  int    `json:"up_to_date"`
	Namespace string `json:"namespace"`
}

type PodStatus struct {
	Name     string `json:"name"`
	Node     string `json:"node"`
	Status   string `json:"status"`
	Ready    bool   `json:"ready"`
	Restarts int    `json:"restarts"`
	Age      string `json:"age"`
}

type CNIStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Config    string `json:"config"`
}

type TailscaleStatus struct {
	Connected bool   `json:"connected"`
	IP        string `json:"ip"`
	Status    string `json:"status"`
}

func NewStatusCommand() *cobra.Command {
	opts := &StatusOptions{}

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check HeadCNI deployment status",
		Long: `Check the status of HeadCNI deployment in your Kubernetes cluster.

This command will show:
- DaemonSet status
- Pod status across all nodes
- CNI plugin status
- Tailscale connectivity status

Examples:
  # Basic status check
  headcni status

  # Status with logs
  headcni status --show-logs

  # JSON output
  headcni status --output json

  # Custom namespace
  headcni status --namespace my-namespace`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(opts)
		},
	}

	cmd.Flags().StringVar(&opts.Namespace, "namespace", "kube-system", "Kubernetes namespace")
	cmd.Flags().StringVar(&opts.ReleaseName, "release-name", "headcni", "Helm release name")
	cmd.Flags().StringVar(&opts.Output, "output", "table", "Output format (table, json, yaml)")
	cmd.Flags().BoolVar(&opts.ShowLogs, "show-logs", false, "Show recent logs from pods")

	return cmd
}

func runStatus(opts *StatusOptions) error {
	// 显示 ASCII logo
	showLogo()

	fmt.Printf("🔍 Checking HeadCNI status in namespace: %s\n\n", opts.Namespace)

	status := &ClusterStatus{}

	// 检查集群连接
	if err := checkClusterConnection(); err != nil {
		return fmt.Errorf("cluster connection failed: %v", err)
	}

	// 获取 DaemonSet 状态
	if err := getDaemonSetStatus(opts, status); err != nil {
		return fmt.Errorf("failed to get DaemonSet status: %v", err)
	}

	// 获取 Pod 状态
	if err := getPodStatus(opts, status); err != nil {
		return fmt.Errorf("failed to get Pod status: %v", err)
	}

	// 获取节点状态
	if err := getNodeStatus(status); err != nil {
		return fmt.Errorf("failed to get node status: %v", err)
	}

	// 检查 CNI 状态
	if err := getCNIStatus(status); err != nil {
		return fmt.Errorf("failed to get CNI status: %v", err)
	}

	// 检查 Tailscale 状态
	if err := getTailscaleStatus(status); err != nil {
		return fmt.Errorf("failed to get Tailscale status: %v", err)
	}

	// 输出结果
	if err := outputStatus(status, opts); err != nil {
		return fmt.Errorf("failed to output status: %v", err)
	}

	// 显示日志（如果需要）
	if opts.ShowLogs {
		if err := showLogs(opts); err != nil {
			return fmt.Errorf("failed to show logs: %v", err)
		}
	}

	return nil
}

func getDaemonSetStatus(opts *StatusOptions, status *ClusterStatus) error {
	fmt.Printf("📊 DaemonSet Status:\n")

	cmd := exec.Command("kubectl", "get", "daemonset", opts.ReleaseName,
		"-n", opts.Namespace, "-o", "json")
	output, err := cmd.Output()
	if err != nil {
		status.DaemonSet.Name = opts.ReleaseName
		status.DaemonSet.Namespace = opts.Namespace
		fmt.Printf("❌ DaemonSet not found\n")
		return nil
	}

	var daemonSet map[string]interface{}
	if err := json.Unmarshal(output, &daemonSet); err != nil {
		return fmt.Errorf("failed to parse DaemonSet JSON: %v", err)
	}

	statusObj := daemonSet["status"].(map[string]interface{})
	status.DaemonSet.Name = opts.ReleaseName
	status.DaemonSet.Namespace = opts.Namespace
	status.DaemonSet.Desired = int(statusObj["desiredNumberScheduled"].(float64))
	status.DaemonSet.Current = int(statusObj["currentNumberScheduled"].(float64))
	status.DaemonSet.Ready = int(statusObj["numberReady"].(float64))
	status.DaemonSet.Available = int(statusObj["numberAvailable"].(float64))
	status.DaemonSet.UpToDate = int(statusObj["updatedNumberScheduled"].(float64))

	fmt.Printf("   Name: %s\n", status.DaemonSet.Name)
	fmt.Printf("   Desired: %d, Current: %d, Ready: %d, Available: %d, Up-to-date: %d\n",
		status.DaemonSet.Desired, status.DaemonSet.Current, status.DaemonSet.Ready,
		status.DaemonSet.Available, status.DaemonSet.UpToDate)

	if status.DaemonSet.Ready == status.DaemonSet.Desired {
		fmt.Printf("   ✅ DaemonSet is healthy\n")
	} else {
		fmt.Printf("   ⚠️  DaemonSet has issues\n")
	}

	fmt.Printf("\n")
	return nil
}

func getPodStatus(opts *StatusOptions, status *ClusterStatus) error {
	fmt.Printf("📦 Pod Status:\n")

	cmd := exec.Command("kubectl", "get", "pods",
		"-l", "app.kubernetes.io/name=headcni",
		"-n", opts.Namespace, "-o", "json")
	output, err := cmd.Output()
	if err != nil {
		fmt.Printf("❌ No pods found\n")
		return nil
	}

	var podList map[string]interface{}
	if err := json.Unmarshal(output, &podList); err != nil {
		return fmt.Errorf("failed to parse pod list JSON: %v", err)
	}

	pods := podList["items"].([]interface{})
	for _, pod := range pods {
		podObj := pod.(map[string]interface{})
		metadata := podObj["metadata"].(map[string]interface{})
		podStatus := podObj["status"].(map[string]interface{})

		podInfo := PodStatus{
			Name:     metadata["name"].(string),
			Node:     podStatus["hostIP"].(string),
			Status:   podStatus["phase"].(string),
			Restarts: 0,
		}

		// 检查容器状态
		containers := podStatus["containerStatuses"].([]interface{})
		for _, container := range containers {
			containerObj := container.(map[string]interface{})
			if restartCount, ok := containerObj["restartCount"].(float64); ok {
				podInfo.Restarts = int(restartCount)
			}
			if ready, ok := containerObj["ready"].(bool); ok {
				podInfo.Ready = ready
			}
		}

		status.Pods = append(status.Pods, podInfo)

		statusIcon := "❌"
		if podInfo.Status == "Running" && podInfo.Ready {
			statusIcon = "✅"
		} else if podInfo.Status == "Pending" {
			statusIcon = "⏳"
		}

		fmt.Printf("   %s %s (Node: %s) - Status: %s, Ready: %v, Restarts: %d\n",
			statusIcon, podInfo.Name, podInfo.Node, podInfo.Status, podInfo.Ready, podInfo.Restarts)
	}

	fmt.Printf("\n")
	return nil
}

func getNodeStatus(status *ClusterStatus) error {
	fmt.Printf("🖥️  Node Status:\n")

	cmd := exec.Command("kubectl", "get", "nodes", "-o", "json")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to get nodes: %v", err)
	}

	var nodeList map[string]interface{}
	if err := json.Unmarshal(output, &nodeList); err != nil {
		return fmt.Errorf("failed to parse node list JSON: %v", err)
	}

	nodes := nodeList["items"].([]interface{})
	for _, node := range nodes {
		nodeObj := node.(map[string]interface{})
		metadata := nodeObj["metadata"].(map[string]interface{})
		nodeStatus := nodeObj["status"].(map[string]interface{})

		nodeInfo := NodeStatus{
			Name: metadata["name"].(string),
		}

		// 检查节点就绪状态
		conditions := nodeStatus["conditions"].([]interface{})
		for _, condition := range conditions {
			condObj := condition.(map[string]interface{})
			if condObj["type"].(string) == "Ready" {
				nodeInfo.Ready = condObj["status"].(string) == "True"
				break
			}
		}

		// 获取节点IP
		if addresses, ok := nodeStatus["addresses"].([]interface{}); ok {
			for _, addr := range addresses {
				addrObj := addr.(map[string]interface{})
				if addrObj["type"].(string) == "InternalIP" {
					nodeInfo.IP = addrObj["address"].(string)
					break
				}
			}
		}

		status.Nodes = append(status.Nodes, nodeInfo)

		statusIcon := "❌"
		if nodeInfo.Ready {
			statusIcon = "✅"
		}

		fmt.Printf("   %s %s (IP: %s) - Ready: %v\n",
			statusIcon, nodeInfo.Name, nodeInfo.IP, nodeInfo.Ready)
	}

	fmt.Printf("\n")
	return nil
}

func getCNIStatus(status *ClusterStatus) error {
	fmt.Printf("🌐 CNI Status:\n")

	// 检查 CNI 二进制文件
	cmd := exec.Command("kubectl", "get", "nodes", "-o", "jsonpath={.items[0].status.nodeInfo.kubeletVersion}")
	output, err := cmd.Output()
	if err == nil {
		status.CNI.Installed = true
		status.CNI.Version = strings.TrimSpace(string(output))
		fmt.Printf("   ✅ CNI plugin installed\n")
		fmt.Printf("   Version: %s\n", status.CNI.Version)
	} else {
		status.CNI.Installed = false
		fmt.Printf("   ❌ CNI plugin not found\n")
	}

	// 检查 CNI 配置
	cmd = exec.Command("kubectl", "get", "configmap", "-n", "kube-system", "-o", "jsonpath={.items[?(@.metadata.name=='headcni-config')].metadata.name}")
	output, err = cmd.Output()
	if err == nil && len(output) > 0 {
		status.CNI.Config = "Configured"
		fmt.Printf("   ✅ CNI configuration found\n")
	} else {
		status.CNI.Config = "Not configured"
		fmt.Printf("   ❌ CNI configuration not found\n")
	}

	fmt.Printf("\n")
	return nil
}

func getTailscaleStatus(status *ClusterStatus) error {
	fmt.Printf("🔗 Tailscale Status:\n")

	// 尝试获取 Tailscale 状态
	cmd := exec.Command("tailscale", "status", "--json")
	output, err := cmd.Output()
	if err != nil {
		status.Tailscale.Connected = false
		status.Tailscale.Status = "Not connected"
		fmt.Printf("   ❌ Tailscale not connected\n")
		fmt.Printf("   Status: %s\n", status.Tailscale.Status)
	} else {
		var tailscaleStatus map[string]interface{}
		if err := json.Unmarshal(output, &tailscaleStatus); err == nil {
			if self, ok := tailscaleStatus["Self"].(map[string]interface{}); ok {
				if ips, ok := self["TailscaleIPs"].([]interface{}); ok && len(ips) > 0 {
					status.Tailscale.Connected = true
					status.Tailscale.IP = ips[0].(string)
					status.Tailscale.Status = "Connected"
					fmt.Printf("   ✅ Tailscale connected\n")
					fmt.Printf("   IP: %s\n", status.Tailscale.IP)
				}
			}
		}
	}

	fmt.Printf("\n")
	return nil
}

func outputStatus(status *ClusterStatus, opts *StatusOptions) error {
	switch opts.Output {
	case "json":
		output, err := json.MarshalIndent(status, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal status to JSON: %v", err)
		}
		fmt.Printf("%s\n", string(output))
	case "yaml":
		// 简单的 YAML 输出
		fmt.Printf("daemonset:\n")
		fmt.Printf("  name: %s\n", status.DaemonSet.Name)
		fmt.Printf("  ready: %d/%d\n", status.DaemonSet.Ready, status.DaemonSet.Desired)
		fmt.Printf("pods:\n")
		for _, pod := range status.Pods {
			fmt.Printf("  - name: %s\n", pod.Name)
			fmt.Printf("    status: %s\n", pod.Status)
			fmt.Printf("    ready: %v\n", pod.Ready)
		}
	default:
		// 默认表格输出已在各个函数中处理
	}

	return nil
}

func showLogs(opts *StatusOptions) error {
	fmt.Printf("📋 Recent Logs:\n")

	cmd := exec.Command("kubectl", "logs",
		"-l", "app.kubernetes.io/name=headcni",
		"-n", opts.Namespace,
		"--tail=50",
		"--all-containers=true")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to get logs: %v", err)
	}

	return nil
}
