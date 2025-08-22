package logging

import (
	"go.uber.org/zap/zapcore"
)

// ExampleUsage 展示日志包的使用方法
func ExampleUsage() {
	// 方法1: 使用默认配置
	config := DefaultConfig().
		WithLogFile("/var/log/headcni/app.log").
		WithLevel(zapcore.InfoLevel)

	if err := Init(config); err != nil {
		panic(err)
	}

	// 使用全局日志函数
	Infof("Application started with log level: %s", "info")
	Debugf("This debug message will not be logged at info level")
	Warnf("This is a warning message")
	Errorf("This is an error message")

	// 方法2: 使用简单日志器（不写入文件）
	simpleLogger := NewSimpleLogger()
	simpleLogger.Info("Using simple logger")
	simpleLogger.Debug("Debug message from simple logger")

	// 方法3: 获取 zap logger 实例进行高级操作
	if zapLogger := GetLogger(); zapLogger != nil {
		zapLogger.With("component", "example").Info("Using zap logger directly")
	}
}

// ExampleDevelopmentConfig 开发环境配置示例
func ExampleDevelopmentConfig() *Config {
	return DefaultConfig().
		WithLogFile("").
		WithLevel(zapcore.DebugLevel).
		WithConsole(true)
}

// ExampleProductionConfig 生产环境配置示例
func ExampleProductionConfig() *Config {
	return DefaultConfig().
		WithLogFile("/var/log/headcni/production.log").
		WithLevel(zapcore.InfoLevel).
		WithMaxSize(100).
		WithMaxBackups(10).
		WithMaxAge(30).
		WithCompress(true).
		WithConsole(false)
}

// ExampleCustomConfig 自定义配置示例
func ExampleCustomConfig() *Config {
	return DefaultConfig().
		WithLogFile("/var/log/headcni/custom.log").
		WithLevel(zapcore.WarnLevel).
		WithMaxSize(50).
		WithMaxBackups(5).
		WithMaxAge(7).
		WithCompress(false).
		WithConsole(true).
		WithCallSkip(2)
} 