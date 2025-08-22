package logging

import (
	"os"
	"testing"

	"go.uber.org/zap/zapcore"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	if config.Level != zapcore.InfoLevel {
		t.Errorf("Expected default level to be InfoLevel, got %v", config.Level)
	}
	if config.MaxSize != 100 {
		t.Errorf("Expected default max size to be 100, got %d", config.MaxSize)
	}
	if config.MaxBackups != DefaultMaxBackups {
		t.Errorf("Expected default max backups to be %d, got %d", DefaultMaxBackups, config.MaxBackups)
	}
}

func TestConfigWithOptions(t *testing.T) {
	config := DefaultConfig().
		WithLogFile("/tmp/test.log").
		WithLevel(zapcore.DebugLevel).
		WithMaxSize(200).
		WithConsole(true)

	if config.LogFile != "/tmp/test.log" {
		t.Errorf("Expected log file to be /tmp/test.log, got %s", config.LogFile)
	}
	if config.Level != zapcore.DebugLevel {
		t.Errorf("Expected level to be DebugLevel, got %v", config.Level)
	}
	if config.MaxSize != 200 {
		t.Errorf("Expected max size to be 200, got %d", config.MaxSize)
	}
	if !config.EnableConsole {
		t.Error("Expected console to be enabled")
	}
}

func TestSimpleLogger(t *testing.T) {
	logger := NewSimpleLogger()
	if logger == nil {
		t.Error("Expected logger to be created")
	}

	// 测试所有日志级别
	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")

	// 测试格式化日志
	logger.Debugf("debug: %s", "test")
	logger.Infof("info: %s", "test")
	logger.Warnf("warn: %s", "test")
	logger.Errorf("error: %s", "test")
}

func TestInitWithConfig(t *testing.T) {
	// 测试空配置文件（应该使用简单日志器）
	err := Init(nil)
	if err != nil {
		t.Errorf("Expected no error when initializing with nil config, got %v", err)
	}

	// 测试空日志文件配置
	config := DefaultConfig().WithLogFile("")
	err = Init(config)
	if err != nil {
		t.Errorf("Expected no error when initializing with empty log file, got %v", err)
	}
}

func TestInitWithFile(t *testing.T) {
	// 创建临时目录
	tempDir := "/tmp/headcni_test"
	err := os.MkdirAll(tempDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 测试文件日志配置
	config := DefaultConfig().
		WithLogFile(tempDir + "/test.log").
		WithLevel(zapcore.InfoLevel).
		WithConsole(true)

	err = Init(config)
	if err != nil {
		t.Errorf("Expected no error when initializing with valid config, got %v", err)
	}

	// 测试日志输出
	Infof("Test info message")
	Warnf("Test warning message")
	Errorf("Test error message")
}

func TestBackwardCompatibility(t *testing.T) {
	// 测试向后兼容的函数
	InitZapLog("") // 应该不会出错

	// 测试带级别的初始化
	InitZapLogWithLevel("", zapcore.InfoLevel) // 应该不会出错
} 