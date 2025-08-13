package logging

import (
	"log"
	"os"
)

// Logger 定义日志接口
type Logger interface {
	Info(msg string, keysAndValues ...interface{})
	Error(msg string, keysAndValues ...interface{})
	Debug(msg string, keysAndValues ...interface{})
	Warn(msg string, keysAndValues ...interface{})
}

// SimpleLogger 是 Logger 的简单实现
type SimpleLogger struct {
	logger *log.Logger
}

// NewLogger 创建新的日志器
func NewLogger() Logger {
	return &SimpleLogger{
		logger: log.New(os.Stdout, "[HeadCNI] ", log.LstdFlags),
	}
}

func (l *SimpleLogger) Info(msg string, keysAndValues ...interface{}) {
	l.logger.Printf("[INFO] %s", msg)
}

func (l *SimpleLogger) Error(msg string, keysAndValues ...interface{}) {
	l.logger.Printf("[ERROR] %s", msg)
}

func (l *SimpleLogger) Debug(msg string, keysAndValues ...interface{}) {
	l.logger.Printf("[DEBUG] %s", msg)
}

func (l *SimpleLogger) Warn(msg string, keysAndValues ...interface{}) {
	l.logger.Printf("[WARN] %s", msg)
}
