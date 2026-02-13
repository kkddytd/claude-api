package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Kiro IDE 版本号
const KiroVersion = "0.7.45"

// GenerateKiroMachineID 生成 64 位小写十六进制 Machine ID
// 按照 Kiro IDE 规范：SHA256(32字节随机数)
// @author ygw
func GenerateKiroMachineID() string {
	data := make([]byte, 32)
	_, err := rand.Read(data)
	if err != nil {
		// 如果随机数生成失败，使用备用方案
		return generateFallbackMachineID()
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// generateFallbackMachineID 备用 Machine ID 生成方案
func generateFallbackMachineID() string {
	// 使用时间戳和固定前缀生成
	data := []byte("kiro-fallback-machine-id-" + fmt.Sprint(randomInt64()))
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// randomInt64 生成随机 int64
func randomInt64() int64 {
	var b [8]byte
	rand.Read(b[:])
	return int64(b[0]) | int64(b[1])<<8 | int64(b[2])<<16 | int64(b[3])<<24 |
		int64(b[4])<<32 | int64(b[5])<<40 | int64(b[6])<<48 | int64(b[7])<<56
}

// BuildKiroUserAgent 构建 Kiro IDE 标准 User-Agent
// 格式: KiroIDE-{版本}-{machineId}
// 用于: OIDC 请求、Social 刷新、获取配额
// @author ygw
func BuildKiroUserAgent(machineId string) string {
	if machineId == "" {
		machineId = GenerateKiroMachineID()
	}
	return fmt.Sprintf("KiroIDE-%s-%s", KiroVersion, machineId)
}

// BuildAmazonQUserAgent 构建 Amazon Q 聊天请求 User-Agent
// 格式: aws-sdk-js/1.0.7 KiroIDE-{版本}-{machineId}
// @author ygw
func BuildAmazonQUserAgent(machineId string) string {
	if machineId == "" {
		machineId = GenerateKiroMachineID()
	}
	return fmt.Sprintf("aws-sdk-js/1.0.7 KiroIDE-%s-%s", KiroVersion, machineId)
}

// BuildAmazonQXAmzUserAgent 构建 Amazon Q 的 X-Amz-User-Agent 头
// 格式: aws-sdk-js/1.0.7 KiroIDE-{版本}-{machineId}
// @author ygw
func BuildAmazonQXAmzUserAgent(machineId string) string {
	// 与 User-Agent 相同格式
	return BuildAmazonQUserAgent(machineId)
}

// ValidateMachineID 验证 Machine ID 格式是否有效
// 有效格式: 64 位小写十六进制字符
// @author ygw
func ValidateMachineID(machineId string) bool {
	if len(machineId) != 64 {
		return false
	}
	for _, c := range machineId {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// EnsureMachineID 确保有有效的 Machine ID，如果无效则生成新的
// 注意：此函数只返回有效的 machineId，不负责持久化
// 对于历史账户（machineId 为空），调用方需要负责将生成的 machineId 保存到数据库
// @author ygw
func EnsureMachineID(machineId *string) string {
	if machineId != nil && ValidateMachineID(*machineId) {
		return *machineId
	}
	return GenerateKiroMachineID()
}

// GetOrCreateMachineID 获取或创建 Machine ID，返回 (machineId, needsSave)
// needsSave 为 true 表示调用方需要将此 machineId 保存到数据库
// @author ygw
func GetOrCreateMachineID(machineId *string) (string, bool) {
	if machineId != nil && ValidateMachineID(*machineId) {
		return *machineId, false // 已有有效 ID，无需保存
	}
	return GenerateKiroMachineID(), true // 新生成的 ID，需要保存
}

