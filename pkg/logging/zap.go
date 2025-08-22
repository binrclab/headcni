package logging

import (
	"os"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

// shortCallerWithClassFunctionEncoder 自定义调用者编码器，包含函数名
func shortCallerWithClassFunctionEncoder(caller zapcore.EntryCaller, enc zapcore.PrimitiveArrayEncoder) {
	callerPath := caller.TrimmedPath()
	if f := runtime.FuncForPC(caller.PC); f != nil {
		name := f.Name()
		i := strings.LastIndex(name, "/")
		j := strings.Index(name[i+1:], ".")
		callerPath += " " + name[i+j+2:]
	}
	enc.AppendString(callerPath)
}

// timeEncoder 自定义时间编码器
func timeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.Format("2006-01-02 15:04:05.000"))
}

// createZapLogger 创建 zap logger 实例
func createZapLogger(config *Config) *zap.SugaredLogger {
	// 验证和调整配置参数
	if config.MaxSize > DefaultLogFileMaxSizeMB {
		config.MaxSize = DefaultLogFileMaxSizeMB
	}

	if config.MaxAge < 0 {
		config.MaxAge = 0
	}

	if config.MaxBackups < 0 {
		config.MaxBackups = 0
	}

	// 准备输出目标
	writers := []zapcore.WriteSyncer{
		zapcore.AddSync(&lumberjack.Logger{
			Filename:   config.LogFile,
			MaxSize:    config.MaxSize,
			MaxBackups: config.MaxBackups,
			MaxAge:     config.MaxAge,
			Compress:   config.Compress,
		}),
	}

	// 如果启用控制台输出，添加到输出列表
	if config.EnableConsole {
		writers = append(writers, zapcore.AddSync(os.Stdout))
	}

	// 配置编码器
	encoderConfig := zapcore.EncoderConfig{
		MessageKey:     "M",
		LevelKey:       "L",
		NameKey:        "N",
		TimeKey:        "T",
		CallerKey:      "C",
		StacktraceKey:  "S",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeTime:     timeEncoder,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   shortCallerWithClassFunctionEncoder,
		EncodeName:     zapcore.FullNameEncoder,
	}

	// 创建核心
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig),
		zapcore.NewMultiWriteSyncer(writers...),
		config.Level,
	)

	// 创建 logger 并添加选项
	logger := zap.New(
		core,
		zap.AddCaller(),
		zap.AddStacktrace(zap.DPanicLevel),
		zap.AddCallerSkip(config.CallSkip),
		zap.Development(),
	)

	return logger.Sugar()
}
