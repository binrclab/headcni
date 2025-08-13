# HeadCNI

[中文](README_CN.md)

<p align="left"><img src="./logo.png" width="400" /></p>

---

## 🇺🇸 English

HeadCNI is a Kubernetes CNI plugin that integrates Headscale and Tailscale functionality, providing a modular and extensible networking solution for Kubernetes clusters.

### 🚀 Features

- **Zero-Configuration Networking**: Automatic discovery and configuration of Tailscale networks
- **High Performance**: Efficient network forwarding based on veth pairs
- **Security**: Leverages Tailscale's WireGuard encryption
- **Simple Deployment**: No additional etcd cluster required
- **Monitoring Friendly**: Built-in Prometheus metrics
- **Multi-Strategy IPAM**: Supports sequential, random, and dense-pack allocation strategies
- **Daemon + Plugin Architecture**: Continuous daemon for dynamic network management
- **MagicDNS Support**: Native Tailscale DNS integration

### 📋 System Requirements

- Kubernetes 1.20+
- Tailscale client
- Headscale server
- Linux kernel 4.19+

### 🛠️ Quick Start

#### Method 1: Helm Deployment (Recommended)

```bash
# Clone the project
git clone <repository-url>
cd headcni

# Use deployment script
./deploy-with-helm.sh -u https://headscale.company.com -k YOUR_AUTH_KEY

# Or manually use Helm
helm upgrade --install headcni ./chart \
  --namespace kube-system \
  --set config.headscale.url=https://headscale.company.com \
  --set config.headscale.authKey=YOUR_AUTH_KEY \
  --set config.ipam.type=headcni-ipam
```

#### Method 2: Manual Deployment

##### 1. Build the Project

```bash
# Clone the project
git clone <repository-url>
cd headcni

# Build
make build
```

##### 2. Install CNI Plugin

```bash
# Install to system
make install

# Or install manually
sudo cp bin/headcni /opt/cni/bin/
sudo cp bin/headcni-ipam /opt/cni/bin/
sudo cp bin/headcni-daemon /opt/cni/bin/
sudo cp 10-headcni.conflist /etc/cni/net.d/
```

##### 3. Configure Tailscale

```bash
# Join Tailscale network
tailscale up --authkey=YOUR_AUTH_KEY
```

##### 4. Verify Installation

```bash
# Check CNI plugin
ls -la /opt/cni/bin/headcni

# Check configuration
cat /etc/cni/net.d/10-headcni.conflist
```

### ⚙️ Configuration

#### CNI Configuration File

```json
{
  "cniVersion": "1.0.0",
  "name": "tailscale-cni",
  "type": "headcni",
  
  "headscale_url": "https://hs.binrc.com",
  "tailscale_socket": "/var/run/tailscale/tailscaled.sock",
  
  "pod_cidr": "10.244.0.0/24",
  "service_cidr": "10.96.0.0/16",
  
  "ipam": {
    "type": "host-local",
    "subnet": "10.244.0.0/24",
    "rangeStart": "10.244.0.10",
    "rangeEnd": "10.244.0.254",
    "gateway": "10.244.0.1"
  },
  
  "magic_dns": {
    "enable": true,
    "base_domain": "cluster.local",
    "nameservers": ["10.2.0.1"],
    "search_domains": ["c.binrc.com"]
  },
  
  "mtu": 1280,
  "enable_ipv6": false,
  "enable_network_policy": true
}
```

#### MagicDNS Configuration

HeadCNI supports MagicDNS configuration for simplified DNS management:

```json
"magic_dns": {
  "enable": true,
  "base_domain": "cluster.local",
  "nameservers": ["10.2.0.1"],
  "search_domains": ["c.binrc.com"]
}
```

**MagicDNS Parameters:**
- **enable**: Enable MagicDNS functionality
- **base_domain**: Base domain for MagicDNS resolution
- **nameservers**: DNS server list
- **search_domains**: DNS search domain list

#### IPAM Types

HeadCNI supports two IPAM types:

1. **host-local**: Standard CNI IPAM plugin, simple and efficient
2. **headcni-ipam**: Custom IPAM plugin with advanced features

### 🔐 API Key Security

HeadCNI requires Headscale API Key for authentication. For security, **strongly avoid** storing API keys in plain text in configuration files.

#### Recommended: Environment Variables

```bash
# Direct environment variable
export HEADSCALE_API_KEY="your-api-key-here"
# Or
export HEADCNI_AUTH_KEY="your-api-key-here"

# From file
export HEADSCALE_API_KEY_FILE="/path/to/api-key-file"
# Or
export HEADCNI_AUTH_KEY_FILE="/path/to/api-key-file"
```

#### Kubernetes Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: headcni-auth
  namespace: kube-system
type: Opaque
data:
  api-key: <base64-encoded-api-key>
```

#### Environment Variable Priority

1. `HEADSCALE_API_KEY` environment variable
2. `HEADCNI_AUTH_KEY` environment variable
3. `HEADSCALE_API_KEY_FILE` file path
4. `HEADCNI_AUTH_KEY_FILE` file path
5. `auth_key` field in config file (not recommended)

### 🔧 Architecture

HeadCNI uses a **Daemon + Plugin** architecture:

- **CNI Plugin** (`headcni`): One-time execution for Pod network setup
- **Daemon** (`headcni-daemon`): Continuous running component for dynamic network management

#### Modes

1. **Host Mode**: Daemon uses existing host Tailscale interface
2. **Daemon Mode**: Daemon manages dedicated Tailscale interface (e.g., `headcni01`)

### 📊 Monitoring

HeadCNI provides Prometheus metrics:

- `headcni_ip_allocations_total`: Total IP allocations
- `headcni_ip_releases_total`: Total IP releases
- `headcni_network_errors_total`: Total network errors
- `headcni_pod_network_setup_duration_seconds`: Pod network setup duration

### 🐛 Troubleshooting

#### Common Issues

1. **CNI Plugin Cannot Load**
   ```bash
   # Check plugin permissions
   ls -la /opt/cni/bin/headcni
   
   # Check configuration
   cat /etc/cni/net.d/10-headcni.conflist
   ```

2. **IP Allocation Failure**
   ```bash
   # Check IPAM status
   journalctl -u kubelet | grep headcni
   
   # Check local storage
   ls -la /var/lib/headcni/
   ```

3. **Network Connectivity Issues**
   ```bash
   # Check Tailscale status
   tailscale status
   
   # Check network interfaces
   ip link show
   ```

### 🔧 Development

#### Project Structure

```
headcni/
├── cmd/                    # Command line tools
│   ├── headcni/           # Main CNI plugin
│   ├── headcni-daemon/    # Daemon component
│   ├── headcni-ipam/      # IPAM plugin
│   └── cli/               # CLI tool
├── pkg/                   # Core packages
│   ├── daemon/           # Daemon logic
│   ├── headscale/        # Headscale client
│   ├── ipam/             # IP address management
│   ├── logging/          # Logging utilities
│   ├── monitoring/       # Monitoring server
│   └── networking/       # Network management
├── chart/                # Helm Chart
├── Dockerfile           # Container build
├── Makefile             # Build scripts
└── README.md            # Project documentation
```

#### Running Tests

```bash
# Run all tests
go test ./...

# Run specific package tests
go test ./pkg/ipam/...

# Run benchmark tests
go test -bench=. ./pkg/ipam/
```

#### Code Quality

```bash
# Format code
make fmt

# Static analysis
make vet

# Code linting
make lint
```

### 🎯 Use Cases

- **Hybrid Cloud**: Connect Kubernetes clusters across different cloud providers
- **Edge Computing**: Connect edge nodes with central clusters
- **Development Environment**: Quick setup of multi-cluster development environments
- **Disaster Recovery**: Cross-region cluster backup and recovery

### 🤝 Contributing

Issues and Pull Requests are welcome! Please follow these steps:

1. Fork the project
2. Create a feature branch
3. Commit your changes
4. Push to the branch
5. Create a Pull Request

### 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

### 🔗 Related Links

- [CNI Specification](https://github.com/containernetworking/cni)
- [Tailscale Documentation](https://tailscale.com/kb/)
- [Headscale Documentation](https://github.com/juanfont/headscale)
- [Kubernetes Networking](https://kubernetes.io/docs/concepts/cluster-administration/networking/)

---

## 🇨🇳 中文

HeadCNI 是一个结合了 Headscale 和 Tailscale 功能的 Kubernetes CNI 插件，提供模块化、可扩展的网络解决方案。

### 🚀 特性

- **零配置网络**：自动发现和配置 Tailscale 网络
- **高性能**：基于 veth 对的高效网络转发
- **安全**：利用 Tailscale 的 WireGuard 加密
- **简单部署**：无需额外的 etcd 集群
- **监控友好**：内置 Prometheus 指标
- **多策略IPAM**：支持顺序、随机、密集打包分配策略
- **守护进程+插件架构**：持续运行的守护进程用于动态网络管理
- **MagicDNS支持**：原生 Tailscale DNS 集成

### 📋 系统要求

- Kubernetes 1.20+
- Tailscale 客户端
- Headscale 服务器
- Linux 内核 4.19+

### 🛠️ 快速开始

#### 方式一：Helm 部署（推荐）

```bash
# 克隆项目
git clone <repository-url>
cd headcni

# 使用部署脚本
./deploy-with-helm.sh -u https://headscale.company.com -k YOUR_AUTH_KEY

# 或者手动使用 Helm
helm upgrade --install headcni ./chart \
  --namespace kube-system \
  --set config.headscale.url=https://headscale.company.com \
  --set config.headscale.authKey=YOUR_AUTH_KEY \
  --set config.ipam.type=headcni-ipam
```

#### 方式二：手动部署

##### 1. 构建项目

```bash
# 克隆项目
git clone <repository-url>
cd headcni

# 构建
make build
```

##### 2. 安装 CNI 插件

```bash
# 安装到系统
make install

# 或者手动安装
sudo cp bin/headcni /opt/cni/bin/
sudo cp bin/headcni-ipam /opt/cni/bin/
sudo cp bin/headcni-daemon /opt/cni/bin/
sudo cp 10-headcni.conflist /etc/cni/net.d/
```

##### 3. 配置 Tailscale

```bash
# 加入 Tailscale 网络
tailscale up --authkey=YOUR_AUTH_KEY
```

##### 4. 验证安装

```bash
# 检查 CNI 插件
ls -la /opt/cni/bin/headcni

# 检查配置
cat /etc/cni/net.d/10-headcni.conflist
```

### ⚙️ 配置说明

#### CNI 配置文件

```json
{
  "cniVersion": "1.0.0",
  "name": "tailscale-cni",
  "type": "headcni",
  
  "headscale_url": "https://hs.binrc.com",
  "tailscale_socket": "/var/run/tailscale/tailscaled.sock",
  
  "pod_cidr": "10.244.0.0/24",
  "service_cidr": "10.96.0.0/16",
  
  "ipam": {
    "type": "host-local",
    "subnet": "10.244.0.0/24",
    "rangeStart": "10.244.0.10",
    "rangeEnd": "10.244.0.254",
    "gateway": "10.244.0.1"
  },
  
  "magic_dns": {
    "enable": true,
    "base_domain": "cluster.local",
    "nameservers": ["10.2.0.1"],
    "search_domains": ["c.binrc.com"]
  },
  
  "mtu": 1280,
  "enable_ipv6": false,
  "enable_network_policy": true
}
```

#### MagicDNS 配置

HeadCNI 支持 MagicDNS 配置，提供更简洁的 DNS 管理：

```json
"magic_dns": {
  "enable": true,
  "base_domain": "cluster.local",
  "nameservers": ["10.2.0.1"],
  "search_domains": ["c.binrc.com"]
}
```

**MagicDNS 参数：**
- **enable**: 启用 MagicDNS 功能
- **base_domain**: MagicDNS 解析的基础域名
- **nameservers**: DNS 服务器列表
- **search_domains**: DNS 搜索域列表

#### IPAM 类型

HeadCNI 支持两种 IPAM 类型：

1. **host-local**：标准 CNI IPAM 插件，简单高效
2. **headcni-ipam**：自定义 IPAM 插件，支持高级功能

### 🔐 API Key 安全配置

HeadCNI 需要 Headscale API Key 进行身份验证。为了安全起见，**强烈建议不要**在配置文件中明文存储 API Key。

#### 推荐方式：环境变量

```bash
# 直接设置环境变量
export HEADSCALE_API_KEY="your-api-key-here"
# 或者
export HEADCNI_AUTH_KEY="your-api-key-here"

# 从文件读取
export HEADSCALE_API_KEY_FILE="/path/to/api-key-file"
# 或者
export HEADCNI_AUTH_KEY_FILE="/path/to/api-key-file"
```

#### Kubernetes Secret

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: headcni-auth
  namespace: kube-system
type: Opaque
data:
  api-key: <base64-encoded-api-key>
```

#### 环境变量优先级

1. `HEADSCALE_API_KEY` 环境变量
2. `HEADCNI_AUTH_KEY` 环境变量
3. `HEADSCALE_API_KEY_FILE` 文件路径
4. `HEADCNI_AUTH_KEY_FILE` 文件路径
5. 配置文件中的 `auth_key` 字段（不推荐）

### 🔧 架构

HeadCNI 使用**守护进程+插件**架构：

- **CNI 插件** (`headcni`)：一次性执行，用于 Pod 网络设置
- **守护进程** (`headcni-daemon`)：持续运行组件，用于动态网络管理

#### 模式

1. **Host 模式**：守护进程使用现有的主机 Tailscale 接口
2. **Daemon 模式**：守护进程管理专用的 Tailscale 接口（如 `headcni01`）

### 📊 监控

HeadCNI 提供以下 Prometheus 指标：

- `headcni_ip_allocations_total`：IP 分配总数
- `headcni_ip_releases_total`：IP 释放总数
- `headcni_network_errors_total`：网络错误总数
- `headcni_pod_network_setup_duration_seconds`：Pod 网络设置耗时

### 🐛 故障排除

#### 常见问题

1. **CNI 插件无法加载**
   ```bash
   # 检查插件权限
   ls -la /opt/cni/bin/headcni
   
   # 检查配置文件
   cat /etc/cni/net.d/10-headcni.conflist
   ```

2. **IP 分配失败**
   ```bash
   # 检查 IPAM 状态
   journalctl -u kubelet | grep headcni
   
   # 检查本地存储
   ls -la /var/lib/headcni/
   ```

3. **网络连接问题**
   ```bash
   # 检查 Tailscale 状态
   tailscale status
   
   # 检查网络接口
   ip link show
   ```

### 🔧 开发

#### 项目结构

```
headcni/
├── cmd/                    # 命令行工具
│   ├── headcni/           # 主 CNI 插件
│   ├── headcni-daemon/    # 守护进程组件
│   ├── headcni-ipam/      # IPAM 插件
│   └── cli/               # CLI 工具
├── pkg/                   # 核心包
│   ├── daemon/           # 守护进程逻辑
│   ├── headscale/        # Headscale 客户端
│   ├── ipam/             # IP 地址管理
│   ├── logging/          # 日志工具
│   ├── monitoring/       # 监控服务器
│   └── networking/       # 网络管理
├── chart/                # Helm Chart
├── Dockerfile           # 容器构建
├── Makefile             # 构建脚本
└── README.md            # 项目文档
```

#### 运行测试

```bash
# 运行所有测试
go test ./...

# 运行特定包的测试
go test ./pkg/ipam/...

# 运行基准测试
go test -bench=. ./pkg/ipam/
```

#### 代码质量

```bash
# 格式化代码
make fmt

# 静态分析
make vet

# 代码检查
make lint
```

### 🎯 使用场景

- **混合云**：连接不同云提供商的 Kubernetes 集群
- **边缘计算**：连接边缘节点与中央集群
- **开发环境**：快速搭建多集群开发环境
- **灾难恢复**：跨区域集群备份和恢复

### 🤝 贡献

欢迎贡献代码！请遵循以下步骤：

1. Fork 项目
2. 创建功能分支
3. 提交更改
4. 推送到分支
5. 创建 Pull Request

### 📄 许可证

本项目采用 MIT 许可证 - 查看 [LICENSE](LICENSE) 文件了解详情。

### 🔗 相关链接

- [CNI 规范](https://github.com/containernetworking/cni)
- [Tailscale 文档](https://tailscale.com/kb/)
- [Headscale 文档](https://github.com/juanfont/headscale)
- [Kubernetes 网络](https://kubernetes.io/docs/concepts/cluster-administration/networking/)
