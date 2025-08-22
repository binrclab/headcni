# HeadCNI Logging Package

这是一个基于 Zap 的高性能日志包，提供了灵活的配置选项和简单的 API。

## 特性

- 🚀 基于 Zap 的高性能日志记录
- ⚙️ 灵活的配置选项
- 🔄 自动日志轮转和压缩
- 🖥️ 支持同时输出到文件和控制台
- 🛠️ 向后兼容的 API
- 📝 详细的调用者信息

## 快速开始

### 基本使用

```go
package main

import (
    "github.com/binrclab/headcni/pkg/logging"
    "go.uber.org/zap/zapcore"
)

func main() {
    // 初始化日志
    config := logging.DefaultConfig().
        WithLogFile("/var/log/headcni/app.log").
        WithLevel(zapcore.InfoLevel)

    if err := logging.Init(config); err != nil {
        panic(err)
    }

    // 使用日志
    logging.Infof("Application started")
    logging.Warnf("This is a warning")
    logging.Errorf("This is an error")
}
```

### 开发环境配置

```go
// 开发环境：输出到控制台，调试级别
config := logging.DefaultConfig().
    WithLogFile("").  // 不写入文件
    WithLevel(zapcore.DebugLevel).
    WithConsole(true)

logging.Init(config)
```

### 生产环境配置

```go
// 生产环境：写入文件，信息级别，启用轮转
config := logging.DefaultConfig().
    WithLogFile("/var/log/headcni/production.log").
    WithLevel(zapcore.InfoLevel).
    WithMaxSize(100).      // 100MB
    WithMaxBackups(10).    // 保留10个备份
    WithMaxAge(30).        // 保留30天
    WithCompress(true).    // 压缩备份
    WithConsole(false)     // 不输出到控制台

logging.Init(config)
```

## API 参考

### 配置选项

| 方法 | 描述 | 默认值 |
|------|------|--------|
| `WithLogFile(path)` | 设置日志文件路径 | "" |
| `WithLevel(level)` | 设置日志级别 | InfoLevel |
| `WithMaxSize(size)` | 设置单个文件最大大小(MB) | 100 |
| `WithMaxBackups(count)` | 设置最大备份文件数 | 7 |
| `WithMaxAge(days)` | 设置日志保留天数 | 7 |
| `WithCompress(compress)` | 是否压缩备份文件 | true |
| `WithCallSkip(skip)` | 设置调用栈跳过层数 | 1 |
| `WithConsole(enable)` | 是否同时输出到控制台 | false |

### 日志级别

- `zapcore.DebugLevel`: 调试信息
- `zapcore.InfoLevel`: 一般信息
- `zapcore.WarnLevel`: 警告信息
- `zapcore.ErrorLevel`: 错误信息

### 全局函数

```go
// 格式化日志
logging.Debugf("Debug message: %s", value)
logging.Infof("Info message: %s", value)
logging.Warnf("Warning message: %s", value)
logging.Errorf("Error message: %s", value)
```

### 获取 Logger 实例

```go
// 获取 zap logger 进行高级操作
if logger := logging.GetLogger(); logger != nil {
    logger.With("component", "api").Info("API request received")
}
```

### 简单日志器

```go
// 创建简单日志器（不写入文件，用于测试）
simpleLogger := logging.NewSimpleLogger()
simpleLogger.Info("Test message")
simpleLogger.Debug("Debug message")
```

## 向后兼容

为了保持向后兼容，以下旧 API 仍然可用：

```go
// 旧 API（不推荐使用）
logging.InitZapLog("/var/log/app.log")
logging.InitWithLevel("/var/log/app.log", zapcore.InfoLevel)
```

## 最佳实践

1. **开发环境**: 使用控制台输出和调试级别
2. **生产环境**: 使用文件输出和信息级别，启用日志轮转
3. **错误处理**: 总是检查 `Init()` 的返回值
4. **性能**: 在生产环境中避免使用调试级别
5. **存储**: 定期清理旧的日志文件

## 示例

更多使用示例请参考 `example.go` 文件。 