# HeadCNI
[English](#README.md) |
<p align="left"><img src="./logo.png" width="400" /></p>

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
  --set config.headscale.authKey=YOUR_AUTH_KEY
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