# HeadCNI Kubernetes 包

这个包提供了与 Kubernetes 集群交互的功能，包括 DNS 服务发现、服务域名生成、集群信息获取等。

## 功能特性

### DNS 服务发现
- 动态获取 Kubernetes 集群中的 DNS 服务 IP
- 支持 CoreDNS 和 kube-dns
- 自动检测 K3s 集群
- 提供完整的 DNS 配置信息

### 服务域名生成
- 生成服务的完整域名和短域名
- 获取集群域名信息
- 生成搜索域名列表
- 支持服务发现和端点查询

### 集群信息获取
- 获取集群域名
- 检测集群类型（K3s vs 标准 Kubernetes）
- 获取服务列表和端点信息

## 主要函数

### DNS 相关
- `GetDNSServiceIP()` - 获取 DNS 服务 IP
- `GetDNSConfig()` - 获取完整 DNS 配置
- `GetDNSServiceInfo()` - 获取 DNS 服务详细信息
- `IsK3sCluster()` - 检测是否为 K3s 集群

### 服务域名相关
- `GetClusterDomain()` - 获取集群域名
- `GetServiceDomain(serviceName, namespace)` - 获取服务完整域名
- `GetServiceShortDomain(serviceName, namespace)` - 获取服务短域名
- `GetServiceDomainInfo(serviceName, namespace)` - 获取服务域名信息
- `GetDefaultSearchDomains()` - 获取默认搜索域名

### 服务发现相关
- `GetServiceInfo(serviceName, namespace)` - 获取服务信息
- `GetServiceList(namespace)` - 获取命名空间服务列表
- `GetServiceEndpoints(serviceName, namespace)` - 获取服务端点

## 使用示例

### 基本使用

```go
package main

import (
    "fmt"
    "github.com/binrclab/headcni/pkg/k8s"
)

func main() {
    // 获取 DNS 服务 IP
    dnsIP := k8s.GetDNSServiceIP()
    fmt.Printf("DNS Service IP: %s\n", dnsIP)

    // 获取集群域名
    clusterDomain := k8s.GetClusterDomain()
    fmt.Printf("Cluster Domain: %s\n", clusterDomain)

    // 生成服务域名
    fullDomain := k8s.GetServiceDomain("kubernetes", "default")
    fmt.Printf("Service Domain: %s\n", fullDomain)
}
```

### 在 CNI 配置中使用

```go
// 获取 DNS 配置
dnsIP := k8s.GetDNSServiceIP()
clusterDomain := k8s.GetClusterDomain()
searchDomains := k8s.GetDefaultSearchDomains()

// 构建 MagicDNS 配置
magicDNSConfig := map[string]interface{}{
    "enable": true,
    "base_domain": clusterDomain,
    "nameservers": []string{
        dnsIP,           // 集群 DNS
        "8.8.8.8",       // Google DNS
        "8.8.4.4",       // Google DNS 备用
    },
    "search_domains": searchDomains,
}
```

### 服务发现

```go
// 获取服务信息
serviceInfo, err := k8s.GetServiceInfo("kubernetes", "default")
if err == nil {
    fmt.Printf("Service: %s, IP: %s, Port: %d\n", 
        serviceInfo.Name, serviceInfo.ClusterIP, serviceInfo.Port)
}

// 获取服务端点
endpoints, err := k8s.GetServiceEndpoints("kubernetes", "default")
if err == nil {
    fmt.Printf("Endpoints: %v\n", endpoints)
}
```

## 配置优先级

### DNS 服务 IP 获取优先级
1. 环境变量（`KUBERNETES_DNS_SERVICE_IP`, `KUBE_DNS_SERVICE_IP`, `COREDNS_SERVICE_IP`, `CLUSTER_DNS`）
2. Kubernetes API（查询 CoreDNS 或 kube-dns Service）
3. 默认值（K3s: `10.43.0.10`, Kubernetes: `10.96.0.10`）

### 集群域名获取优先级
1. 环境变量（`CLUSTER_DOMAIN`）
2. kubelet 配置文件
3. Kubernetes API（查询 ConfigMap）
4. 默认值（`cluster.local`）

## 测试

运行测试：
```bash
go test ./pkg/k8s -v
```

运行基准测试：
```bash
go test ./pkg/k8s -bench=.
```

## 注意事项

1. **集群内运行**: 大部分功能需要在 Kubernetes 集群内运行才能正常工作
2. **权限要求**: 需要适当的 RBAC 权限来访问 Kubernetes API
3. **网络连接**: 需要能够连接到 Kubernetes API Server
4. **超时处理**: API 调用都有超时设置，避免长时间阻塞

## 错误处理

所有函数都包含适当的错误处理：
- API 调用失败时会返回错误信息
- 在测试环境中，某些功能可能无法正常工作，这是正常的
- 函数会优雅地处理网络超时和权限问题 