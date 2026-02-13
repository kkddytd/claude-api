package amazonq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"claude-api/internal/auth"
	"claude-api/internal/config"
	"claude-api/internal/logger"
	proxypool "claude-api/internal/proxy"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/proxy"
)

// streamBufferPool 流式响应缓冲区池，复用 8KB 缓冲区减少内存分配
// @author ygw - 高并发优化
var streamBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 8192)
		return &buf
	},
}

const (
	// 使用正确的 Amazon Q / CodeWhisperer Streaming API 端点
	AmazonQEndpoint = "https://q.us-east-1.amazonaws.com/"
	MaxRetries      = 3
	RetryDelay      = 500 * time.Millisecond
)

// NonRetriableError 表示不应重试的错误
type NonRetriableError struct {
	Code         string // 错误代码
	Message      string // 中文友好提示
	Hint         string // 解决建议
	IsRequestErr bool   // true 表示请求本身的问题（换号也没用），false 表示账号问题（可以换号）
}

func (e *NonRetriableError) Error() string {
	return e.Message
}

// IsNonRetriable 检查错误是否为不可重试错误
func IsNonRetriable(err error) bool {
	_, ok := err.(*NonRetriableError)
	return ok
}

// IsRequestError 检查是否为请求本身的错误（换号也没用的错误）
func IsRequestError(err error) bool {
	if nrErr, ok := err.(*NonRetriableError); ok {
		return nrErr.IsRequestErr
	}
	return false
}

// 定义不可重试的错误映射 - 按匹配优先级排列
// IsRequestErr=true 表示请求本身的问题（换号也没用），false 表示账号问题（可以换号重试）
var nonRetriableErrors = []struct {
	Pattern string
	Error   NonRetriableError
}{
	// ===== 请求相关错误（换号也没用，直接返回）=====
	// 内容长度超限
	{
		Pattern: "CONTENT_LENGTH_EXCEEDS_THRESHOLD",
		Error: NonRetriableError{
			Code:         "CONTENT_LENGTH_EXCEEDS_THRESHOLD",
			Message:      "对话内容超出长度限制啦",
			Hint:         "老板您好～当前会话积累的内容太长了，已超出上游服务的处理能力。建议您新建一个会话或者使用/compact命令压缩再继续，或者将内容分批发送。给您添麻烦了，实在抱歉！🙏",
			IsRequestErr: true,
		},
	},
	{
		Pattern: "Input is too long",
		Error: NonRetriableError{
			Code:         "INPUT_TOO_LONG",
			Message:      "输入内容有点长了",
			Hint:         "老板您好～这次发送的内容超出长度限制了。麻烦您新建会话重新开始，或者把内容拆分成小块发送。非常抱歉给您带来不便！🙏",
			IsRequestErr: true,
		},
	},
	// 请求格式错误
	{
		Pattern: "Improperly formed request",
		Error: NonRetriableError{
			Code:         "INVALID_REQUEST",
			Message:      "会话内容解析异常",
			Hint:         "老板您好～当前会话出了点小问题，可能是对话格式有些异常。建议您新建一个会话试试，后续我们会持续优化。给您添麻烦了，抱歉！🙏",
			IsRequestErr: true,
		},
	},

	// ===== 账号相关错误（可以换号重试）=====
	// 月度配额超限
	{
		Pattern: "ServiceQuotaExceededException",
		Error: NonRetriableError{
			Code:         "QUOTA_EXCEEDED",
			Message:      "当前通道配额已用尽",
			Hint:         "老板您好～当前使用的通道本月配额已耗尽，系统正在为您自动切换其他通道。请稍候，马上就好！🙏",
			IsRequestErr: false,
		},
	},
	// 账号暂停
	{
		Pattern: "TEMPORARILY_SUSPENDED",
		Error: NonRetriableError{
			Code:         "TEMPORARILY_SUSPENDED",
			Message:      "当前通道临时受限",
			Hint:         "老板您好～当前使用的通道被上游临时限制了，系统正在为您自动切换其他通道。如果问题持续，请稍后再试。非常抱歉给您带来不便！🙏",
			IsRequestErr: false,
		},
	},
	{
		Pattern: "temporarily is suspended",
		Error: NonRetriableError{
			Code:         "TEMPORARILY_SUSPENDED",
			Message:      "当前通道临时受限",
			Hint:         "老板您好～当前使用的通道被上游临时限制了，系统正在为您自动切换其他通道。如果问题持续，请稍后再试。非常抱歉给您带来不便！🙏",
			IsRequestErr: false,
		},
	},
	// 访问被拒绝
	{
		Pattern: "AccessDeniedException",
		Error: NonRetriableError{
			Code:         "ACCESS_DENIED",
			Message:      "访问权限受限",
			Hint:         "老板您好～当前通道的访问权限出现问题，系统正在尝试其他通道。如果问题持续，请联系管理员检查配置。给您添麻烦了！🙏",
			IsRequestErr: false,
		},
	},
	// 认证失败
	{
		Pattern: "UnauthorizedException",
		Error: NonRetriableError{
			Code:         "UNAUTHORIZED",
			Message:      "认证信息已失效",
			Hint:         "老板您好～当前通道的认证信息失效了，系统正在尝试刷新或切换通道。如果问题持续，请联系管理员重新配置。抱歉给您带来不便！🙏",
			IsRequestErr: false,
		},
	},
	{
		Pattern: "ExpiredTokenException",
		Error: NonRetriableError{
			Code:         "EXPIRED_TOKEN",
			Message:      "访问令牌已过期",
			Hint:         "老板您好～当前通道的令牌过期了，系统正在自动刷新。请稍后重试，如果问题持续请联系管理员。感谢您的耐心！🙏",
			IsRequestErr: false,
		},
	},
	// 资源不存在
	{
		Pattern: "ResourceNotFoundException",
		Error: NonRetriableError{
			Code:         "RESOURCE_NOT_FOUND",
			Message:      "请求的资源不存在",
			Hint:         "老板您好～请求的资源暂时找不到了，可能是配置有变动。建议您刷新页面重试，或联系管理员检查。抱歉给您添麻烦！🙏",
			IsRequestErr: true,
		},
	},
	// 无效模型 - 需要放在 ValidationException 之前，优先匹配
	{
		Pattern: "INVALID_MODEL_ID",
		Error: NonRetriableError{
			Code:         "INVALID_MODEL",
			Message:      "请求的模型不可用",
			Hint:         "老板您好～您请求的模型当前不可用或不受支持。请尝试使用其他模型，如 claude-sonnet-4-5-20250929。给您添麻烦了！🙏",
			IsRequestErr: true,
		},
	},
	{
		Pattern: "Invalid model",
		Error: NonRetriableError{
			Code:         "INVALID_MODEL",
			Message:      "请求的模型不可用",
			Hint:         "老板您好～您请求的模型当前不可用或不受支持。请尝试使用其他模型，如 claude-sonnet-4-5-20250929。给您添麻烦了！🙏",
			IsRequestErr: true,
		},
	},
	// 验证异常（通用）- 通常是请求参数问题，放在具体错误之后
	{
		Pattern: "ValidationException",
		Error: NonRetriableError{
			Code:         "VALIDATION_ERROR",
			Message:      "请求参数校验失败",
			Hint:         "老板您好～这次请求的参数没能通过校验，可能是格式或参数有些问题。建议您新建会话重试，或调整一下发送的内容。给您添麻烦了！🙏",
			IsRequestErr: true,
		},
	},
	// 通用未预期错误
	{
		Pattern: "Encountered an unexpected error",
		Error: NonRetriableError{
			Code:         "UNEXPECTED_ERROR",
			Message:      "服务暂时异常",
			Hint:         "老板您好～上游服务遇到了临时问题，系统正在为您自动切换通道。如果问题持续，请稍后重试。给您添麻烦了！🙏",
			IsRequestErr: false,
		},
	},
	// 流量限制/模型容量不足
	{
		Pattern: "ThrottlingException",
		Error: NonRetriableError{
			Code:         "THROTTLING",
			Message:      "服务繁忙，请稍后重试",
			Hint:         "老板您好～当前上游服务流量较大，模型容量暂时不足。系统正在为您自动切换其他通道，请稍候片刻。感谢您的耐心等待！🙏",
			IsRequestErr: false,
		},
	},
	{
		Pattern: "INSUFFICIENT_MODEL_CAPACITY",
		Error: NonRetriableError{
			Code:         "INSUFFICIENT_CAPACITY",
			Message:      "模型容量不足",
			Hint:         "老板您好～当前模型正在经历高峰流量，容量暂时不足。系统正在为您自动切换其他通道，请稍候片刻。感谢您的耐心等待！🙏",
			IsRequestErr: false,
		},
	},
}

// Client 表示 Amazon Q API 客户端
type Client struct {
	httpClient    *http.Client
	cfg           *config.Config
	proxyPool     *proxypool.ProxyPool // 代理池
	baseTransport *http.Transport      // 基础 Transport（无代理配置）
}

// NewClient 创建新的 Amazon Q 客户端
// 高并发 HTTP 连接池配置常量
// @author ygw - 高并发优化
const (
	// DefaultMaxIdleConns 默认最大空闲连接数（支持高并发）
	DefaultMaxIdleConns = 200
	// DefaultMaxIdleConnsPerHost 每个主机的最大空闲连接数
	DefaultMaxIdleConnsPerHost = 100
	// DefaultIdleConnTimeout 空闲连接超时时间
	DefaultIdleConnTimeout = 120 * time.Second
	// DefaultResponseHeaderTimeout 响应头超时时间
	DefaultResponseHeaderTimeout = 60 * time.Second
	// DefaultTLSHandshakeTimeout TLS 握手超时时间
	DefaultTLSHandshakeTimeout = 15 * time.Second
)

func NewClient(cfg *config.Config) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	// 高并发优化：增加连接池大小
	// 支持更多的并发请求，减少连接建立开销
	transport.MaxIdleConns = DefaultMaxIdleConns               // 最大空闲连接总数
	transport.MaxIdleConnsPerHost = DefaultMaxIdleConnsPerHost // 每个主机的最大空闲连接
	transport.MaxConnsPerHost = 0                              // 不限制每个主机的总连接数

	// 优化连接设置以防止 EOF 错误和提高连接复用率
	transport.IdleConnTimeout = DefaultIdleConnTimeout             // 空闲连接保持时间
	transport.ResponseHeaderTimeout = DefaultResponseHeaderTimeout // 等待响应头的超时时间
	transport.ExpectContinueTimeout = 1 * time.Second              // Expect: 100-continue 的超时时间
	transport.TLSHandshakeTimeout = DefaultTLSHandshakeTimeout     // TLS 握手超时
	transport.DisableKeepAlives = false                            // 保持连接活跃（重要！）
	transport.ForceAttemptHTTP2 = true                             // 尝试使用 HTTP/2

	// 保存基础 Transport（无代理配置）
	baseTransport := transport.Clone()

	// 如果设置了全局代理且未启用代理池，则配置全局代理
	if cfg.HTTPProxy != "" && !cfg.ProxyPoolEnabled {
		proxyURL, err := url.Parse(cfg.HTTPProxy)
		if err == nil {
			if proxyURL.Scheme == "socks5" {
				// SOCKS5 代理
				dialer, err := proxy.FromURL(proxyURL, proxy.Direct)
				if err == nil {
					transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
						return dialer.Dial(network, addr)
					}
					logger.Info("已配置 SOCKS5 代理: %s", cfg.HTTPProxy)
				} else {
					logger.Error("SOCKS5 代理配置失败: %v", err)
				}
			} else {
				// HTTP/HTTPS 代理
				transport.Proxy = http.ProxyURL(proxyURL)
				logger.Info("已配置 HTTP/HTTPS 代理: %s", cfg.HTTPProxy)
			}
		} else {
			logger.Error("代理 URL 解析失败: %v", err)
		}
	}

	logger.Info("HTTP 连接池已优化 - MaxIdleConns: %d, MaxIdleConnsPerHost: %d, IdleConnTimeout: %v",
		DefaultMaxIdleConns, DefaultMaxIdleConnsPerHost, DefaultIdleConnTimeout)

	return &Client{
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   300 * time.Second,
		},
		cfg:           cfg,
		baseTransport: baseTransport,
	}
}

// SendChatRequest 发送聊天请求到 Amazon Q（带重试逻辑）
// payload 可以是 map[string]interface{} 或实现了自定义 MarshalJSON 的结构体
// machineId: 设备标识，用于构建 User-Agent
// accountID: 账号ID，用于代理池 Session 派生
// logTimestamp 用于日志文件配对（可为空）
// @author ygw
func (c *Client) SendChatRequest(ctx context.Context, accessToken, machineId, accountID string, payload interface{}, logTimestamp string) (*http.Response, error) {
	// 获取账号专用的 HTTP 客户端（支持代理池）
	httpClient := c.GetHTTPClientForAccount(accountID)
	// 序列化请求一次
	reqBody, err := json.Marshal(payload)
	if err != nil {
		logger.Error("序列化请求失败: %v", err)
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	logger.Debug("Q 请求体大小: %d 字节", len(reqBody))

	// 保存请求体到 log 目录（仅调试模式）
	saveRequestLog(reqBody, logTimestamp)

	// 打印格式化的请求体（用于调试）
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, reqBody, "", "  "); err == nil {
		logger.Debug("Q 请求体: %s", prettyJSON.String())
	} else {
		logger.Debug("Q 请求体（未格式化）: %s", string(reqBody))
	}

	var lastErr error
	for attempt := 1; attempt <= MaxRetries; attempt++ {
		// 为每次尝试创建新请求（body 需要可重读）
		req, err := http.NewRequestWithContext(ctx, "POST", AmazonQEndpoint, bytes.NewReader(reqBody))
		if err != nil {
			logger.Error("创建 HTTP 请求失败: %v", err)
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		// 设置请求头（使用 Kiro IDE User-Agent 格式）
		invocationID := uuid.New().String()
		userAgent := auth.BuildAmazonQUserAgent(machineId)
		req.Header.Set("Content-Type", "application/x-amz-json-1.0")
		req.Header.Set("X-Amz-Target", "AmazonCodeWhispererStreamingService.GenerateAssistantResponse")
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("X-Amz-User-Agent", userAgent)
		req.Header.Set("X-Amzn-Codewhisperer-Optout", "false")
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Amz-Sdk-Request", fmt.Sprintf("attempt=%d; max=%d", attempt, MaxRetries))
		req.Header.Set("Amz-Sdk-Invocation-Id", invocationID)

		logger.Debug("发送 Q 请求 - 尝试: %d/%d, 调用ID: %s", attempt, MaxRetries, invocationID)
		startTime := time.Now()

		// 发送请求（使用账号专用客户端，支持代理池）
		resp, err := httpClient.Do(req)
		duration := time.Since(startTime)

		if err != nil {
			lastErr = err
			logger.Error("Q HTTP 请求失败 - 尝试: %d/%d, 耗时: %v, 错误: %v, URL: %s, 调用ID: %s",
				attempt, MaxRetries, duration, err, AmazonQEndpoint, invocationID)

			// 检查是否应该重试
			if attempt < MaxRetries && isRetriableError(err) {
				logger.Info("检测到可重试错误，等待 %v 后重试...", RetryDelay*time.Duration(attempt))
				time.Sleep(RetryDelay * time.Duration(attempt)) // 指数退避
				continue
			}

			return nil, fmt.Errorf("failed to send request after %d attempts: %w", attempt, err)
		}

		logger.Debug("Q 响应 - 状态码: %d, 耗时: %v", resp.StatusCode, duration)

		// 检查状态码
		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			bodyStr := string(body)
			logger.Error("Q 返回错误 - 状态码: %d, 响应体: %s", resp.StatusCode, bodyStr)

			// 检查是否为不可重试的错误
			if nrErr := checkNonRetriableError(bodyStr); nrErr != nil {
				logger.Warn("检测到不可重试错误: %s - %s (提示: %s)", nrErr.Code, nrErr.Message, nrErr.Hint)
				return nil, nrErr
			}

			// 5xx 错误时重试
			if resp.StatusCode >= 500 && attempt < MaxRetries {
				logger.Info("检测到服务器错误 (5xx)，等待 %v 后重试...", RetryDelay*time.Duration(attempt))
				time.Sleep(RetryDelay * time.Duration(attempt))
				continue
			}

			return nil, fmt.Errorf("upstream error %d: %s", resp.StatusCode, bodyStr)
		}

		logger.Debug("Q 请求成功")
		return resp, nil
	}

	return nil, fmt.Errorf("failed after %d attempts, last error: %w", MaxRetries, lastErr)
}

// checkNonRetriableError 检查响应体是否包含不可重试的错误
func checkNonRetriableError(bodyStr string) *NonRetriableError {
	for _, item := range nonRetriableErrors {
		if strings.Contains(bodyStr, item.Pattern) {
			return &NonRetriableError{
				Code:         item.Error.Code,
				Message:      item.Error.Message,
				Hint:         item.Error.Hint,
				IsRequestErr: item.Error.IsRequestErr,
			}
		}
	}
	return nil
}

// isRetriableError 判断错误是否可重试
func isRetriableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	// EOF、连接重置和临时网络错误时重试
	return errStr == "EOF" ||
		errStr == "unexpected EOF" ||
		errStr == "connection reset by peer" ||
		errStr == "broken pipe"
}

// saveRequestLog 保存请求体到 logs/awsq_logs/时间_out.log（仅调试模式）
// @author ygw - 添加超时控制，避免 goroutine 泄漏
func saveRequestLog(reqBody []byte, timestamp string) {
	if !logger.IsDebugEnabled() {
		return
	}

	data := make([]byte, len(reqBody))
	copy(data, reqBody)

	go func() {
		// 设置 30 秒超时，避免文件系统问题导致 goroutine 堆积
		done := make(chan struct{})

		go func() {
			defer close(done)

			logDir := filepath.Join("logs", "awsq_logs")
			if err := os.MkdirAll(logDir, 0755); err != nil {
				logger.Error("创建日志目录失败: %v", err)
				return
			}

			filename := fmt.Sprintf("%s_out.log", timestamp)
			filePath := filepath.Join(logDir, filename)

			var prettyJSON bytes.Buffer
			if err := json.Indent(&prettyJSON, data, "", "  "); err != nil {
				prettyJSON.Write(data)
			}

			if err := os.WriteFile(filePath, prettyJSON.Bytes(), 0644); err != nil {
				logger.Error("保存out日志失败: %v", err)
				return
			}

			logger.Debug("请求体已保存到: %s", filePath)
		}()

		select {
		case <-done:
			// 正常完成
		case <-time.After(30 * time.Second):
			logger.Warn("保存请求日志超时（30秒），跳过: %s", timestamp)
		}
	}()
}

// StreamEventGenerator 返回原始事件的通道
// @author ygw - 高并发优化：增大 channel 缓冲区，使用 sync.Pool 复用缓冲区
func StreamEventGenerator(ctx context.Context, resp *http.Response) (<-chan EventInfo, <-chan error) {
	eventChan := make(chan EventInfo, 50) // 从 10 增大到 50，减少高速事件流阻塞
	errChan := make(chan error, 1)

	go func() {
		defer close(eventChan)
		defer close(errChan)
		defer resp.Body.Close()

		logger.Info("开始解析 Q 事件流")
		eventCount := 0

		parser := NewEventStreamParser()

		// 使用 sync.Pool 复用缓冲区，减少内存分配
		bufPtr := streamBufferPool.Get().(*[]byte)
		buf := *bufPtr
		defer streamBufferPool.Put(bufPtr)

		for {
			select {
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			default:
			}

			n, err := resp.Body.Read(buf)
			if n > 0 {
				events, parseErr := parser.Feed(buf[:n])
				if parseErr != nil {
					logger.Error("解析事件流失败: %v", parseErr)
					errChan <- parseErr
					return
				}

				for _, event := range events {
					eventType := ExtractEventType(event.Headers)
					payload, _ := ParsePayload(event.Payload)
					eventCount++

					// 调试模式：打印事件详情
					payloadJSON, _ := json.Marshal(payload)
					logger.Debug("[AmazonQ 事件] #%d 类型: %s, Payload: %s", eventCount, eventType, string(payloadJSON))

					select {
					case eventChan <- EventInfo{
						EventType: eventType,
						Payload:   payload,
					}:
					case <-ctx.Done():
						logger.Warn("事件流处理被取消 - 已处理 %d 个事件", eventCount)
						errChan <- ctx.Err()
						return
					}
				}
			}

			if err == io.EOF {
				logger.Info("Q 事件流结束 - 共处理 %d 个事件", eventCount)
				return
			}
			if err != nil {
				logger.Error("读取响应体失败 - 已处理 %d 个事件, 错误: %v", eventCount, err)
				errChan <- err
				return
			}
		}
	}()

	return eventChan, errChan
}

// EventInfo 表示解析后的事件信息
type EventInfo struct {
	EventType string
	Payload   map[string]interface{}
}

// GetUsageLimits 查询用户配额限制
// machineId: 设备标识，用于构建 User-Agent
// @author ygw
func (c *Client) GetUsageLimits(ctx context.Context, accessToken, machineId, resourceType string) (map[string]interface{}, error) {
	startTime := time.Now()
	
	if resourceType == "" {
		resourceType = "AGENTIC_REQUEST"
	}

	url := fmt.Sprintf("%sgetUsageLimits?isEmailRequired=true&origin=AI_EDITOR&resourceType=%s", AmazonQEndpoint, resourceType)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	userAgent := auth.BuildKiroUserAgent(machineId)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Amz-Sdk-Invocation-Id", uuid.New().String())

	logger.Debug("查询配额 - URL: %s", url)

	requestStartTime := time.Now()
	resp, err := c.httpClient.Do(req)
	requestElapsed := time.Since(requestStartTime)
	
	if err != nil {
		logger.Error("配额查询 HTTP 请求失败 - URL: %s, 耗时: %.0fms, 错误: %v", url, requestElapsed.Seconds()*1000, err)
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode >= 400 {
		// 解析错误响应
		var errResp struct {
			Message string `json:"message"`
			Reason  string `json:"reason"`
		}
		_ = json.Unmarshal(body, &errResp)

		// 根据响应内容返回对应的错误码
		var apiErr *APIError
		bodyStr := string(body)

		totalElapsed := time.Since(startTime)
		// 先打印原始响应，方便调试
		logger.Error("配额查询失败 - 状态码: %d, 耗时: %.0fms, 原始响应: %s", resp.StatusCode, totalElapsed.Seconds()*1000, bodyStr)

		// 优先检查 reason 字段
		switch errResp.Reason {
		case "TEMPORARILY_SUSPENDED":
			apiErr = NewAPIErrorWithDetail(ErrCodeSuspended, "账号已被封控", errResp.Message)
		case "INVALID_TOKEN":
			apiErr = NewAPIErrorWithDetail(ErrCodeTokenInvalid, "Token 无效", errResp.Message)
		default:
			// 再检查响应内容中的关键字（不区分大小写）
			bodyLower := strings.ToLower(bodyStr)
			if strings.Contains(bodyLower, "suspended") {
				apiErr = NewAPIErrorWithDetail(ErrCodeSuspended, "账号已被封控", bodyStr)
			} else if strings.Contains(bodyLower, "invalid") || strings.Contains(bodyLower, "expired") {
				apiErr = NewAPIErrorWithDetail(ErrCodeTokenInvalid, "Token 无效", bodyStr)
			} else if resp.StatusCode == 401 {
				apiErr = NewAPIErrorWithDetail(ErrCodeUnauthorized, "未授权", bodyStr)
			} else if resp.StatusCode == 403 {
				apiErr = NewAPIErrorWithDetail(ErrCodeForbidden, "禁止访问", bodyStr)
			} else {
				apiErr = NewAPIErrorWithDetail(ErrCodeQuotaFailed, "配额查询失败", bodyStr)
			}
		}
		logger.Error("配额查询解析结果 - 错误码: %s, 友好提示: %s", apiErr.Code, apiErr.Message)
		return nil, apiErr
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	totalElapsed := time.Since(startTime)
	logger.Debug("配额查询成功 - 耗时: %.0fms", totalElapsed.Seconds()*1000)
	return result, nil
}

// SetProxyPool 设置代理池
// @param pool 代理池实例
// @author ygw
func (c *Client) SetProxyPool(pool *proxypool.ProxyPool) {
	c.proxyPool = pool
	if pool != nil {
		logger.Info("代理池已设置 - 代理数量: %d, 启用数量: %d", pool.Count(), pool.EnabledCount())
	}
}

// GetHTTPClientForAccount 获取账号专用的 HTTP 客户端
// 如果启用了代理池，会根据账号 ID 派生代理地址
// @param accountID 账号ID，用于 Session 派生
// @return *http.Client 配置了代理的 HTTP 客户端
// @author ygw
func (c *Client) GetHTTPClientForAccount(accountID string) *http.Client {
	// 如果未启用代理池或代理池为空，返回默认客户端
	if !c.cfg.ProxyPoolEnabled || c.proxyPool == nil {
		return c.httpClient
	}

	// 从代理池获取代理地址（已经过 Session 派生）
	proxyURL := c.proxyPool.GetProxy(accountID)
	if proxyURL == "" {
		// 代理池为空，回退到全局代理或无代理
		return c.httpClient
	}

	// 创建带代理的 Transport
	transport := c.baseTransport.Clone()
	parsedURL, err := url.Parse(proxyURL)
	if err != nil {
		logger.Error("代理 URL 解析失败: %v, 使用默认客户端", err)
		return c.httpClient
	}

	if parsedURL.Scheme == "socks5" {
		// SOCKS5 代理
		dialer, err := proxy.FromURL(parsedURL, proxy.Direct)
		if err != nil {
			logger.Error("SOCKS5 代理配置失败: %v, 使用默认客户端", err)
			return c.httpClient
		}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}
	} else {
		// HTTP/HTTPS 代理
		transport.Proxy = http.ProxyURL(parsedURL)
	}

	logger.Debug("账号 %s 使用代理: %s", accountID, proxyURL)

	return &http.Client{
		Transport: transport,
		Timeout:   300 * time.Second,
	}
}
