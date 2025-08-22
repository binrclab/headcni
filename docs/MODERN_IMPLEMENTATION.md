# HeadCNI 现代实现方式

本文档说明 HeadCNI 的现代实现方式，参考 Flannel 的最佳实践。

## 架构概述

### 现代 Kubernetes 网络插件架构

```
┌─────────────────────────────────────┐
│           Master Node               │
│                                     │
│ kube-controller-manager             │
│ ├── --cluster-cidr=10.244.0.0/16    │
│ ├── --node-cidr-mask-size=24        │
│ └── 自动分配PodCIDR给每个Node         │
│                                     │
│ etcd (存储Node信息，包含PodCIDR)      │
└─────────────────────────────────────┘
           │ 通过kube-apiserver
           ▼
┌─────────────────────────────────────┐
│           Worker Node               │
│                                     │
│ headcni-daemon                      │
│ ├── 通过本地文件获取PodCIDR          │
│ ├── 生成CNI配置文件                  │
│ ├── 写入/etc/cni/net.d/             │
│ └── 不直接连接etcd ❌                │
│                                     │
│ headcni CNI插件                      │
│ ├── 读取CNI配置文件                  │
│ └── 委托给bridge+host-local IPAM     │
└─────────────────────────────────────┘
```

## PodCIDR 获取策略

### 优先级顺序

1. **配置文件**: `config.network.podCIDR.nodeLocal`
2. **环境变量**: `NODE_POD_CIDR`
3. **本地文件**: kubelet/k3s 配置文件
4. **哈希计算**: 使用主机名哈希作为备用（可能不准确）
5. **默认值**: `10.244.0.0/24`

### 本地文件读取

HeadCNI 支持从以下本地文件读取 PodCIDR：

#### k3s 环境
- `/var/lib/rancher/k3s/agent/etc/node-{nodeName}.yaml`
- `/etc/rancher/k3s/k3s.yaml`
- `/var/lib/rancher/k3s/agent/etc/kubelet.config`

#### 标准 Kubernetes 环境
- `/var/lib/kubelet/config.yaml`
- `/etc/kubernetes/kubeadm-config.yaml`
- `/etc/kubernetes/admin.conf`

#### 其他路径
- `/etc/kubernetes/manifests/kube-controller-manager.yaml`
- `/etc/kubernetes/manifests/kube-apiserver.yaml`

### 文件解析格式

支持多种配置格式：

```yaml
# YAML 格式
podCIDR: "10.244.1.0/24"
pod-cidr: "10.244.1.0/24"
```

```bash
# 命令行参数格式
--cluster-cidr=10.244.0.0/16
--cluster-cidr 10.244.0.0/16
```

## CNI 配置生成

### 配置结构

```json
{
  "cniVersion": "0.4.0",
  "name": "headcni",
  "plugins": [
    {
      "type": "bridge",
      "bridge": "headcni0",
      "ipMasq": true,
      "isGateway": true,
      "ipam": {
        "type": "host-local",
        "subnet": "10.244.1.0/24",
        "routes": [
          {
            "dst": "0.0.0.0/0"
          }
        ]
      }
    }
  ]
}
```

### 配置管理

- **检查存在性**: 验证配置文件是否已存在
- **备份现有配置**: 备份其他 `.conflist` 文件
- **生成新配置**: 根据获取的 PodCIDR 生成配置
- **验证配置**: 确保配置格式正确
- **写入文件**: 原子写入到 `/etc/cni/net.d/10-headcni.conflist`

## 与 Flannel 的对比

### 相似之处

1. **本地文件优先**: 都优先从本地文件读取配置
2. **备用计算**: 都支持从集群 CIDR 计算节点本地 CIDR
3. **配置生成**: 都生成标准的 CNI 配置文件
4. **不依赖 etcd**: 都不直接连接 etcd

### 不同之处

| 特性 | Flannel | HeadCNI |
|------|---------|---------|
| 主要数据源 | Kubernetes API | 本地文件 |
| 网络模式 | Overlay (VXLAN/UDP) | Underlay (Tailscale) |
| 路由通告 | 通过 etcd/API | 通过 Headscale |
| 节点发现 | Kubernetes API | Tailscale 网络 |

## 部署配置

### Helm Chart 配置

```yaml
config:
  network:
    podCIDR:
      nodeLocal: "10.244.1.0/24"  # 当前节点的本地 Pod CIDR
      base: "10.244.0.0/16"       # 集群 Pod CIDR（用于计算）
      perNode: "/24"              # 每个节点的子网大小
    serviceCIDR: "10.96.0.0/16"
    mtu: 1280
```

### 环境变量方式

```bash
helm install headcni ./headcni/chart \
  --set config.network.podCIDR.nodeLocal="10.244.1.0/24"
```

### 自动检测方式

```bash
# 不指定 nodeLocal，让 HeadCNI 自动检测
helm install headcni ./headcni/chart
```

## 故障排除

### 常见问题

1. **PodCIDR 获取失败**
   - 检查本地配置文件是否存在
   - 验证文件格式是否正确
   - 确认节点名称是否正确

2. **CNI 配置生成失败**
   - 检查 `/etc/cni/net.d/` 目录权限
   - 验证 PodCIDR 格式是否正确
   - 查看 daemon 日志

3. **网络连通性问题**
   - 检查 Tailscale 连接状态
   - 验证 Headscale 配置
   - 确认路由通告是否正确

### 调试命令

```bash
# 检查 PodCIDR 获取
kubectl logs -n kube-system daemonset/headcni-daemon

# 检查 CNI 配置
cat /etc/cni/net.d/10-headcni.conflist

# 检查本地文件
cat /var/lib/kubelet/config.yaml | grep podCIDR
```

## 最佳实践

1. **明确指定节点本地 CIDR**: 在生产环境中，建议明确指定 `nodeLocal` 配置
2. **监控日志**: 定期检查 daemon 日志，确保 PodCIDR 获取正常
3. **备份配置**: 在升级前备份现有的 CNI 配置
4. **测试验证**: 在部署到生产环境前，在测试环境中验证配置

## 总结

HeadCNI 采用现代 Kubernetes 网络插件的最佳实践：

- ✅ 不直接依赖 etcd
- ✅ 优先使用本地文件
- ✅ 支持多种配置源
- ✅ 提供备用计算方案
- ✅ 生成标准 CNI 配置
- ✅ 支持原子配置更新

这种设计使得 HeadCNI 能够在各种 Kubernetes 环境中稳定运行，包括 worker 节点无法访问 API Server 的场景。 