package logging

import (
	"fmt"
	"log"
	"os"
	"path"
	"runtime"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	globalLogger *zap.SugaredLogger
	initOnce     sync.Once
)

// Logger 定义日志接口
type Logger interface {
	Debug(msg string, keysAndValues ...interface{})
	Info(msg string, keysAndValues ...interface{})
	Warn(msg string, keysAndValues ...interface{})
	Error(msg string, keysAndValues ...interface{})
	Debugf(template string, args ...interface{})
	Infof(template string, args ...interface{})
	Warnf(template string, args ...interface{})
	Errorf(template string, args ...interface{})
}

// SimpleLogger 是 Logger 的简单实现，用于开发环境或测试
type SimpleLogger struct {
	logger *log.Logger
}

// NewSimpleLogger 创建简单的日志器
func NewSimpleLogger() Logger {
	return &SimpleLogger{
		logger: log.New(os.Stdout, "[HeadCNI] ", log.LstdFlags),
	}
}

func (l *SimpleLogger) Debug(msg string, keysAndValues ...interface{}) {
	l.logger.Printf("[DEBUG] %s", msg)
}

func (l *SimpleLogger) Info(msg string, keysAndValues ...interface{}) {
	l.logger.Printf("[INFO] %s", msg)
}

func (l *SimpleLogger) Warn(msg string, keysAndValues ...interface{}) {
	l.logger.Printf("[WARN] %s", msg)
}

func (l *SimpleLogger) Error(msg string, keysAndValues ...interface{}) {
	l.logger.Printf("[ERROR] %s", msg)
}

func (l *SimpleLogger) Debugf(template string, args ...interface{}) {
	l.logger.Printf("[DEBUG] "+template, args...)
}

func (l *SimpleLogger) Infof(template string, args ...interface{}) {
	l.logger.Printf("[INFO] "+template, args...)
}

func (l *SimpleLogger) Warnf(template string, args ...interface{}) {
	l.logger.Printf("[WARN] "+template, args...)
}

func (l *SimpleLogger) Errorf(template string, args ...interface{}) {
	l.logger.Printf("[ERROR] "+template, args...)
}

// Init 初始化日志系统
func Init(config *Config) error {
	if config == nil {
		config = DefaultConfig()
	}

	if config.LogFile == "" {
		// 如果没有指定日志文件，使用简单日志器
		log.Println("No log file specified, using simple logger")
		return nil
	}

	// 确保日志目录存在
	logDir := path.Dir(config.LogFile)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory %s: %v", logDir, err)
	}

	initOnce.Do(func() {
		globalLogger = createZapLogger(config)
	})

	return nil
}

// InitWithLevel 使用指定级别初始化日志（向后兼容）
func InitWithLevel(logFile string, level zapcore.LevelEnabler) error {
	config := DefaultConfig().
		WithLogFile(logFile).
		WithLevel(level)
	return Init(config)
}

// InitZapLog 初始化日志配置（向后兼容）
func InitZapLog(logFile string) {
	if logFile == "" {
		log.Println("logFile is null, do not init zap log")
		return
	}

	config := DefaultConfig().
		WithLogFile(logFile).
		WithLevel(zapcore.DebugLevel)

	if err := Init(config); err != nil {
		log.Printf("Failed to initialize logging: %v", err)
	}
}

// InitZapLogWithLevel 初始化日志配置，支持自定义日志级别（向后兼容）
func InitZapLogWithLevel(logFile string, level zapcore.LevelEnabler) {
	if logFile == "" {
		log.Println("logFile is null, do not init zap log")
		return
	}

	config := DefaultConfig().
		WithLogFile(logFile).
		WithLevel(level)

	if err := Init(config); err != nil {
		log.Printf("Failed to initialize logging: %v", err)
	}
}

// GetLogger 获取当前的 zap logger 实例
func GetLogger() *zap.SugaredLogger {
	return globalLogger
}

// 全局日志函数，向后兼容
func Debugf(template string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.Debugf(template, args...)
	} else {
		logWithCaller("DEBUG", template, args...)
	}
}

func Infof(template string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.Infof(template, args...)
	} else {
		logWithCaller("INFO", template, args...)
	}
}

func Warnf(template string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.Warnf(template, args...)
	} else {
		logWithCaller("WARN", template, args...)
	}
}

func Errorf(template string, args ...interface{}) {
	if globalLogger != nil {
		globalLogger.Errorf(template, args...)
	} else {
		logWithCaller("ERROR", template, args...)
	}
}

// logWithCaller 带调用者信息的日志输出
func logWithCaller(level, template string, args ...interface{}) {
	_, file, line, _ := runtime.Caller(2) // 跳过一层调用
	prefix := fmt.Sprintf("[%s] %s:%d: ", level, path.Base(file), line)
	log.Printf(prefix+template, args...)
}
