# HeadCNI
<p align="left"><img src="./logo.png" width="400" /></p>

HeadCNI 是一个结合了 Headscale 和 Tailscale 功能的 Kubernetes CNI 插件，参考 Flannel 架构设计，提供模块化、可扩展的网络解决方案。

## 🚀 特性

- **零配置网络**：自动发现和配置 Tailscale 网络
- **高性能**：基于 veth 对的高效网络转发
- **安全**：利用 Tailscale 的 WireGuard 加密
- **简单部署**：无需额外的 etcd 集群
- **监控友好**：内置 Prometheus 指标
- **多策略IPAM**：支持顺序、随机、密集打包分配策略

## 📋 系统要求

- Kubernetes 1.20+
- Tailscale 客户端
- Headscale 服务器
- Linux 内核 4.19+

## 🛠️ 快速开始

### 方式一：Helm 部署（推荐）

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

### 方式二：手动部署

#### 1. 构建项目

```bash
# 克隆项目
git clone <repository-url>
cd headcni

# 构建
make build
```

#### 2. 安装 CNI 插件

```bash
# 安装到系统
make install

# 或者手动安装
sudo cp bin/headcni /opt/cni/bin/
sudo cp bin/headcni-ipam /opt/cni/bin/
sudo cp 10-headcni.conflist /etc/cni/net.d/
```

### 3. 配置 Tailscale

```bash
# 加入 Tailscale 网络
tailscale up --authkey=YOUR_AUTH_KEY
```

### 4. 验证安装

```bash
# 检查 CNI 插件
ls -la /opt/cni/bin/headcni

# 检查配置
cat /etc/cni/net.d/10-headcni.conflist
```

## ⚙️ 配置说明

### conflist 文件生成方式

HeadCNI 支持多种方式生成 CNI 配置文件：

#### 1. **Helm Chart 自动生成（推荐）**
- 通过 `values.yaml` 配置参数
- 自动生成 ConfigMap
- 通过 InitContainer 复制到节点
- 支持动态配置更新

#### 2. **手动创建**
- 直接编辑 `10-headcni.conflist` 文件
- 复制到 `/etc/cni/net.d/` 目录
- 适合简单部署场景

#### 3. **脚本生成**
- 使用 `deploy-with-helm.sh` 脚本
- 根据参数自动生成配置
- 支持命令行参数覆盖

### CNI 配置文件

#### 使用标准 host-local IPAM

```json
{
  "cniVersion": "1.0.0",
  "name": "tailscale-cni",
  "type": "headcni",
  
  "headscale_url": "https://headscale.company.com",
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
  
  "dns": {
    "nameservers": ["10.96.0.10"],
    "search": ["default.svc.cluster.local", "svc.cluster.local", "cluster.local"],
    "options": ["ndots:5"]
  },
  
  "mtu": 1420,
  "enable_ipv6": false,
  "enable_network_policy": true
}
```

#### 使用自定义 headcni-ipam

```json
{
  "cniVersion": "1.0.0",
  "name": "tailscale-cni",
  "type": "headcni",
  
  "headscale_url": "https://headscale.company.com",
  "tailscale_socket": "/var/run/tailscale/tailscaled.sock",
  
  "pod_cidr": "10.244.0.0/24",
  "service_cidr": "10.96.0.0/16",
  
  "ipam": {
    "type": "headcni-ipam",
    "subnet": "10.244.0.0/24",
    "gateway": "10.244.0.1",
    "allocation_strategy": "sequential",
    "routes": [
      {
        "dst": "0.0.0.0/0",
        "gw": "10.244.0.1"
      }
    ]
  },
  
  "dns": {
    "nameservers": ["10.96.0.10"],
    "search": ["default.svc.cluster.local", "svc.cluster.local", "cluster.local"],
    "options": ["ndots:5"]
  },
  
  "mtu": 1420,
  "enable_ipv6": false,
  "enable_network_policy": true
}
```

### 配置参数说明

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `headscale_url` | string | - | Headscale 服务器地址 |
| `tailscale_socket` | string | `/var/run/tailscale/tailscaled.sock` | Tailscale socket 路径 |
| `pod_cidr` | string | - | Pod 网络 CIDR |
| `service_cidr` | string | - | Service 网络 CIDR |
| `mtu` | int | 1420 | 网络 MTU |
| `enable_ipv6` | bool | false | 是否启用 IPv6 |
| `enable_network_policy` | bool | true | 是否启用网络策略 |

### IPAM 类型选择

HeadCNI 支持两种 IPAM 类型：

1. **host-local**：标准 CNI IPAM 插件，简单高效
2. **headcni-ipam**：自定义 IPAM 插件，支持高级功能

#### host-local 特点
- ✅ 简单可靠
- ✅ 性能优秀
- ✅ 标准兼容
- ❌ 功能有限

#### headcni-ipam 特点
- ✅ 多种分配策略（顺序、随机、密集打包）
- ✅ 详细统计信息
- ✅ 垃圾回收机制
- ✅ 健康检查
- ❌ 复杂度较高

## 🔧 开发

### 项目结构

```
headcni/
├── cmd/                    # 命令行工具
│   └── headcni/           # 主 CNI 插件
├── pkg/                   # 核心包
│   ├── ipam/             # IP 地址管理
│   ├── networking/       # 网络管理
│   ├── monitoring/       # 监控
│   └── utils/            # 工具函数
├── chart/                # Helm Chart
├── Dockerfile           # 容器构建
├── Makefile             # 构建脚本
└── README.md            # 项目文档
```

### 运行测试

```bash
# 运行所有测试
go test ./...

# 运行特定包的测试
go test ./pkg/ipam/...

# 运行基准测试
go test -bench=. ./pkg/ipam/
```

### 代码检查

```bash
# 格式化代码
make fmt

# 静态分析
make vet

# 代码检查
make lint
```

## 📊 监控

HeadCNI 提供以下监控指标：

- `headcni_ip_allocations_total`：IP 分配总数
- `headcni_ip_releases_total`：IP 释放总数
- `headcni_network_errors_total`：网络错误总数
- `headcni_pod_network_setup_duration_seconds`：Pod 网络设置耗时

### 启用监控

```bash
# 启动监控服务
./headcni --metrics-port=8080 --metrics-path=/metrics
```

## 🐛 故障排除

### 常见问题

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

### 日志级别

可以通过环境变量设置日志级别：

```bash
export HEADCNI_LOG_LEVEL=debug
```

## 🤝 贡献

欢迎贡献代码！请遵循以下步骤：

1. Fork 项目
2. 创建功能分支
3. 提交更改
4. 推送到分支
5. 创建 Pull Request

## 📄 许可证

本项目采用 MIT 许可证 - 查看 [LICENSE](LICENSE) 文件了解详情。

## 🔗 相关链接

- [CNI 规范](https://github.com/containernetworking/cni)
- [Tailscale 文档](https://tailscale.com/kb/)
- [Headscale 文档](https://github.com/juanfont/headscale)
- [Kubernetes 网络](https://kubernetes.io/docs/concepts/cluster-administration/networking/)
