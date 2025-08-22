package commands

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

type MetricsOptions struct {
	Namespace   string
	ReleaseName string
	Port        int
	Host        string
	Timeout     int
	Output      string
	Filter      string
}

type MetricData struct {
	Name   string            `json:"name"`
	Value  float64           `json:"value"`
	Labels map[string]string `json:"labels"`
	Type   string            `json:"type"`
	Help   string            `json:"help"`
}

func NewMetricsCommand() *cobra.Command {
	opts := &MetricsOptions{}

	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "View Prometheus metrics from HeadCNI",
		Long: `View Prometheus metrics from HeadCNI components.

This command allows you to:
- View real-time metrics from HeadCNI daemon
- Filter metrics by name or labels
- Export metrics in different formats
- Monitor HeadCNI performance and health

Examples:
  # View all metrics
  headcni metrics

  # View specific metrics
  headcni metrics --filter "headcni_ip"

  # Export metrics to JSON
  headcni metrics --output json

  # Use custom port
  headcni metrics --port 9090`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMetrics(opts)
		},
	}

	cmd.Flags().StringVar(&opts.Namespace, "namespace", "kube-system", "Kubernetes namespace")
	cmd.Flags().StringVar(&opts.ReleaseName, "release-name", "headcni", "Helm release name")
	cmd.Flags().IntVar(&opts.Port, "port", 9090, "Metrics port")
	cmd.Flags().StringVar(&opts.Host, "host", "localhost", "Metrics host")
	cmd.Flags().IntVar(&opts.Timeout, "timeout", 10, "Request timeout in seconds")
	cmd.Flags().StringVar(&opts.Output, "output", "table", "Output format (table, json, prometheus)")
	cmd.Flags().StringVar(&opts.Filter, "filter", "", "Filter metrics by name or labels")

	return cmd
}

func runMetrics(opts *MetricsOptions) error {
	// 显示 ASCII logo
	showLogo()

	fmt.Printf("📊 Viewing HeadCNI metrics...\n")
	fmt.Printf("Namespace: %s\n", opts.Namespace)
	fmt.Printf("Release Name: %s\n", opts.ReleaseName)
	fmt.Printf("Metrics Endpoint: %s:%d\n\n", opts.Host, opts.Port)

	// 检查集群连接
	if err := checkClusterConnection(); err != nil {
		return fmt.Errorf("cluster connection failed: %v", err)
	}

	// 获取HeadCNI pods
	pods, err := getHeadCNIPods(opts.Namespace, opts.ReleaseName)
	if err != nil {
		return fmt.Errorf("failed to get HeadCNI pods: %v", err)
	}

	if len(pods) == 0 {
		fmt.Println("❌ No HeadCNI pods found in the cluster")
		return nil
	}

	// 尝试端口转发
	fmt.Printf("🔗 Setting up port forward to access metrics...\n")

	// 选择第一个可用的pod
	targetPod := pods[0].Name

	// 启动端口转发
	portForwardCmd := exec.Command("kubectl", "port-forward",
		fmt.Sprintf("pod/%s", targetPod),
		fmt.Sprintf("%d:%d", opts.Port, opts.Port),
		"-n", opts.Namespace)

	// 在后台启动端口转发
	if err := portForwardCmd.Start(); err != nil {
		return fmt.Errorf("failed to start port forward: %v", err)
	}

	// 等待端口转发建立
	time.Sleep(2 * time.Second)

	// 确保在函数结束时停止端口转发
	defer func() {
		if portForwardCmd.Process != nil {
			portForwardCmd.Process.Kill()
		}
	}()

	// 获取指标
	metrics, err := fetchMetrics(opts)
	if err != nil {
		return fmt.Errorf("failed to fetch metrics: %v", err)
	}

	// 过滤指标
	if opts.Filter != "" {
		metrics = filterMetrics(metrics, opts.Filter)
	}

	// 显示指标
	return displayMetrics(metrics, opts.Output)
}

func fetchMetrics(opts *MetricsOptions) ([]MetricData, error) {
	client := &http.Client{
		Timeout: time.Duration(opts.Timeout) * time.Second,
	}

	url := fmt.Sprintf("http://%s:%d/metrics", opts.Host, opts.Port)

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metrics from %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metrics endpoint returned status %d", resp.StatusCode)
	}

	// 解析Prometheus格式的指标
	metrics, err := parsePrometheusMetrics(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse metrics: %v", err)
	}

	return metrics, nil
}

func parsePrometheusMetrics(body io.Reader) ([]MetricData, error) {
	scanner := bufio.NewScanner(body)
	var metrics []MetricData

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// 跳过注释和空行
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// 解析指标行
		metric, err := parseMetricLine(line)
		if err != nil {
			continue // 跳过无法解析的行
		}

		metrics = append(metrics, metric)
	}

	return metrics, scanner.Err()
}

func parseMetricLine(line string) (MetricData, error) {
	// 简单的Prometheus指标解析
	// 格式: metric_name{label="value"} value

	parts := strings.Split(line, " ")
	if len(parts) != 2 {
		return MetricData{}, fmt.Errorf("invalid metric format")
	}

	metricPart := parts[0]
	valuePart := parts[1]

	// 解析指标名称和标签
	var name string
	labels := make(map[string]string)

	if strings.Contains(metricPart, "{") {
		// 有标签的指标
		labelStart := strings.Index(metricPart, "{")
		labelEnd := strings.LastIndex(metricPart, "}")

		if labelStart == -1 || labelEnd == -1 {
			return MetricData{}, fmt.Errorf("invalid label format")
		}

		name = metricPart[:labelStart]
		labelStr := metricPart[labelStart+1 : labelEnd]

		// 解析标签
		labelPairs := strings.Split(labelStr, ",")
		for _, pair := range labelPairs {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}

			kv := strings.SplitN(pair, "=", 2)
			if len(kv) == 2 {
				key := strings.TrimSpace(kv[0])
				value := strings.Trim(strings.TrimSpace(kv[1]), `"`)
				labels[key] = value
			}
		}
	} else {
		// 无标签的指标
		name = metricPart
	}

	// 解析值
	value, err := strconv.ParseFloat(valuePart, 64)
	if err != nil {
		return MetricData{}, fmt.Errorf("invalid metric value: %v", err)
	}

	return MetricData{
		Name:   name,
		Value:  value,
		Labels: labels,
		Type:   "gauge", // 默认类型
	}, nil
}

func filterMetrics(metrics []MetricData, filter string) []MetricData {
	var filtered []MetricData

	for _, metric := range metrics {
		if strings.Contains(strings.ToLower(metric.Name), strings.ToLower(filter)) {
			filtered = append(filtered, metric)
			continue
		}

		// 检查标签
		for key, value := range metric.Labels {
			if strings.Contains(strings.ToLower(key), strings.ToLower(filter)) ||
				strings.Contains(strings.ToLower(value), strings.ToLower(filter)) {
				filtered = append(filtered, metric)
				break
			}
		}
	}

	return filtered
}

func displayMetrics(metrics []MetricData, outputFormat string) error {
	switch outputFormat {
	case "json":
		return displayMetricsJSON(metrics)
	case "prometheus":
		return displayMetricsPrometheus(metrics)
	default:
		return displayMetricsTable(metrics)
	}
}

func displayMetricsTable(metrics []MetricData) error {
	if len(metrics) == 0 {
		fmt.Println("No metrics found")
		return nil
	}

	fmt.Printf("📊 Found %d metrics:\n\n", len(metrics))

	// 创建表格数据
	tableData := [][]string{
		{"Metric Name", "Value", "Labels"},
	}

	for _, metric := range metrics {
		labelsStr := ""
		if len(metric.Labels) > 0 {
			var labelPairs []string
			for k, v := range metric.Labels {
				labelPairs = append(labelPairs, fmt.Sprintf("%s=%s", k, v))
			}
			labelsStr = strings.Join(labelPairs, ", ")
		}

		tableData = append(tableData, []string{
			metric.Name,
			fmt.Sprintf("%.2f", metric.Value),
			labelsStr,
		})
	}

	// 使用pterm显示表格
	pterm.DefaultTable.WithHasHeader().WithData(tableData).Render()

	return nil
}

func displayMetricsJSON(metrics []MetricData) error {
	jsonData, err := json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metrics to JSON: %v", err)
	}

	fmt.Println(string(jsonData))
	return nil
}

func displayMetricsPrometheus(metrics []MetricData) error {
	for _, metric := range metrics {
		if len(metric.Labels) > 0 {
			var labelPairs []string
			for k, v := range metric.Labels {
				labelPairs = append(labelPairs, fmt.Sprintf(`%s="%s"`, k, v))
			}
			fmt.Printf("%s{%s} %f\n", metric.Name, strings.Join(labelPairs, ","), metric.Value)
		} else {
			fmt.Printf("%s %f\n", metric.Name, metric.Value)
		}
	}
	return nil
}
