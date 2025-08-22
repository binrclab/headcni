# 获取节点的 node.Spec.PodCIDR

本文档详细说明如何获取 Kubernetes 节点的 `node.Spec.PodCIDR`，这是 HeadCNI 正确配置网络的关键信息。

## 概述

在 Kubernetes 集群中，每个节点都会被分配一个唯一的 Pod CIDR 子网。这个信息存储在节点的 `Spec.PodCIDR` 字段中，用于：

1. **CNI 配置**: 为节点上的 Pod 分配 IP 地址
2. **路由配置**: 设置正确的网络路由
3. **网络隔离**: 确保不同节点的 Pod 网络不冲突

## 获取方式

### 1. 通过 Kubernetes API（推荐）

#### 基本获取

```go
package main

import (
    "context"
    "fmt"
    
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"
)

func getNodePodCIDR(nodeName string) (string, error) {
    // 创建 Kubernetes 客户端
    config, err := rest.InClusterConfig()
    if err != nil {
        return "", fmt.Errorf("failed to create in-cluster config: %v", err)
    }
    
    clientset, err := kubernetes.NewForConfig(config)
    if err != nil {
        return "", fmt.Errorf("failed to create kubernetes client: %v", err)
    }
    
    // 获取节点信息
    node, err := clientset.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
    if err != nil {
        return "", fmt.Errorf("failed to get node %s: %v", nodeName, err)
    }
    
    // 优先从 PodCIDRs 数组获取（支持双栈）
    if len(node.Spec.PodCIDRs) > 0 {
        // 查找 IPv4 CIDR
        for _, cidr := range node.Spec.PodCIDRs {
            if strings.Contains(cidr, ".") { // IPv4
                return cidr, nil
            }
        }
    }
    
    // 从 PodCIDR 字段获取（单栈或旧版本）
    if node.Spec.PodCIDR != "" {
        return node.Spec.PodCIDR, nil
    }
    
    return "", fmt.Errorf("node %s has no PodCIDR assigned", nodeName)
}
```

#### 等待分配

```go
func waitForPodCIDRAssignment(nodeName string, timeout time.Duration) (string, error) {
    config, err := rest.InClusterConfig()
    if err != nil {
        return "", err
    }
    
    clientset, err := kubernetes.NewForConfig(config)
    if err != nil {
        return "", err
    }
    
    // 使用 Watch API 监听节点变化
    watcher, err := clientset.CoreV1().Nodes().Watch(context.Background(), metav1.ListOptions{
        FieldSelector: fmt.Sprintf("metadata.name=%s", nodeName),
    })
    if err != nil {
        return "", err
    }
    defer watcher.Stop()
    
    timeoutCh := time.After(timeout)
    
    for {
        select {
        case event := <-watcher.ResultChan():
            if event.Type == watch.Modified || event.Type == watch.Added {
                if node, ok := event.Object.(*corev1.Node); ok {
                    if len(node.Spec.PodCIDRs) > 0 {
                        for _, cidr := range node.Spec.PodCIDRs {
                            if strings.Contains(cidr, ".") {
                                return cidr, nil
                            }
                        }
                    }
                    if node.Spec.PodCIDR != "" {
                        return node.Spec.PodCIDR, nil
                    }
                }
            }
        case <-timeoutCh:
            return "", fmt.Errorf("timeout waiting for PodCIDR assignment")
        }
    }
}
```

### 2. 通过 kubectl 命令

#### 查看节点信息

```bash
# 查看所有节点的 PodCIDR
kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.podCIDR}{"\n"}{end}'

# 查看特定节点的 PodCIDR
kubectl get node <node-name> -o jsonpath='{.spec.podCIDR}'

# 查看节点的详细信息
kubectl describe node <node-name>
```

#### 示例输出

```bash
$ kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.podCIDR}{"\n"}{end}'
worker-1	10.244.1.0/24
worker-2	10.244.2.0/24
master-1	10.244.0.0/24
```

### 3. 通过本地文件读取

#### k3s 环境

```bash
# 查看 k3s 节点配置
cat /var/lib/rancher/k3s/agent/etc/node-$(hostname).yaml | grep podCIDR

# 查看 k3s 主配置
cat /etc/rancher/k3s/k3s.yaml | grep cluster-cidr
```

#### 标准 Kubernetes 环境

```bash
# 查看 kubelet 配置
cat /var/lib/kubelet/config.yaml | grep podCIDR

# 查看 kubeadm 配置
cat /etc/kubernetes/kubeadm-config.yaml | grep podSubnet
```

### 4. 通过环境变量

```bash
# 设置环境变量
export NODE_POD_CIDR="10.244.1.0/24"

# 在代码中读取
podCIDR := os.Getenv("NODE_POD_CIDR")
```

## HeadCNI 中的实现

### 优先级顺序

HeadCNI 按照以下优先级获取 PodCIDR：

1. **配置文件**: `config.network.podCIDR.nodeLocal`
2. **环境变量**: `NODE_POD_CIDR`
3. **Kubernetes API**: 通过 `node.Spec.PodCIDR` 获取
4. **本地文件**: 从 kubelet/k3s 配置文件读取
5. **哈希计算**: 使用主机名哈希作为备用
6. **默认值**: `10.244.0.0/24`

### 代码实现

```go
func getNodePodCIDR(config *Config) (string, error) {
    // 1. 检查配置
    if config.PodCIDR != "" {
        return config.PodCIDR, nil
    }
    
    // 2. 检查环境变量
    if podCIDR := os.Getenv("NODE_POD_CIDR"); podCIDR != "" {
        return podCIDR, nil
    }
    
    // 3. 尝试 Kubernetes API
    if k8sClient, err := NewK8sClient(config.Logger); err == nil {
        if podCIDR, err := k8sClient.GetNodePodCIDR(); err == nil {
            return podCIDR, nil
        }
    }
    
    // 4. 尝试本地文件
    if podCIDR, err := getPodCIDRFromLocalFile(config); err == nil {
        return podCIDR, nil
    }
    
    // 5. 使用默认值
    return "10.244.0.0/24", nil
}
```

## 验证和调试

### 验证 PodCIDR 格式

```go
func validatePodCIDR(podCIDR string) error {
    _, _, err := net.ParseCIDR(podCIDR)
    if err != nil {
        return fmt.Errorf("invalid PodCIDR format: %v", err)
    }
    return nil
}
```

### 调试命令

```bash
# 检查节点状态
kubectl get nodes -o wide

# 查看节点详细信息
kubectl get node <node-name> -o yaml

# 检查 kubelet 日志
journalctl -u kubelet | grep podCIDR

# 检查 CNI 配置
ls -la /etc/cni/net.d/
cat /etc/cni/net.d/*.conflist
```

### 常见问题

1. **PodCIDR 未分配**
   ```bash
   # 检查 kube-controller-manager 配置
   kubectl get pods -n kube-system | grep controller-manager
   kubectl logs -n kube-system kube-controller-manager-<pod-name> | grep cluster-cidr
   ```

2. **节点无法访问 API Server**
   ```bash
   # 检查网络连通性
   kubectl get nodes
   kubectl describe node <node-name>
   ```

3. **配置文件不存在**
   ```bash
   # 检查文件是否存在
   ls -la /var/lib/kubelet/config.yaml
   ls -la /etc/rancher/k3s/k3s.yaml
   ```

## 最佳实践

1. **明确配置**: 在生产环境中，建议明确指定 PodCIDR
2. **监控日志**: 定期检查获取过程的日志
3. **备用方案**: 提供多种获取方式作为备用
4. **验证格式**: 确保获取的 PodCIDR 格式正确
5. **错误处理**: 优雅处理获取失败的情况

## 总结

获取节点的 `node.Spec.PodCIDR` 是 Kubernetes 网络插件的关键功能。HeadCNI 提供了多种获取方式，确保在各种环境下都能正确获取 PodCIDR 信息，从而正确配置 CNI 网络。 