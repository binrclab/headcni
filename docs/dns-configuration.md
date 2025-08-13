# DNS 配置说明 / DNS Configuration Guide

[English](#english) | [中文](#chinese)

---

## 🇨🇳 中文

### DNS 配置概述

在 Kubernetes CNI 插件中，DNS 配置是 Pod 网络功能的核心组件。HeadCNI 通过 DNS 配置实现服务发现、集群内通信和外部网络访问。

### DNS 配置结构

```json
"dns": {
  "nameservers": ["10.96.0.10"],
  "search": ["default.svc.cluster.local", "svc.cluster.local", "cluster.local"],
  "options": ["ndots:5"]
}
```

### 参数详细说明

#### 1. nameservers（DNS 服务器）

**作用**: 指定 Pod 使用的 DNS 服务器地址

**配置值**: `["10.96.0.10"]`

**说明**:
- `10.96.0.10` 是 Kubernetes 集群中 CoreDNS 服务的默认 IP 地址
- 这个 IP 来自 `service_cidr` 配置（通常是 `10.96.0.0/16`）
- Pod 通过这个 DNS 服务器解析所有域名

**示例**:
```bash
# 在 Pod 中测试 DNS 解析
nslookup kubernetes.default
nslookup google.com
```

#### 2. search（搜索域）

**作用**: 指定 DNS 搜索域，用于简化服务访问

**配置值**: 
```json
[
  "default.svc.cluster.local",
  "svc.cluster.local", 
  "cluster.local"
]
```

**说明**:
- `default.svc.cluster.local` - 默认命名空间的服务
- `svc.cluster.local` - 所有命名空间的服务
- `cluster.local` - 集群域名

**使用示例**:
```bash
# 在 Pod 中访问服务
curl http://nginx                    # 自动解析为 nginx.default.svc.cluster.local
curl http://nginx.default            # 自动解析为 nginx.default.svc.cluster.local
curl http://nginx.default.svc        # 自动解析为 nginx.default.svc.cluster.local
```

#### 3. options（DNS 选项）

**作用**: 配置 DNS 解析行为

**配置值**: `["ndots:5"]`

**说明**:
- `ndots:5` - 当域名中的点少于 5 个时，会先尝试搜索域
- 这是 Kubernetes 的标准配置

**解析逻辑**:
```
域名: nginx
点数: 0 < 5
解析顺序:
1. nginx.default.svc.cluster.local
2. nginx.svc.cluster.local  
3. nginx.cluster.local
4. nginx (直接解析)
```

### 为什么需要 DNS 配置？

#### 1. 服务发现 (Service Discovery)
```bash
# Pod 可以通过服务名直接访问
curl http://my-service
curl http://my-service.my-namespace
```

#### 2. 集群内通信 (Intra-Cluster Communication)
```bash
# 支持 Kubernetes 服务发现机制
kubectl get svc
# 服务可以通过 DNS 名访问
```

#### 3. 外部访问 (External Access)
```bash
# Pod 可以解析外部域名
curl https://www.google.com
wget https://api.github.com
```

#### 4. 网络策略支持 (Network Policy Support)
```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-dns
spec:
  egress:
  - to: []
    ports:
    - protocol: UDP
      port: 53
```

### 常见问题

#### Q1: DNS 解析失败怎么办？
```bash
# 检查 CoreDNS 服务状态
kubectl get pods -n kube-system -l k8s-app=kube-dns

# 检查 DNS 服务
kubectl get svc -n kube-system kube-dns

# 测试 DNS 解析
kubectl run test-dns --image=busybox --rm -it -- nslookup kubernetes.default
```

#### Q2: 如何自定义 DNS 服务器？
```json
"dns": {
  "nameservers": ["8.8.8.8", "8.8.4.4"],
  "search": ["default.svc.cluster.local", "svc.cluster.local", "cluster.local"],
  "options": ["ndots:5"]
}
```

#### Q3: 如何添加自定义搜索域？
```json
"dns": {
  "nameservers": ["10.96.0.10"],
  "search": ["default.svc.cluster.local", "svc.cluster.local", "cluster.local", "mycompany.com"],
  "options": ["ndots:5"]
}
```

### 最佳实践

1. **使用集群内 DNS**: 优先使用 `10.96.0.10`
2. **保持搜索域顺序**: 按照 Kubernetes 标准配置
3. **测试 DNS 功能**: 部署后验证 DNS 解析
4. **监控 DNS 性能**: 关注 DNS 查询延迟

---

## 🇺🇸 English

### DNS Configuration Overview

In Kubernetes CNI plugins, DNS configuration is a core component of Pod networking functionality. HeadCNI implements service discovery, intra-cluster communication, and external network access through DNS configuration.

### DNS Configuration Structure

```json
"dns": {
  "nameservers": ["10.96.0.10"],
  "search": ["default.svc.cluster.local", "svc.cluster.local", "cluster.local"],
  "options": ["ndots:5"]
}
```

### Parameter Details

#### 1. nameservers (DNS Servers)

**Purpose**: Specifies the DNS server addresses used by Pods

**Configuration**: `["10.96.0.10"]`

**Description**:
- `10.96.0.10` is the default IP address of the CoreDNS service in Kubernetes cluster
- This IP comes from the `service_cidr` configuration (usually `10.96.0.0/16`)
- Pods resolve all domain names through this DNS server

**Example**:
```bash
# Test DNS resolution in Pod
nslookup kubernetes.default
nslookup google.com
```

#### 2. search (Search Domains)

**Purpose**: Specifies DNS search domains for simplified service access

**Configuration**:
```json
[
  "default.svc.cluster.local",
  "svc.cluster.local", 
  "cluster.local"
]
```

**Description**:
- `default.svc.cluster.local` - Services in default namespace
- `svc.cluster.local` - Services in all namespaces
- `cluster.local` - Cluster domain name

**Usage Example**:
```bash
# Access services in Pod
curl http://nginx                    # Auto-resolves to nginx.default.svc.cluster.local
curl http://nginx.default            # Auto-resolves to nginx.default.svc.cluster.local
curl http://nginx.default.svc        # Auto-resolves to nginx.default.svc.cluster.local
```

#### 3. options (DNS Options)

**Purpose**: Configure DNS resolution behavior

**Configuration**: `["ndots:5"]`

**Description**:
- `ndots:5` - When a domain has fewer than 5 dots, it will try search domains first
- This is the standard Kubernetes configuration

**Resolution Logic**:
```
Domain: nginx
Dots: 0 < 5
Resolution order:
1. nginx.default.svc.cluster.local
2. nginx.svc.cluster.local  
3. nginx.cluster.local
4. nginx (direct resolution)
```

### Why DNS Configuration is Needed?

#### 1. Service Discovery
```bash
# Pods can access services directly by name
curl http://my-service
curl http://my-service.my-namespace
```

#### 2. Intra-Cluster Communication
```bash
# Supports Kubernetes service discovery mechanism
kubectl get svc
# Services can be accessed via DNS names
```

#### 3. External Access
```bash
# Pods can resolve external domains
curl https://www.google.com
wget https://api.github.com
```

#### 4. Network Policy Support
```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-dns
spec:
  egress:
  - to: []
    ports:
    - protocol: UDP
      port: 53
```

### Common Issues

#### Q1: What if DNS resolution fails?
```bash
# Check CoreDNS service status
kubectl get pods -n kube-system -l k8s-app=kube-dns

# Check DNS service
kubectl get svc -n kube-system kube-dns

# Test DNS resolution
kubectl run test-dns --image=busybox --rm -it -- nslookup kubernetes.default
```

#### Q2: How to customize DNS servers?
```json
"dns": {
  "nameservers": ["8.8.8.8", "8.8.4.4"],
  "search": ["default.svc.cluster.local", "svc.cluster.local", "cluster.local"],
  "options": ["ndots:5"]
}
```

#### Q3: How to add custom search domains?
```json
"dns": {
  "nameservers": ["10.96.0.10"],
  "search": ["default.svc.cluster.local", "svc.cluster.local", "cluster.local", "mycompany.com"],
  "options": ["ndots:5"]
}
```

### Best Practices

1. **Use In-Cluster DNS**: Prefer `10.96.0.10`
2. **Maintain Search Domain Order**: Follow Kubernetes standard configuration
3. **Test DNS Functionality**: Verify DNS resolution after deployment
4. **Monitor DNS Performance**: Pay attention to DNS query latency 