# HeadCNI Daemon 配置说明

## 概述

HeadCNI Daemon 支持多种方式配置网络参数，包括命令行参数、环境变量和配置文件。`podCIDR` 和 `serviceCIDR` 已经作为入参提供，无需复杂的动态获取逻辑。

## 配置方式

### 1. 命令行参数
```bash
./headcni-daemon \
  --pod-cidr=10.244.0.0/16 \
  --service-cidr=10.96.0.0/16 \
  --headscale-url=https://hs.binrc.com \
  --headscale-auth-key=your-auth-key
```

### 2. 环境变量
```bash
export POD_CIDR="10.244.0.0/16"
export SERVICE_CIDR="10.96.0.0/16"
export HEADSCALE_URL="https://hs.binrc.com"
export HEADSCALE_AUTH_KEY="your-auth-key"
./headcni-daemon
```

### 3. 配置文件
```yaml
# config.yaml
network:
  podCIDR:
    base: "10.244.0.0/16"
    perNode: "/24"
  serviceCIDR: "10.96.0.0/16"
```

## 配置优先级

配置的优先级顺序为：
1. **命令行参数** (最高优先级)
2. **环境变量**
3. **配置文件**
4. **默认值** (最低优先级)

## 主要配置参数

### 网络配置
- `--pod-cidr` / `POD_CIDR`: Pod 网络 CIDR
- `--service-cidr` / `SERVICE_CIDR`: Service 网络 CIDR
- `--network-mtu` / `NETWORK_MTU`: 网络 MTU
- `--enable-ipv6` / `ENABLE_IPV6`: 启用 IPv6
- `--enable-network-policy` / `ENABLE_NETWORK_POLICY`: 启用网络策略

### HeadScale 配置
- `--headscale-url` / `HEADSCALE_URL`: HeadScale 服务器 URL
- `--headscale-auth-key` / `HEADSCALE_AUTH_KEY`: HeadScale API 密钥
- `--headscale-timeout` / `HEADSCALE_TIMEOUT`: 请求超时时间
- `--headscale-retries` / `HEADSCALE_RETRIES`: 重试次数

### Tailscale 配置
- `--tailscale-mode` / `TAILSCALE_MODE`: 运行模式 (host/daemon)
- `--tailscale-url` / `TAILSCALE_URL`: Tailscale 服务器 URL
- `--tailscale-socket` / `TAILSCALE_SOCKET`: Tailscale socket 路径
- `--tailscale-mtu` / `TAILSCALE_MTU`: Tailscale MTU
- `--tailscale-accept-dns` / `TAILSCALE_ACCEPT_DNS`: 接受 DNS 设置

### 监控配置
- `--monitoring-enabled` / `MONITORING_ENABLED`: 启用监控
- `--metrics-port` / `METRICS_PORT`: 指标端口
- `--metrics-path` / `METRICS_PATH`: 指标路径

### 日志配置
- `--log-level` / `LOG_LEVEL`: 日志级别
- `--log-format` / `LOG_FORMAT`: 日志格式
- `--log-output` / `LOG_OUTPUT`: 日志输出

## 使用示例

### 基本使用
```bash
# 使用默认配置
./headcni-daemon

# 指定配置文件
./headcni-daemon --config=/path/to/config.yaml

# 使用命令行参数
./headcni-daemon --pod-cidr=10.244.0.0/16 --service-cidr=10.96.0.0/16
```

### Kubernetes 部署
```yaml
# daemonset.yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: headcni-daemon
spec:
  template:
    spec:
      containers:
      - name: headcni-daemon
        image: headcni/headcni-daemon:latest
        command:
        - /opt/cni/bin/headcni-daemon
        - --config=/tmp/headcni/config/daemon.yaml
        - --pod-cidr=10.244.0.0/16
        - --service-cidr=10.96.0.0/16
        env:
        - name: HEADSCALE_AUTH_KEY
          valueFrom:
            secretKeyRef:
              name: headscale-secret
              key: auth-key
```

### 环境变量配置
```bash
#!/bin/bash
# deploy.sh

export POD_CIDR="10.244.0.0/16"
export SERVICE_CIDR="10.96.0.0/16"
export HEADSCALE_URL="https://hs.binrc.com"
export HEADSCALE_AUTH_KEY="your-auth-key"
export TAILSCALE_MODE="daemon"
export LOG_LEVEL="info"

./headcni-daemon
```

## 配置文件结构

### 默认配置文件 (default.yaml)
```yaml
# 网络配置 (可通过命令行参数或环境变量覆盖)
network:
  podCIDR:
    base: ""  # 通过 --pod-cidr 或 POD_CIDR 设置
    perNode: "/24"
  serviceCIDR: ""  # 通过 --service-cidr 或 SERVICE_CIDR 设置
  mtu: 1280
  enableIPv6: false
  enableNetworkPolicy: true

# HeadScale 配置
headscale:
  url: "https://hs.binrc.com"
  authKey: ""  # 通过 --headscale-auth-key 或 HEADSCALE_AUTH_KEY 设置
  timeout: "30s"
  retries: 3

# Tailscale 配置
tailscale:
  mode: "daemon"
  url: "https://hs.binrc.com"
  socket:
    path: "/var/run/headcni/headcni_tailscale.sock"
    name: "headcni_tailscale.sock"
  mtu: 1280
  acceptDNS: true
  hostname:
    prefix: "headcni-pod"
    type: "hostname"
  user: "server"
  tags:
    - "tag:control-server"
    - "tag:headcni"
  interfaceName: "headcni01"
```

## 配置验证

### 检查配置是否正确加载
```bash
# 查看日志中的配置信息
kubectl logs -n kube-system headcni-daemon-xxx

# 检查环境变量
env | grep -E "(POD_CIDR|SERVICE_CIDR|HEADSCALE)"

# 验证网络配置
kubectl get nodes -o jsonpath='{.items[*].spec.podCIDR}'
```

## 故障排除

### 1. 配置参数未生效
- 检查命令行参数拼写是否正确
- 确认环境变量名称是否正确
- 验证配置文件格式是否正确

### 2. 网络配置冲突
- 确保 `podCIDR` 与集群配置一致
- 检查 `serviceCIDR` 是否与现有服务冲突
- 验证 MTU 设置是否合理

### 3. 连接问题
- 检查 HeadScale URL 是否可访问
- 验证 API 密钥是否有效
- 确认网络连接是否正常

## 最佳实践

1. **使用配置文件**: 对于复杂配置，建议使用 YAML 配置文件
2. **环境变量**: 对于敏感信息（如 API 密钥），使用环境变量
3. **命令行参数**: 对于临时测试或简单配置，使用命令行参数
4. **配置验证**: 部署前验证所有配置参数
5. **日志监控**: 启用适当的日志级别进行问题排查

## 注意事项

1. **配置优先级**: 命令行参数会覆盖环境变量和配置文件
2. **敏感信息**: 不要在配置文件中硬编码敏感信息
3. **网络一致性**: 确保网络配置与 Kubernetes 集群一致
4. **版本兼容**: 注意配置参数在不同版本间的兼容性 