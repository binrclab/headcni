package k8s

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/binrclab/headcni/pkg/logging"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ServiceInfo 服务信息
type ServiceInfo struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	ClusterIP string `json:"clusterIP"`
	Port      int32  `json:"port"`
}

// ServiceDomainInfo 服务域名信息
type ServiceDomainInfo struct {
	ServiceName      string   `json:"serviceName"`
	ServiceNamespace string   `json:"serviceNamespace"`
	ClusterDomain    string   `json:"clusterDomain"`
	FullDomain       string   `json:"fullDomain"`
	ShortDomain      string   `json:"shortDomain"`
	SearchDomains    []string `json:"searchDomains"`
}

// GetClusterDomain 获取集群域名
func GetClusterDomain() string {
	// 1. 尝试从环境变量获取
	if domain := os.Getenv("CLUSTER_DOMAIN"); domain != "" {
		return domain
	}

	// 2. 尝试从 kubelet 配置获取
	if domain := getClusterDomainFromKubelet(); domain != "" {
		return domain
	}

	// 3. 尝试从 Kubernetes API 获取
	if domain := getClusterDomainFromK8s(); domain != "" {
		return domain
	}

	// 4. 默认值
	return "cluster.local"
}

// GetServiceDomain 获取服务的完整域名
func GetServiceDomain(serviceName, namespace string) string {
	clusterDomain := GetClusterDomain()

	// 格式: <service-name>.<namespace>.svc.<cluster-domain>
	return fmt.Sprintf("%s.%s.svc.%s", serviceName, namespace, clusterDomain)
}

// GetServiceShortDomain 获取服务的短域名
func GetServiceShortDomain(serviceName, namespace string) string {
	// 格式: <service-name>.<namespace>.svc
	return fmt.Sprintf("%s.%s.svc", serviceName, namespace)
}

// GetServiceDomainInfo 获取服务的完整域名信息
func GetServiceDomainInfo(serviceName, namespace string) *ServiceDomainInfo {
	clusterDomain := GetClusterDomain()

	return &ServiceDomainInfo{
		ServiceName:      serviceName,
		ServiceNamespace: namespace,
		ClusterDomain:    clusterDomain,
		FullDomain:       GetServiceDomain(serviceName, namespace),
		ShortDomain:      GetServiceShortDomain(serviceName, namespace),
		SearchDomains:    getSearchDomains(clusterDomain),
	}
}

// GetServiceInfo 获取服务信息
func GetServiceInfo(serviceName, namespace string) (*ServiceInfo, error) {
	clientset, err := getK8sClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	service, err := clientset.CoreV1().Services(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get service %s/%s: %v", namespace, serviceName, err)
	}

	// 获取第一个端口
	var port int32
	if len(service.Spec.Ports) > 0 {
		port = service.Spec.Ports[0].Port
	}

	return &ServiceInfo{
		Name:      service.Name,
		Namespace: service.Namespace,
		ClusterIP: service.Spec.ClusterIP,
		Port:      port,
	}, nil
}

// GetDefaultSearchDomains 获取默认的搜索域名列表
func GetDefaultSearchDomains() []string {
	clusterDomain := GetClusterDomain()
	return getSearchDomains(clusterDomain)
}

// getSearchDomains 根据集群域名生成搜索域名列表
func getSearchDomains(clusterDomain string) []string {
	return []string{
		fmt.Sprintf("%s.svc.%s", "default", clusterDomain),
		fmt.Sprintf("svc.%s", clusterDomain),
		clusterDomain,
	}
}

// getClusterDomainFromKubelet 从 kubelet 配置获取集群域名
func getClusterDomainFromKubelet() string {
	// 尝试读取 kubelet 配置文件
	configPaths := []string{
		"/var/lib/kubelet/config.yaml",
		"/etc/kubernetes/kubelet.conf",
		"/var/lib/rancher/k3s/agent/etc/kubelet.config",
	}

	for _, path := range configPaths {
		if content, err := os.ReadFile(path); err == nil {
			// 简单的 YAML 解析，查找 clusterDomain
			lines := strings.Split(string(content), "\n")
			for _, line := range lines {
				if strings.Contains(line, "clusterDomain:") {
					parts := strings.SplitN(line, ":", 2)
					if len(parts) == 2 {
						domain := strings.TrimSpace(parts[1])
						if domain != "" {
							logging.Infof("Found cluster domain from kubelet config: %s", domain)
							return domain
						}
					}
				}
			}
		}
	}

	return ""
}

// getClusterDomainFromK8s 从 Kubernetes API 获取集群域名
func getClusterDomainFromK8s() string {
	clientset, err := getK8sClient()
	if err != nil {
		logging.Debugf("Failed to get kubernetes client: %v", err)
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// 尝试从 kube-system 命名空间的 ConfigMap 获取
	configMap, err := clientset.CoreV1().ConfigMaps("kube-system").Get(ctx, "kube-dns", metav1.GetOptions{})
	if err == nil {
		if domain, exists := configMap.Data["stubDomains"]; exists {
			logging.Infof("Found cluster domain from kube-dns ConfigMap: %s", domain)
			return domain
		}
	}

	// 尝试从 CoreDNS ConfigMap 获取
	configMap, err = clientset.CoreV1().ConfigMaps("kube-system").Get(ctx, "coredns", metav1.GetOptions{})
	if err == nil {
		if corefile, exists := configMap.Data["Corefile"]; exists {
			// 解析 Corefile 查找 cluster.local
			lines := strings.Split(corefile, "\n")
			for _, line := range lines {
				if strings.Contains(line, "cluster.local") {
					parts := strings.Fields(line)
					for _, part := range parts {
						if strings.Contains(part, "cluster.local") {
							logging.Infof("Found cluster domain from CoreDNS ConfigMap: %s", part)
							return part
						}
					}
				}
			}
		}
	}

	return ""
}

// GetServiceList 获取指定命名空间的服务列表
func GetServiceList(namespace string) ([]ServiceInfo, error) {
	clientset, err := getK8sClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	services, err := clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list services in namespace %s: %v", namespace, err)
	}

	var serviceList []ServiceInfo
	for _, service := range services.Items {
		var port int32
		if len(service.Spec.Ports) > 0 {
			port = service.Spec.Ports[0].Port
		}

		serviceList = append(serviceList, ServiceInfo{
			Name:      service.Name,
			Namespace: service.Namespace,
			ClusterIP: service.Spec.ClusterIP,
			Port:      port,
		})
	}

	return serviceList, nil
}

// GetServiceEndpoints 获取服务的端点信息
func GetServiceEndpoints(serviceName, namespace string) ([]string, error) {
	clientset, err := getK8sClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get kubernetes client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	endpoints, err := clientset.CoreV1().Endpoints(namespace).Get(ctx, serviceName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get endpoints for service %s/%s: %v", namespace, serviceName, err)
	}

	var addresses []string
	for _, subset := range endpoints.Subsets {
		for _, address := range subset.Addresses {
			addresses = append(addresses, address.IP)
		}
	}

	return addresses, nil
}
