package logging

import (
	"go.uber.org/zap/zapcore"
)

const (
	// DefaultLogFileMaxSizeMB 默认日志文件最大大小（MB）
	DefaultLogFileMaxSizeMB = 1024
	// DefaultMaxBackups 默认最大备份文件数
	DefaultMaxBackups = 7
	// DefaultMaxAge 默认日志文件保留天数
	DefaultMaxAge = 7
	// DefaultCallSkip 默认调用栈跳过层数
	DefaultCallSkip = 1
)

// Config 日志配置结构体
type Config struct {
	// LogFile 日志文件路径
	LogFile string
	// Level 日志级别
	Level zapcore.LevelEnabler
	// MaxSize 单个日志文件最大大小（MB）
	MaxSize int
	// MaxBackups 最大备份文件数
	MaxBackups int
	// MaxAge 日志文件保留天数
	MaxAge int
	// Compress 是否压缩备份文件
	Compress bool
	// CallSkip 调用栈跳过层数
	CallSkip int
	// EnableConsole 是否同时输出到控制台
	EnableConsole bool
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Level:         zapcore.InfoLevel,
		MaxSize:       100,
		MaxBackups:    DefaultMaxBackups,
		MaxAge:        DefaultMaxAge,
		Compress:      true,
		CallSkip:      DefaultCallSkip,
		EnableConsole: false,
	}
}

// WithLogFile 设置日志文件路径
func (c *Config) WithLogFile(logFile string) *Config {
	c.LogFile = logFile
	return c
}

// WithLevel 设置日志级别
func (c *Config) WithLevel(level zapcore.LevelEnabler) *Config {
	c.Level = level
	return c
}

// WithMaxSize 设置最大文件大小
func (c *Config) WithMaxSize(maxSize int) *Config {
	c.MaxSize = maxSize
	return c
}

// WithMaxBackups 设置最大备份数
func (c *Config) WithMaxBackups(maxBackups int) *Config {
	c.MaxBackups = maxBackups
	return c
}

// WithMaxAge 设置最大保留天数
func (c *Config) WithMaxAge(maxAge int) *Config {
	c.MaxAge = maxAge
	return c
}

// WithCompress 设置是否压缩
func (c *Config) WithCompress(compress bool) *Config {
	c.Compress = compress
	return c
}

// WithCallSkip 设置调用栈跳过层数
func (c *Config) WithCallSkip(callSkip int) *Config {
	c.CallSkip = callSkip
	return c
}

// WithConsole 设置是否输出到控制台
func (c *Config) WithConsole(enable bool) *Config {
	c.EnableConsole = enable
	return c
} 