# Health Checker 优化说明

## 概述

健康检查器 (`HealthChecker`) 已经进行了全面的优化，提供了更好的可靠性、可配置性和可观测性。

## 主要优化内容

### 1. 配置管理优化

- **可配置参数**: 所有超时时间、检查间隔等都可以通过配置文件自定义
- **配置验证**: 内置配置验证逻辑，确保参数合理性
- **配置合并**: 支持基础配置和覆盖配置的合并
- **配置文件支持**: 支持从 JSON 文件加载和保存配置

```go
// 默认配置
config := health.DefaultConfig()

// 自定义配置
customConfig := &health.Config{
    Port:                    ":9090",
    HealthCheckInterval:     15 * time.Second,
    MaxConsecutiveFailures:  5,
    EnableMetrics:           true,
}

// 合并配置
mergedConfig := health.MergeConfig(baseConfig, customConfig)
```

### 2. 并发安全性改进

- **原子操作**: 使用 `sync/atomic` 包确保计数器的线程安全
- **读写锁**: 使用 `sync.RWMutex` 保护状态数据
- **并发检查**: 健康检查项可以并发执行，提高响应速度
- **优雅关闭**: 支持优雅关闭，确保资源正确释放

### 3. 错误处理和重试机制

- **超时控制**: 每个检查项都有独立的超时控制
- **失败计数**: 原子操作记录连续失败次数
- **恢复机制**: 自动触发恢复流程，防止重复触发
- **上下文支持**: 所有操作都支持上下文取消

### 4. 指标收集和监控

- **详细指标**: 收集总检查次数、成功/失败次数、恢复尝试次数等
- **性能指标**: 记录检查耗时、运行时间等
- **指标端点**: 提供 `/metrics` 端点暴露指标数据
- **状态查询**: 提供 `GetStatus()` 方法获取当前状态

### 5. 网络检查优化

- **接口状态检查**: 不仅检查接口存在性，还检查接口状态
- **路由检查**: 验证 Tailscale 路由是否正确配置
- **非阻塞连通性测试**: 连通性测试不阻塞主检查流程
- **更安全的清理脚本**: 改进的网络接口清理逻辑

### 6. 恢复机制增强

- **防重复触发**: 使用原子操作防止重复触发恢复
- **超时控制**: 恢复操作有独立的超时控制
- **上下文感知**: 恢复操作支持上下文取消
- **失败计数器重置**: 恢复完成后重置失败计数器

### 7. HTTP 服务优化

- **超时设置**: HTTP 服务器配置了读写超时和空闲超时
- **JSON 响应**: 使用标准 JSON 库替代手动字符串拼接
- **结构化响应**: 返回结构化的健康状态信息
- **指标集成**: 健康检查响应包含指标信息

## 使用示例

### 基本使用

```go
// 创建健康检查器
healthChecker := health.NewHealthChecker(ipamManager, networkManager, nil)

// 启动
go healthChecker.Start()

// 获取状态
status := healthChecker.GetStatus()
fmt.Printf("Status: %s, Uptime: %v\n", status.Status, status.Uptime)

// 优雅关闭
healthChecker.Stop()
```

### 配置管理

```go
// 从文件加载配置
config, err := health.LoadConfigFromFile("config.json")
if err != nil {
    config = health.DefaultConfig()
}

// 验证配置
if err := health.ValidateConfig(config); err != nil {
    log.Fatal(err)
}

// 创建健康检查器
healthChecker := health.NewHealthChecker(ipamManager, networkManager, config)
```

### 监控集成

```go
// 定期获取状态
ticker := time.NewTicker(30 * time.Second)
defer ticker.Stop()

for range ticker.C {
    status := healthChecker.GetStatus()
    
    // 检查指标
    if status.Metrics != nil {
        fmt.Printf("Success Rate: %.2f%%\n", 
            float64(status.Metrics.SuccessfulChecks) / 
            float64(status.Metrics.TotalChecks) * 100)
    }
    
    // 检查失败次数
    if status.ConsecutiveFailures > 5 {
        fmt.Printf("WARNING: High failure count: %d\n", 
            status.ConsecutiveFailures)
    }
}
```

## API 端点

### `/healthz`
- **方法**: GET
- **功能**: 综合健康检查
- **响应**: JSON 格式的健康状态
- **状态码**: 200 (健康) / 503 (不健康)

### `/readyz`
- **方法**: GET
- **功能**: 就绪检查
- **响应**: "ready" 或错误信息
- **状态码**: 200 (就绪) / 503 (未就绪)

### `/livez`
- **方法**: GET
- **功能**: 存活检查
- **响应**: "alive" 或错误信息
- **状态码**: 200 (存活) / 503 (未存活)

### `/metrics`
- **方法**: GET
- **功能**: 指标数据
- **响应**: JSON 格式的指标信息
- **状态码**: 200

## 配置参数说明

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `port` | string | ":8081" | HTTP 服务端口 |
| `healthCheckInterval` | duration | "30s" | 定期检查间隔 |
| `healthCheckTimeout` | duration | "15s" | 健康检查超时 |
| `readinessTimeout` | duration | "10s" | 就绪检查超时 |
| `livenessTimeout` | duration | "5s" | 存活检查超时 |
| `maxConsecutiveFailures` | int | 3 | 最大连续失败次数 |
| `recoveryTimeout` | duration | "60s" | 恢复操作超时 |
| `tailscaleRestartTimeout` | duration | "30s" | Tailscale 重启超时 |
| `enableMetrics` | bool | true | 是否启用指标收集 |

## 性能优化

1. **并发检查**: 健康检查项并发执行，减少总检查时间
2. **原子操作**: 使用原子操作减少锁竞争
3. **非阻塞操作**: 非关键检查项使用非阻塞方式
4. **内存优化**: 避免不必要的内存分配
5. **超时控制**: 防止长时间阻塞

## 可靠性改进

1. **优雅关闭**: 支持优雅关闭，确保资源正确释放
2. **错误恢复**: 自动错误恢复机制
3. **状态监控**: 详细的状态监控和指标收集
4. **配置验证**: 配置参数验证，防止错误配置
5. **日志记录**: 详细的日志记录，便于问题排查

## 向后兼容性

优化后的健康检查器保持了与原有 API 的兼容性，现有的集成代码只需要很小的修改即可使用新功能。 