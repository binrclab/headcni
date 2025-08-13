# HeadCNI Daemon 使用指南

## 🚀 **快速开始**

### **命令行运行**

```bash
# Host 模式运行
./headcni-daemon \
  --headscale-url="https://hs.binrc.com" \
  --headscale-auth-key="your-api-key-here" \
  --pod-cidr="10.244.0.0/16" \
  --service-cidr="10.96.0.0/16" \
  --mtu=1280 \
  --mode="host"

# Daemon 模式运行
./headcni-daemon \
  --headscale-url="https://hs.binrc.com" \
  --headscale-auth-key="your-api-key-here" \
  --pod-cidr="10.244.0.0/16" \
  --service-cidr="10.96.0.0/16" \
  --mtu=1280 \
  --mode="daemon" \
  --interface-name="headcni01"
```

### **Helm 部署**

```bash
# 创建 values.yaml
cat > values.yaml << EOF
config:
  headscale:
    url: "https://hs.binrc.com"
    authKey: "your-api-key-here"
  
  tailscale:
    mode: "daemon"  # 或 "host"
    socket_name: "headcni01"
  
  network:
    podCIDRBase: "10.244.0.0/16"
    serviceCIDR: "10.96.0.0/16"
    mtu: 1280
  
  ipam:
    type: "host-local"
    strategy: "sequential"
  
  monitoring:
    enabled: true
    port: 8080
    path: "/metrics"

image:
  repository: "your-registry/headcni"
  tag: "latest"
  pullPolicy: "IfNotPresent"

resources:
  manager:
    requests:
      memory: "256Mi"
      cpu: "200m"
    limits:
      memory: "512Mi"
      cpu: "500m"
EOF

# 部署
helm install headcni ./chart -f values.yaml
```

## 🔧 **参数说明**

### **必需参数**

| 参数 | 说明 | 示例 |
|------|------|------|
| `--headscale-url` | Headscale 服务器 URL | `https://hs.binrc.com` |
| `--headscale-auth-key` | Headscale API Key | `tskey-auth-xxx` |

### **网络参数**

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--pod-cidr` | `10.244.0.0/16` | Pod CIDR 网段 |
| `--service-cidr` | `10.96.0.0/16` | Service CIDR 网段 |
| `--mtu` | `1280` | 网络接口 MTU |

### **IPAM 参数**

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--ipam-type` | `host-local` | IPAM 类型 |
| `--allocation-strategy` | `sequential` | IP 分配策略 |

### **模式参数**

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--mode` | `host` | 运行模式：`host` 或 `daemon` |
| `--interface-name` | `headcni01` | Tailscale 接口名称（仅 daemon 模式） |

### **监控参数**

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--metrics-port` | `8080` | 监控端口 |
| `--metrics-path` | `/metrics` | 监控路径 |

## 🎯 **模式配置示例**

### **Host 模式配置**

```yaml
# values-host.yaml
config:
  headscale:
    url: "https://hs.binrc.com"
    authKey: "your-api-key-here"
  
  tailscale:
    mode: "host"  # 使用现有 Tailscale 接口
  
  network:
    podCIDRBase: "10.244.0.0/16"
    serviceCIDR: "10.96.0.0/16"
    mtu: 1280
  
  ipam:
    type: "host-local"
    strategy: "sequential"
  
  monitoring:
    enabled: true
    port: 8080
    path: "/metrics"
```

**部署命令：**
```bash
helm install headcni ./chart -f values-host.yaml
```

### **Daemon 模式配置**

```yaml
# values-daemon.yaml
config:
  headscale:
    url: "https://hs.binrc.com"
    authKey: "your-api-key-here"
  
  tailscale:
    mode: "daemon"  # 创建专用接口
    socket_name: "headcni01"
  
  network:
    podCIDRBase: "10.244.0.0/16"
    serviceCIDR: "10.96.0.0/16"
    mtu: 1280
  
  ipam:
    type: "host-local"
    strategy: "sequential"
  
  monitoring:
    enabled: true
    port: 8080
    path: "/metrics"

resources:
  manager:
    requests:
      memory: "256Mi"
      cpu: "200m"
    limits:
      memory: "512Mi"
      cpu: "500m"
```

**部署命令：**
```bash
helm install headcni ./chart -f values-daemon.yaml
```

## 🔍 **监控和调试**

### **查看日志**

```bash
# 查看 DaemonSet 日志
kubectl logs -n kube-system -l app=headcni -f

# 查看特定 Pod 日志
kubectl logs -n kube-system headcni-daemon-xxx -f
```

### **检查状态**

```bash
# 检查 DaemonSet 状态
kubectl get daemonset -n kube-system headcni

# 检查 Pod 状态
kubectl get pods -n kube-system -l app=headcni

# 检查网络接口（Daemon 模式）
kubectl exec -n kube-system headcni-daemon-xxx -- ip link show headcni01
```

### **访问监控指标**

```bash
# 端口转发
kubectl port-forward -n kube-system headcni-daemon-xxx 8080:8080

# 访问指标
curl http://localhost:8080/metrics
```

## 🔧 **故障排除**

### **常见问题**

#### **1. API Key 错误**
```bash
# 错误日志
ERROR: API key is invalid or expired

# 解决方案
# 检查 API Key 是否正确
curl -H "Authorization: Bearer YOUR_API_KEY" https://hs.binrc.com/api/v1/apikey
```

#### **2. 网络接口创建失败**
```bash
# 错误日志
ERROR: Failed to create headcni01 interface

# 解决方案
# 检查是否有足够的权限
kubectl exec -n kube-system headcni-daemon-xxx -- ls -la /var/run/tailscale/
```

#### **3. Tailscale 服务启动失败**
```bash
# 错误日志
ERROR: Failed to start tailscaled

# 解决方案
# 检查系统是否有 tailscaled 二进制文件
kubectl exec -n kube-system headcni-daemon-xxx -- which tailscaled
```

### **调试命令**

```bash
# 进入 Pod 调试
kubectl exec -it -n kube-system headcni-daemon-xxx -- /bin/sh

# 检查网络接口
ip link show

# 检查路由表
ip route show

# 检查 Tailscale 状态
tailscale status

# 检查 HeadCNI 接口（Daemon 模式）
tailscale status --socket /var/run/tailscale/headcni01.sock
```

## 📊 **性能调优**

### **资源配置**

```yaml
resources:
  manager:
    requests:
      memory: "256Mi"    # 根据 Pod 数量调整
      cpu: "200m"        # 根据负载调整
    limits:
      memory: "512Mi"    # 建议不超过 1Gi
      cpu: "500m"        # 建议不超过 1 core
```

### **监控配置**

```yaml
config:
  monitoring:
    enabled: true
    port: 8080
    path: "/metrics"
  
  # 添加自定义标签
  labels:
    app: headcni
    version: v1.0.0
```

## 🚀 **生产环境部署**

### **高可用配置**

```yaml
# values-production.yaml
replicaCount: 3

podAntiAffinity:
  enabled: true

config:
  headscale:
    url: "https://hs.binrc.com"
    authKey: "your-api-key-here"
  
  tailscale:
    mode: "daemon"
    socket_name: "headcni01"
  
  network:
    podCIDRBase: "10.244.0.0/16"
    serviceCIDR: "10.96.0.0/16"
    mtu: 1280
  
  monitoring:
    enabled: true
    port: 8080
    path: "/metrics"

resources:
  manager:
    requests:
      memory: "512Mi"
      cpu: "300m"
    limits:
      memory: "1Gi"
      cpu: "800m"

nodeSelector:
  node-role.kubernetes.io/worker: "true"

tolerations:
- key: "node-role.kubernetes.io/master"
  operator: "Exists"
  effect: "NoSchedule"
```

**部署命令：**
```bash
helm install headcni ./chart -f values-production.yaml
```

## 📋 **总结**

HeadCNI Daemon 提供了灵活的配置选项，支持 Host 和 Daemon 两种模式：

- **Host 模式**：适合开发和测试环境，资源消耗少
- **Daemon 模式**：适合生产环境，提供完全的网络隔离

通过合理的配置和监控，可以确保 HeadCNI 在 Kubernetes 集群中稳定运行！ 