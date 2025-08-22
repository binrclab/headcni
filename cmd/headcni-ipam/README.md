# HeadCNI IPAM Plugin

HeadCNI IPAM 插件是一个自定义的 CNI IPAM 插件，用于为 HeadCNI 网络插件提供 IP 地址管理功能。

## 功能特性

- 支持多种 IP 分配策略（sequential, random）
- 支持 ranges 配置格式
- 自动网关生成
- Pod 信息解析
- 完整的 ADD/DEL/CHECK 操作支持
- 详细的日志记录

## 配置格式

### 基本配置

```json
{
  "cniVersion": "1.0.0",
  "name": "headcni",
  "ipam": {
    "type": "headcni-ipam",
    "subnet": "10.244.0.0/16",
    "gateway": "10.244.0.1",
    "dataDir": "/var/lib/cni/networks/headcni",
    "allocation_strategy": "sequential"
  }
}
```

### 使用 ranges 配置

```json
{
  "cniVersion": "1.0.0",
  "name": "headcni",
  "ipam": {
    "type": "headcni-ipam",
    "ranges": [
      [
        {
          "subnet": "10.244.0.0/16",
          "gateway": "10.244.0.1"
        }
      ]
    ],
    "dataDir": "/var/lib/cni/networks/headcni",
    "allocation_strategy": "sequential"
  }
}
```

## 配置参数

### IPAM 配置

- `type`: IPAM 类型，必须为 "headcni-ipam"
- `subnet`: 子网 CIDR（如 "10.244.0.0/16"）
- `gateway`: 网关 IP 地址（可选，自动生成）
- `ranges`: IP 范围配置数组
- `dataDir`: 数据存储目录（默认："/var/lib/cni/networks/headcni"）
- `allocation_strategy`: 分配策略（默认："sequential"）

### Ranges 配置

- `subnet`: 子网 CIDR
- `rangeStart`: 起始 IP 地址（可选）
- `rangeEnd`: 结束 IP 地址（可选）
- `gateway`: 网关 IP 地址（可选）

## 分配策略

### Sequential（顺序分配）
按顺序分配 IP 地址，从子网的第一个可用 IP 开始。

### Random（随机分配）
随机分配 IP 地址，提高安全性。

## 使用示例

### 在 CNI 配置中使用

```json
{
  "cniVersion": "1.0.0",
  "name": "headcni",
  "plugins": [
    {
      "type": "headcni",
      "mtu": 1500,
      "ipam": {
        "type": "headcni-ipam",
        "ranges": [
          [
            {
              "subnet": "10.244.0.0/16",
              "gateway": "10.244.0.1"
            }
          ]
        ],
        "dataDir": "/var/lib/cni/networks/headcni",
        "allocation_strategy": "sequential"
      }
    }
  ]
}
```

## 构建和安装

```bash
# 构建插件
go build -o headcni-ipam ./cmd/headcni-ipam

# 安装到 CNI 插件目录
sudo cp headcni-ipam /opt/cni/bin/
sudo chmod +x /opt/cni/bin/headcni-ipam
```

## 测试

```bash
# 运行单元测试
go test ./cmd/headcni-ipam -v

# 运行集成测试
go test ./pkg/ipam -v
```

## 日志

插件使用 klog 进行日志记录，可以通过设置环境变量控制日志级别：

```bash
export KLOG_V=4  # 设置详细日志级别
```

## 故障排除

### 常见问题

1. **IP 分配失败**
   - 检查子网配置是否正确
   - 确认 IPAM 管理器是否正常运行
   - 查看日志获取详细错误信息

2. **网关配置错误**
   - 确保网关 IP 在子网范围内
   - 检查网关 IP 格式是否正确

3. **Pod 信息解析失败**
   - 确认 CNI_ARGS 包含必要的 Pod 信息
   - 检查 Kubernetes 集成配置

### 调试模式

启用详细日志记录：

```bash
export KLOG_V=4
export KLOG_LOGTOSTDERR=true
```

## 开发

### 添加新功能

1. 在 `main.go` 中添加新功能
2. 更新测试文件
3. 更新文档

### 贡献

欢迎提交 Issue 和 Pull Request！

## 许可证

本项目采用 MIT 许可证。 