# Kubernetes 集成指南

## 概述

HeadCNI Daemon 需要获取当前节点的 Pod CIDR 信息来正确配置 CNI。本文档说明 HeadCNI 如何采用现代 Kubernetes 网络插件的最佳实践，参考 Flannel 的实现方式。

## 现代架构设计

### 参考 Flannel 的最佳实践

现代 Kubernetes 网络插件（如 Flannel）采用以下架构：

1. **Master Node**: kube-controller-manager 自动分配 PodCIDR 给每个节点
2. **Worker Node**: 网络插件通过本地文件或环境变量获取 PodCIDR
3. **不直接连接 etcd**: 所有 API 访问都通过 kube-apiserver 统一管理

### HeadCNI 的设计原则

- ✅ 优先使用本地文件读取
- ✅ 支持环境变量配置
- ✅ 提供智能备用计算
- ✅ 不依赖 Kubernetes API
- ✅ 适用于所有环境（包括 worker 节点）

## 解决方案

### 方案 1: 环境变量注入（推荐）

通过 Helm Chart 的 `values.yaml` 直接配置 Pod CIDR：

```yaml
config:
  network:
    podCIDR:
      base: "10.244.0.0/16"  # 集群的 Pod CIDR
      perNode: "/24"         # 每个节点的子网大小
    serviceCIDR: "10.96.0.0/16"
```

**优点**:
- 简单可靠
- 不依赖 Kubernetes API
- 适用于所有环境

**缺点**:
- 需要手动配置
- 不支持动态分配

### 方案 2: 环境变量注入

通过环境变量直接指定节点本地 Pod CIDR：

```yaml
config:
  network:
    podCIDR:
      nodeLocal: "10.244.1.0/24"  # 当前节点的本地 Pod CIDR
```

或者在部署时指定：

```bash
helm install headcni ./headcni/chart \
  --set config.network.podCIDR.nodeLocal="10.244.1.0/24"
```

**优点**:
- 简单可靠
- 不依赖网络
- 适用于所有环境

**缺点**:
- 需要手动配置
- 不支持动态分配

### 方案 3: 本地文件读取 + 智能计算（备用）

当 Kubernetes API 不可用时，会尝试从本地文件读取集群 Pod CIDR，然后智能计算当前节点的本地 CIDR：

#### 步骤 1: 读取集群 Pod CIDR
从以下配置文件读取集群的 Pod CIDR：

**k3s 配置文件**
1. **k3s 主配置**: `/etc/rancher/k3s/k3s.yaml`
2. **k3s kubelet 配置**: `/var/lib/rancher/k3s/agent/etc/kubelet.config`

**标准 Kubernetes 配置文件**
3. **kubelet 配置**: `/var/lib/kubelet/config.yaml`
4. **kubeadm 配置**: `/etc/kubernetes/kubeadm-config.yaml`
5. **kubeadm admin 配置**: `/etc/kubernetes/admin.conf`

**其他配置文件**
6. **controller manager manifest**: `/etc/kubernetes/manifests/kube-controller-manager.yaml`
7. **api server manifest**: `/etc/kubernetes/manifests/kube-apiserver.yaml`

#### 步骤 2: 获取节点本地 CIDR
优先从本地文件读取节点实际分配的 Pod CIDR：

1. **kubelet 配置**: `/var/lib/kubelet/config.yaml` 中的 `podCIDR` 字段
2. **k3s 节点配置**: `/var/lib/rancher/k3s/agent/etc/node-{nodeName}.yaml`
3. **环境变量**: `NODE_POD_CIDR` 环境变量
4. **哈希计算**: 如果都无法获取，使用主机名哈希计算（可能不准确）

**优点**:
- 不依赖网络
- 自动降级
- 智能计算节点本地 CIDR

**缺点**:
- 依赖本地文件存在
- 计算逻辑可能与实际分配不完全一致

## 配置优先级

Pod CIDR 的获取优先级如下：

1. **配置文件**: `config.network.podCIDR.nodeLocal`
2. **环境变量**: `NODE_POD_CIDR`
3. **本地文件**: kubelet/k3s 配置文件
4. **哈希计算**: 使用主机名哈希作为备用（可能不准确）
5. **默认值**: `10.244.0.0/16`

## 节点本地 CIDR 获取优先级

1. **kubelet 配置文件**: `/var/lib/kubelet/config.yaml`
   ```yaml
   podCIDR: "10.244.1.0/24"
   ```

2. **k3s 节点配置**: `/var/lib/rancher/k3s/agent/etc/node-{nodeName}.yaml`
   ```yaml
   podCIDR: "10.42.2.0/24"
   ```

3. **环境变量**: `NODE_POD_CIDR`
   ```bash
   export NODE_POD_CIDR="10.244.3.0/24"
   ```

4. **哈希计算**: 使用主机名哈希作为备用（可能不准确）

## 部署示例

### 基本部署（环境变量方式）

```bash
helm install headcni ./headcni/chart \
  --set config.network.podCIDR.base="10.244.0.0/16" \
  --set config.network.serviceCIDR="10.96.0.0/16"
```

### 指定节点本地 CIDR

```bash
helm install headcni ./headcni/chart \
  --set config.network.podCIDR.nodeLocal="10.244.1.0/24"
```

### 混合模式

```bash
helm install headcni ./headcni/chart \
  --set config.network.podCIDR.nodeLocal="10.244.1.0/24" \
  --set serviceAccount.create=true  # 启用 RBAC（如果需要其他功能）
```

### k3s 部署示例

对于 k3s 集群，推荐使用环境变量方式：

```bash
helm install headcni ./headcni/chart \
  --set config.network.podCIDR.base="10.42.0.0/16" \
  --set config.network.serviceCIDR="10.43.0.0/16"
```

**注意**: k3s 默认使用不同的 CIDR 范围：
- Pod CIDR: `10.42.0.0/16`
- Service CIDR: `10.43.0.0/16`

## 故障排除

### 1. 检查 Pod CIDR 获取

查看 daemon 日志：

```bash
kubectl logs -n kube-system -l app=headcni-daemon
```

查找以下日志：
- `"Using Pod CIDR from config"`
- `"Using Pod CIDR from environment variable"`
- `"Found Pod CIDR in PodCIDRs"`
- `"Found Pod CIDR in kubelet config"`

### 2. 验证 RBAC 权限

```bash
kubectl auth can-i get nodes --as=system:serviceaccount:kube-system:headcni-headcni
kubectl auth can-i list pods --as=system:serviceaccount:kube-system:headcni-headcni
```

### 3. 检查网络连通性

```bash
# 在 daemon pod 中测试 API 访问
kubectl exec -n kube-system -c headcni-daemon <pod-name> -- curl -k https://kubernetes.default.svc/api/v1/nodes
```

### 4. 检查本地配置文件

```bash
# 检查 k3s 配置
kubectl exec -n kube-system -c headcni-daemon <pod-name> -- cat /etc/rancher/k3s/k3s.yaml

# 检查标准 kubelet 配置
kubectl exec -n kube-system -c headcni-daemon <pod-name> -- cat /var/lib/kubelet/config.yaml

# 检查 kubeadm 配置
kubectl exec -n kube-system -c headcni-daemon <pod-name> -- cat /etc/kubernetes/kubeadm-config.yaml
```

### 5. 验证 CNI 配置

```bash
# 检查生成的 CNI 配置
kubectl exec -n kube-system -c headcni-daemon <pod-name> -- cat /etc/cni/net.d/10-headcni.conflist
```

## 最佳实践

1. **生产环境**: 使用环境变量方式，确保配置的确定性
2. **开发环境**: 启用 RBAC，便于调试和动态配置
3. **混合环境**: 配置环境变量作为主要方式，启用 RBAC 作为备用
4. **监控**: 监控 daemon 日志，确保 Pod CIDR 正确获取
5. **备份**: 定期备份 CNI 配置文件

## 相关文件

- `headcni/chart/templates/daemonset.yaml` - DaemonSet 配置
- `headcni/chart/templates/rbac.yaml` - RBAC 配置
- `headcni/pkg/daemon/daemon.go` - Pod CIDR 获取逻辑
- `headcni/cmd/headcni-daemon/main.go` - Kubernetes 客户端创建 