package utils

import (
	"strconv"
	"strings"
	"time"

	"github.com/binrclab/headcni/pkg/logging"
)

func ParseTimeout(timeout string) time.Duration {
	timeout = strings.ToLower(strings.TrimSpace(timeout))

	// 时间单位映射表
	units := map[byte]time.Duration{
		's': time.Second,
		'm': time.Minute,
		'h': time.Hour,
		'd': 24 * time.Hour,
		'w': 7 * 24 * time.Hour,
		'y': 365 * 24 * time.Hour,
	}

	// 检查是否以时间单位结尾
	if len(timeout) > 1 {
		if unit, exists := units[timeout[len(timeout)-1]]; exists {
			if value, err := strconv.Atoi(timeout[:len(timeout)-1]); err == nil {
				return time.Duration(value) * unit
			}
		}
	}

	// 纯数值格式：默认使用秒
	if value, err := strconv.Atoi(timeout); err == nil {
		return time.Duration(value) * time.Second
	}

	// 解析失败，返回默认值
	logging.Warnf("Invalid timeout format: %s, defaulting to 30s", timeout)
	return 30 * time.Second
}
