// Package amazonq 错误定义
// @author ygw
package amazonq

// 错误码常量
const (
	// 账号相关错误
	ErrCodeSuspended    = "SUSPENDED"     // 账号被封控
	ErrCodeTokenInvalid = "TOKEN_INVALID" // Token 无效
	ErrCodeTokenExpired = "TOKEN_EXPIRED" // Token 过期

	// 配额相关错误
	ErrCodeQuotaExceeded = "QUOTA_EXCEEDED" // 配额超限
	ErrCodeQuotaFailed   = "QUOTA_FAILED"   // 配额查询失败

	// 请求相关错误
	ErrCodeBadRequest   = "BAD_REQUEST"   // 请求参数错误
	ErrCodeUnauthorized = "UNAUTHORIZED"  // 未授权
	ErrCodeForbidden    = "FORBIDDEN"     // 禁止访问
	ErrCodeServerError  = "SERVER_ERROR"  // 服务器错误
	ErrCodeTimeout      = "TIMEOUT"       // 请求超时
	ErrCodeNetworkError = "NETWORK_ERROR" // 网络错误
)

// APIError 统一 API 错误结构
// @author ygw
type APIError struct {
	Code    string // 错误码（英文常量）
	Message string // 中文友好提示
	Detail  string // 详细信息（可选，用于日志）
}

func (e *APIError) Error() string {
	return e.Message
}

// 预定义错误实例
var (
	ErrSuspended = &APIError{
		Code:    ErrCodeSuspended,
		Message: "账号已被封控",
	}
	ErrTokenInvalid = &APIError{
		Code:    ErrCodeTokenInvalid,
		Message: "Token 无效",
	}
	ErrTokenExpired = &APIError{
		Code:    ErrCodeTokenExpired,
		Message: "Token 已过期",
	}
	ErrQuotaExceeded = &APIError{
		Code:    ErrCodeQuotaExceeded,
		Message: "配额已用尽",
	}
	ErrQuotaFailed = &APIError{
		Code:    ErrCodeQuotaFailed,
		Message: "配额查询失败",
	}
	ErrServerError = &APIError{
		Code:    ErrCodeServerError,
		Message: "服务器错误",
	}
)

// NewAPIError 创建新的 API 错误
// @author ygw
func NewAPIError(code, message string) *APIError {
	return &APIError{Code: code, Message: message}
}

// NewAPIErrorWithDetail 创建带详情的 API 错误
// @author ygw
func NewAPIErrorWithDetail(code, message, detail string) *APIError {
	return &APIError{Code: code, Message: message, Detail: detail}
}

// IsAPIError 检查是否为 API 错误
func IsAPIError(err error) bool {
	_, ok := err.(*APIError)
	return ok
}

// GetAPIError 获取 API 错误（如果是的话）
func GetAPIError(err error) *APIError {
	if apiErr, ok := err.(*APIError); ok {
		return apiErr
	}
	return nil
}

// IsErrorCode 检查错误是否为指定错误码
func IsErrorCode(err error, code string) bool {
	if apiErr := GetAPIError(err); apiErr != nil {
		return apiErr.Code == code
	}
	return false
}

// IsSuspendedError 检查错误是否为账号被封控错误
func IsSuspendedError(err error) bool {
	return IsErrorCode(err, ErrCodeSuspended)
}
