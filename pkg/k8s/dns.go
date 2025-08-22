package k8s

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/binrclab/headcni/pkg/logging"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// DNSServiceInfo DNS 服务信息
type DNSServiceInfo struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	ClusterIP string `json:"clusterIP"`
	Type      string `json:"type"` // "coredns", "kube-dns", "other"
}

// DNSConfig DNS 配置
type DNSConfig struct {
	Nameservers   []string `json:"nameservers"`
	SearchDomains []string `json:"searchDomains"`
	ServiceIP     string   `json:"serviceIP"`
}

// GetDNSServiceIP 获取 DNS 服务 IP
func GetDNSServiceIP() string {
	// 1. 尝试从环境变量获取（最可靠）
	if dnsIP := getDNSServiceIPFromEnv(); dnsIP != "" {
		return dnsIP
	}

	// 2. 尝试从 Kubernetes API 获取
	if dnsIP := getDNSServiceIPFromK8s(); dnsIP != "" {
		return dnsIP
	}

	// 3. 根据集群类型返回默认值
	return getDefaultDNSServiceIP()
}

// GetDNSServiceInfo 获取 DNS 服务详细信息
func GetDNSServiceInfo() (*DNSServiceInfo, error) {
	clientset, err := getK8sClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 尝试获取 CoreDNS Service
	service, err := clientset.CoreV1().Services("kube-system").Get(ctx, "coredns", metav1.GetOptions{})
	if err == nil && service.Spec.ClusterIP != "" {
		return &DNSServiceInfo{
			Name:      service.Name,
			Namespace: service.Namespace,
			ClusterIP: service.Spec.ClusterIP,
			Type:      "coredns",
		}, nil
	}

	// 尝试获取 kube-dns Service
	service, err = clientset.CoreV1().Services("kube-system").Get(ctx, "kube-dns", metav1.GetOptions{})
	if err == nil && service.Spec.ClusterIP != "" {
		return &DNSServiceInfo{
			Name:      service.Name,
			Namespace: service.Namespace,
			ClusterIP: service.Spec.ClusterIP,
			Type:      "kube-dns",
		}, nil
	}

	return nil, fmt.Errorf("no DNS service found")
}

// GetDNSConfig 获取完整的 DNS 配置
func GetDNSConfig() (*DNSConfig, error) {
	dnsInfo, err := GetDNSServiceInfo()
	if err != nil {
		return nil, err
	}

	// 构建 DNS 配置
	config := &DNSConfig{
		Nameservers: []string{dnsInfo.ClusterIP},
		SearchDomains: []string{
			"default.svc.cluster.local",
			"svc.cluster.local",
			"cluster.local",
		},
		ServiceIP: dnsInfo.ClusterIP,
	}

	// 添加备用 DNS 服务器
	config.Nameservers = append(config.Nameservers, "8.8.8.8", "8.8.4.4")

	return config, nil
}

// getDNSServiceIPFromK8s 从 Kubernetes API 获取 DNS 服务 IP
func getDNSServiceIPFromK8s() string {
	clientset, err := getK8sClient()
	if err != nil {
		logging.Debugf("Failed to get kubernetes client: %v", err)
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// 尝试获取 CoreDNS Service
	service, err := clientset.CoreV1().Services("kube-system").Get(ctx, "coredns", metav1.GetOptions{})
	if err == nil && service.Spec.ClusterIP != "" {
		logging.Infof("Found CoreDNS Service IP: %s", service.Spec.ClusterIP)
		return service.Spec.ClusterIP
	}

	// 尝试获取 kube-dns Service
	service, err = clientset.CoreV1().Services("kube-system").Get(ctx, "kube-dns", metav1.GetOptions{})
	if err == nil && service.Spec.ClusterIP != "" {
		logging.Infof("Found kube-dns Service IP: %s", service.Spec.ClusterIP)
		return service.Spec.ClusterIP
	}

	return ""
}

// getDNSServiceIPFromEnv 从环境变量获取 DNS 服务 IP
func getDNSServiceIPFromEnv() string {
	// 常见的环境变量
	envVars := []string{
		"KUBERNETES_DNS_SERVICE_IP",
		"KUBE_DNS_SERVICE_IP",
		"COREDNS_SERVICE_IP",
		"CLUSTER_DNS",
	}

	for _, envVar := range envVars {
		if value := os.Getenv(envVar); value != "" {
			logging.Infof("Found DNS Service IP from environment %s: %s", envVar, value)
			return value
		}
	}

	return ""
}

// getDefaultDNSServiceIP 获取默认 DNS 服务 IP
func getDefaultDNSServiceIP() string {
	// 根据集群类型返回默认值
	if IsK3sCluster() {
		logging.Infof("Detected K3s cluster, using default DNS IP: 10.43.0.10")
		return "10.43.0.10"
	}

	logging.Infof("Using default Kubernetes DNS IP: 10.96.0.10")
	return "10.96.0.10"
}

// getK8sClient 获取 Kubernetes 客户端
func getK8sClient() (*kubernetes.Clientset, error) {
	// 尝试获取集群内配置
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get in-cluster config: %v", err)
	}

	return kubernetes.NewForConfig(config)
}

// IsK3sCluster 检测是否为 K3s 集群
func IsK3sCluster() bool {
	// 检查 K3s 特有的环境变量
	if os.Getenv("K3S_DATA_DIR") != "" {
		return true
	}
	if os.Getenv("K3S_KUBECONFIG_OUTPUT") != "" {
		return true
	}

	// 检查 K3s 特有的文件
	if _, err := os.Stat("/var/lib/rancher/k3s"); err == nil {
		return true
	}

	return false
}
