# HeadCNI Daemon

HeadCNI Daemon 是一个 Kubernetes CNI 插件，使用 Tailscale 为节点间提供安全、加密的网络通信。

## 文件结构（按子命令分割）

```
headcni-daemon/
├── main.go          # 程序入口点
├── root.go          # 根命令（主命令）和标志定义
├── version.go       # version 子命令
├── configcmd.go     # config 子命令
├── health.go        # health 子命令
├── config.go        # 配置管理功能
├── daemon.go        # 守护进程核心逻辑
├── network.go       # 网络相关功能
├── config/          # 配置文件目录
│   ├── config.go    # 配置结构定义
│   ├── default.yaml # 默认配置文件
│   └── README.md    # 配置说明
└── README.md        # 本文档
```

## 文件说明

### main.go
- 程序的主入口点
- 创建并执行主命令

### root.go
- 定义命令行标志结构 `CommandLineFlags`
- 创建根命令 `NewHeadCNIDaemonCommand()`
- 添加各种命令行标志（通用、监控、高级）
- 注册所有子命令

### version.go
- `version` 子命令实现
- 显示版本信息

### configcmd.go
- `config` 子命令实现
- 包含 `config validate` 和 `config show` 子命令

### health.go
- `health` 子命令实现
- 执行健康检查

### config.go
- 配置加载和管理逻辑
- 支持多级配置优先级：命令行参数 > 环境变量 > 配置文件 > 默认值
- 配置合并和覆盖逻辑

### daemon.go
- 守护进程的核心运行逻辑
- 信号处理和优雅关闭
- Kubernetes 客户端初始化
- CNI 配置文件初始化
- 控制器和节点监听器启动

### network.go
- Tailscale 网络初始化
- 网络接口配置
- 节点注解信息上传
- 网络相关的常量定义

## 命令结构

```
headcni-daemon                    # 根命令（运行守护进程）
├── version                       # 显示版本信息
├── config                        # 配置管理
│   ├── validate                  # 验证配置文件
│   └── show                      # 显示当前配置
└── health                        # 健康检查
```

## 配置优先级

1. **命令行参数** (最高优先级) - 用于调试和临时覆盖
2. **环境变量** - 容器化部署常用
3. **配置文件** - 持久化设置
4. **默认常量** (最低优先级) - 回退值

## 使用方法

```bash
# 基本运行
./headcni-daemon

# 使用配置文件
./headcni-daemon --config /path/to/config.yaml

# 调试模式
./headcni-daemon --log-level debug --tailscale-url https://example.com

# 查看版本
./headcni-daemon version

# 显示配置
./headcni-daemon config show

# 验证配置文件
./headcni-daemon config validate --config /path/to/config.yaml

# 健康检查
./headcni-daemon health
```

## 环境变量

- `TAILSCALE_SOCKET_PATH`: Tailscale socket 路径
- `TAILSCALE_MTU`: Tailscale MTU 值
- `HEADSCALE_URL`: Headscale 服务器 URL
- `HEADSCALE_AUTH_KEY`: Headscale API 密钥
- `POD_CIDR`: Pod CIDR
- `SERVICE_CIDR`: Service CIDR
- `LOG_LEVEL`: 日志级别
- `MONITORING_ENABLED`: 是否启用监控
- `METRICS_PORT`: 监控端口

## 开发说明

- 所有文件都在 `package main` 中
- 使用 `klog` 进行日志记录
- 使用 `cobra` 进行命令行处理
- 使用 `pkg/errors` 进行错误包装
- 按子命令分割文件，便于维护和扩展
- 遵循 Go 代码规范和最佳实践 