package commands

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

type LogsOptions struct {
	Namespace   string
	ReleaseName string
	Follow      bool
	Tail        int
	Since       string
	Container   string
	Previous    bool
	Timestamps  bool
}

func NewLogsCommand() *cobra.Command {
	opts := &LogsOptions{}

	cmd := &cobra.Command{
		Use:   "logs [pod-name]",
		Short: "View logs from HeadCNI components",
		Long: `View logs from HeadCNI components in your Kubernetes cluster.

This command allows you to view logs from:
- HeadCNI DaemonSet pods
- HeadCNI IPAM pods
- Specific containers within pods

Examples:
  # View logs from all HeadCNI pods
  headcni logs

  # Follow logs from a specific pod
  headcni logs headcni-abc123 --follow

  # View last 100 lines with timestamps
  headcni logs --tail 100 --timestamps

  # View logs from a specific container
  headcni logs --container headcni-daemon

  # View logs since a specific time
  headcni logs --since 1h`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogs(opts, args)
		},
	}

	cmd.Flags().StringVar(&opts.Namespace, "namespace", "kube-system", "Kubernetes namespace")
	cmd.Flags().StringVar(&opts.ReleaseName, "release-name", "headcni", "Helm release name")
	cmd.Flags().BoolVar(&opts.Follow, "follow", false, "Follow log output")
	cmd.Flags().IntVar(&opts.Tail, "tail", 0, "Number of lines to show from the end of the logs")
	cmd.Flags().StringVar(&opts.Since, "since", "", "Show logs since timestamp (e.g. 42m for 42 minutes)")
	cmd.Flags().StringVar(&opts.Container, "container", "", "Container name within the pod")
	cmd.Flags().BoolVar(&opts.Previous, "previous", false, "Show previous container logs")
	cmd.Flags().BoolVar(&opts.Timestamps, "timestamps", false, "Include timestamps on each line")

	return cmd
}

func runLogs(opts *LogsOptions, args []string) error {
	// 显示 ASCII logo
	showLogo()

	fmt.Printf("📋 Viewing HeadCNI logs...\n")
	fmt.Printf("Namespace: %s\n", opts.Namespace)
	fmt.Printf("Release Name: %s\n\n", opts.ReleaseName)

	// 检查集群连接
	if err := checkClusterConnection(); err != nil {
		return fmt.Errorf("cluster connection failed: %v", err)
	}

	// 如果指定了具体的pod名称
	if len(args) > 0 {
		return viewPodLogs(opts, args[0])
	}

	// 获取所有HeadCNI pods
	pods, err := getHeadCNIPods(opts.Namespace, opts.ReleaseName)
	if err != nil {
		return fmt.Errorf("failed to get HeadCNI pods: %v", err)
	}

	if len(pods) == 0 {
		fmt.Println("❌ No HeadCNI pods found in the cluster")
		return nil
	}

	fmt.Printf("Found %d HeadCNI pods:\n", len(pods))
	for i, pod := range pods {
		fmt.Printf("  %d. %s (%s)\n", i+1, pod.Name, pod.Status)
	}
	fmt.Println()

	// 如果只有一个pod，直接显示其日志
	if len(pods) == 1 {
		return viewPodLogs(opts, pods[0].Name)
	}

	// 如果有多个pod，显示选择菜单
	fmt.Println("Select a pod to view logs (or press Enter to view all):")
	var selection string
	fmt.Print("Enter pod number or name: ")
	fmt.Scanln(&selection)

	if selection == "" {
		// 显示所有pods的日志
		return viewAllPodsLogs(opts, pods)
	}

	// 尝试按数字选择
	if podIndex := parsePodSelection(selection, len(pods)); podIndex >= 0 {
		return viewPodLogs(opts, pods[podIndex].Name)
	}

	// 尝试按名称选择
	for _, pod := range pods {
		if strings.Contains(pod.Name, selection) {
			return viewPodLogs(opts, pod.Name)
		}
	}

	return fmt.Errorf("invalid pod selection: %s", selection)
}

func viewPodLogs(opts *LogsOptions, podName string) error {
	args := []string{"logs", podName, "-n", opts.Namespace}

	if opts.Follow {
		args = append(args, "-f")
	}

	if opts.Tail > 0 {
		args = append(args, "--tail", fmt.Sprintf("%d", opts.Tail))
	}

	if opts.Since != "" {
		args = append(args, "--since", opts.Since)
	}

	if opts.Container != "" {
		args = append(args, "-c", opts.Container)
	}

	if opts.Previous {
		args = append(args, "-p")
	}

	if opts.Timestamps {
		args = append(args, "--timestamps")
	}

	cmd := exec.Command("kubectl", args...)
	cmd.Stdout = cmd.Stdout
	cmd.Stderr = cmd.Stderr

	fmt.Printf("📋 Viewing logs for pod: %s\n", podName)
	fmt.Printf("Command: kubectl %s\n\n", strings.Join(args, " "))

	return cmd.Run()
}

func viewAllPodsLogs(opts *LogsOptions, pods []PodStatus) error {
	fmt.Println("📋 Viewing logs for all HeadCNI pods...")
	fmt.Println("Press Ctrl+C to stop following logs")
	fmt.Println()

	for _, pod := range pods {
		fmt.Printf("=== Logs from %s ===\n", pod.Name)

		args := []string{"logs", pod.Name, "-n", opts.Namespace}

		if opts.Follow {
			args = append(args, "-f")
		}

		if opts.Tail > 0 {
			args = append(args, "--tail", fmt.Sprintf("%d", opts.Tail))
		}

		if opts.Since != "" {
			args = append(args, "--since", opts.Since)
		}

		if opts.Timestamps {
			args = append(args, "--timestamps")
		}

		cmd := exec.Command("kubectl", args...)
		output, err := cmd.Output()
		if err != nil {
			fmt.Printf("Error getting logs for %s: %v\n", pod.Name, err)
			continue
		}

		if len(output) > 0 {
			fmt.Println(string(output))
		} else {
			fmt.Println("No logs available")
		}
		fmt.Println()
	}

	return nil
}

func getHeadCNIPods(namespace, releaseName string) ([]PodStatus, error) {
	cmd := exec.Command("kubectl", "get", "pods", "-n", namespace,
		"-l", fmt.Sprintf("app=%s", releaseName), "-o", "json")

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, err
	}

	items := result["items"].([]interface{})
	var pods []PodStatus

	for _, item := range items {
		pod := item.(map[string]interface{})
		metadata := pod["metadata"].(map[string]interface{})
		status := pod["status"].(map[string]interface{})

		podStatus := PodStatus{
			Name:   metadata["name"].(string),
			Status: status["phase"].(string),
		}

		pods = append(pods, podStatus)
	}

	return pods, nil
}

func parsePodSelection(selection string, maxPods int) int {
	var podIndex int
	if _, err := fmt.Sscanf(selection, "%d", &podIndex); err == nil {
		if podIndex >= 1 && podIndex <= maxPods {
			return podIndex - 1
		}
	}
	return -1
}
