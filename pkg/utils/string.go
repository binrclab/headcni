package utils

import "math/rand"

// 预定义常用编码字符集
const (
	CharsetBase32    = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567" // RFC 4648 标准
	CharsetBase32Low = "abcdefghijklmnopqrstuvwxyz234567" // RFC 4648 标准
	CharsetBase32Hex = "0123456789ABCDEFGHIJKLMNOPQRSTUV" // Base32hex 扩展
	CharsetBase62    = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	CharsetBase64    = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	CharsetBase64URL = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	CharsetHex       = "0123456789abcdef"
	CharsetUpperHex  = "0123456789ABCDEF"
)

// RandomStringWithCharset 使用自定义字符集生成随机字符串
func RandomStringWithCharset(length int, charset string) string {
	if len(charset) == 0 {
		panic("charset must not be empty")
	}
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// 以下为各编码的快捷方法

// RandomBase32 生成标准Base32随机字符串
func RandomBase32(length int) string {
	return RandomStringWithCharset(length, CharsetBase32)
}

func RandomBase32Low(length int) string {
	return RandomStringWithCharset(length, CharsetBase32Low)
}

// RandomBase32Hex 生成Base32hex随机字符串
func RandomBase32Hex(length int) string {
	return RandomStringWithCharset(length, CharsetBase32Hex)
}

// RandomBase62 生成Base62随机字符串
func RandomBase62(length int) string {
	return RandomStringWithCharset(length, CharsetBase62)
}

// RandomBase64 生成标准Base64随机字符串
func RandomBase64(length int) string {
	return RandomStringWithCharset(length, CharsetBase64)
}

// RandomBase64URL 生成URL安全的Base64随机字符串
func RandomBase64URL(length int) string {
	return RandomStringWithCharset(length, CharsetBase64URL)
}

// RandomHex 生成小写十六进制随机字符串
func RandomHex(length int) string {
	return RandomStringWithCharset(length, CharsetHex)
}

// RandomUpperHex 生成大写十六进制随机字符串
func RandomUpperHex(length int) string {
	return RandomStringWithCharset(length, CharsetUpperHex)
}
