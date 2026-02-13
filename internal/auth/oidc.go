package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"claude-api/internal/config"
	"claude-api/internal/logger"
	"time"

	"github.com/google/uuid"
)

const (
	OIDCBase      = "https://oidc.us-east-1.amazonaws.com"
	RegisterURL   = OIDCBase + "/client/register"
	DeviceAuthURL = OIDCBase + "/device_authorization"
	TokenURL      = OIDCBase + "/token"
	StartURL      = "https://view.awsapps.com/start"
	AmzSDKRequest = "attempt=1; max=3"
)

// OIDCClient 处理 OIDC 操作
type OIDCClient struct {
	httpClient *http.Client
	cfg        *config.Config
}

// NewOIDCClient 创建新的 OIDC 客户端
func NewOIDCClient(cfg *config.Config) *OIDCClient {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	if cfg.HTTPProxy != "" {
		proxyURL, err := url.Parse(cfg.HTTPProxy)
		if err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	return &OIDCClient{
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   60 * time.Second,
		},
		cfg: cfg,
	}
}

// makeHeaders 构建请求头（使用动态 User-Agent）
// @author ygw
func (c *OIDCClient) makeHeaders(machineId string) map[string]string {
	userAgent := BuildKiroUserAgent(machineId)
	return map[string]string{
		"Content-Type":          "application/json",
		"User-Agent":            userAgent,
		"Amz-Sdk-Request":       AmzSDKRequest,
		"Amz-Sdk-Invocation-Id": uuid.New().String(),
	}
}

// postJSON 发送 POST 请求（支持动态 machineId）
// @author ygw
func (c *OIDCClient) postJSON(ctx context.Context, url string, payload interface{}, machineId string) (map[string]interface{}, error) {
	logger.Debug("OIDC: 发送 POST 请求 - URL: %s", url)

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		logger.Error("OIDC: 序列化请求体失败: %v", err)
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		logger.Error("OIDC: 创建 HTTP 请求失败: %v", err)
		return nil, err
	}

	headers := c.makeHeaders(machineId)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	startTime := time.Now()
	resp, err := c.httpClient.Do(req)
	duration := time.Since(startTime)

	if err != nil {
		logger.Error("OIDC: HTTP 请求失败 - 耗时: %v, 错误: %v", duration, err)
		return nil, err
	}
	defer resp.Body.Close()

	logger.Debug("OIDC: 收到响应 - 状态码: %d, 耗时: %v", resp.StatusCode, duration)

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)
		// authorization_pending 是正常的等待状态,不记录为错误
		if containsIgnoreCase(bodyStr, "authorization_pending") {
			logger.Debug("OIDC: 授权待处理 - 状态码: %d", resp.StatusCode)
		} else {
			logger.Error("OIDC: 请求失败 - 状态码: %d, 响应: %s", resp.StatusCode, bodyStr)
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, bodyStr)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		logger.Error("OIDC: 解析响应 JSON 失败: %v", err)
		return nil, err
	}

	logger.Debug("OIDC: 请求成功完成")
	return result, nil
}

// RegisterClient 注册新的 OIDC 客户端
// machineId: 设备标识，用于构建 User-Agent
// @author ygw
func (c *OIDCClient) RegisterClient(ctx context.Context, machineId string) (string, string, error) {
	logger.Info("OIDC: 开始注册客户端")

	payload := map[string]interface{}{
		"clientName": "Kiro IDE",
		"clientType": "public",
		"scopes": []string{
			"codewhisperer:completions",
			"codewhisperer:analysis",
			"codewhisperer:conversations",
			"codewhisperer:taskassist",
			"codewhisperer:transformations",
		},
	}

	result, err := c.postJSON(ctx, RegisterURL, payload, machineId)
	if err != nil {
		logger.Error("OIDC: 注册客户端失败: %v", err)
		return "", "", err
	}

	clientID, _ := result["clientId"].(string)
	clientSecret, _ := result["clientSecret"].(string)

	if clientID == "" || clientSecret == "" {
		logger.Error("OIDC: 注册响应无效 - 缺少 clientId 或 clientSecret")
		return "", "", fmt.Errorf("invalid response: missing clientId or clientSecret")
	}

	logger.Info("OIDC: 客户端注册成功 - ClientID: %s", clientID)
	return clientID, clientSecret, nil
}

// DeviceAuthorize 启动设备授权流程
// machineId: 设备标识，用于构建 User-Agent
// @author ygw
func (c *OIDCClient) DeviceAuthorize(ctx context.Context, clientID, clientSecret, machineId string) (map[string]interface{}, error) {
	logger.Info("OIDC: 开始设备授权流程 - ClientID: %s", clientID)

	payload := map[string]interface{}{
		"clientId":     clientID,
		"clientSecret": clientSecret,
		"startUrl":     StartURL,
	}

	result, err := c.postJSON(ctx, DeviceAuthURL, payload, machineId)
	if err != nil {
		logger.Error("OIDC: 设备授权失败: %v", err)
		return nil, err
	}

	deviceCode, _ := result["deviceCode"].(string)
	userCode, _ := result["userCode"].(string)
	logger.Info("OIDC: 设备授权成功 - UserCode: %s, DeviceCode: %s", userCode, deviceCode[:8]+"...")

	return result, nil
}

// PollToken 轮询设备代码令牌
// machineId: 设备标识，用于构建 User-Agent
// @author ygw
func (c *OIDCClient) PollToken(ctx context.Context, clientID, clientSecret, deviceCode, machineId string, interval, expiresIn int) (map[string]interface{}, error) {
	logger.Info("OIDC: 开始轮询令牌 - 间隔: %ds, 超时: %ds", interval, expiresIn)

	payload := map[string]interface{}{
		"clientId":     clientID,
		"clientSecret": clientSecret,
		"deviceCode":   deviceCode,
		"grantType":    "urn:ietf:params:oauth:grant-type:device_code",
	}

	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	pollInterval := time.Duration(interval) * time.Second
	// 设置最小轮询间隔为5秒,避免过于频繁的请求
	if pollInterval < 5*time.Second {
		pollInterval = 5 * time.Second
	}

	pollCount := 0
	maxPolls := 120 // 最多轮询120次(10分钟 ÷ 5秒),避免无限轮询

	for time.Now().Before(deadline) && pollCount < maxPolls {
		select {
		case <-ctx.Done():
			logger.Warn("OIDC: 令牌轮询被取消 - 已轮询 %d 次", pollCount)
			return nil, ctx.Err()
		default:
		}

		pollCount++
		logger.Debug("OIDC: 令牌轮询尝试 #%d/%d", pollCount, maxPolls)

		result, err := c.postJSON(ctx, TokenURL, payload, machineId)
		if err == nil {
			logger.Info("OIDC: 令牌轮询成功 - 共轮询 %d 次", pollCount)
			return result, nil
		}

		// 检查是否是 authorization_pending
		if errStr := fmt.Sprint(err); containsIgnoreCase(errStr, "authorization_pending") {
			logger.Debug("OIDC: 等待用户授权 - 将在 %v 后重试 (尝试 #%d)", pollInterval, pollCount)
			time.Sleep(pollInterval)
			continue
		}

		logger.Error("OIDC: 令牌轮询失败: %v", err)
		return nil, err
	}

	if pollCount >= maxPolls {
		logger.Error("OIDC: 令牌轮询达到最大次数 - 已轮询 %d 次", pollCount)
		return nil, fmt.Errorf("device authorization timeout: exceeded maximum poll attempts")
	}

	logger.Error("OIDC: 令牌轮询超时 - 已轮询 %d 次,耗时: %v", pollCount, time.Since(time.Now().Add(-time.Duration(expiresIn)*time.Second)))
	return nil, fmt.Errorf("device authorization timeout")
}

// RefreshAccessToken 刷新访问令牌
// machineId: 设备标识，必须使用登录时的 machineId
// @author ygw
func (c *OIDCClient) RefreshAccessToken(ctx context.Context, clientID, clientSecret, refreshToken, machineId string) (string, string, error) {
	logger.Debug("OIDC: 开始刷新访问令牌 - ClientID: %s", clientID)

	payload := map[string]interface{}{
		"grantType":    "refresh_token",
		"clientId":     clientID,
		"clientSecret": clientSecret,
		"refreshToken": refreshToken,
	}

	result, err := c.postJSON(ctx, TokenURL, payload, machineId)
	if err != nil {
		logger.Error("OIDC: 刷新访问令牌失败: %v", err)
		return "", "", err
	}

	accessToken, _ := result["accessToken"].(string)
	newRefreshToken, _ := result["refreshToken"].(string)

	if accessToken == "" {
		logger.Error("OIDC: 刷新响应无效 - 缺少 accessToken")
		return "", "", fmt.Errorf("no accessToken in response")
	}

	if newRefreshToken == "" {
		logger.Warn("OIDC: 响应中未包含新的刷新令牌,使用旧令牌")
		newRefreshToken = refreshToken // 如果未提供则使用旧刷新令牌
	}

	logger.Debug("OIDC: 访问令牌刷新成功 - AccessToken长度: %d", len(accessToken))
	return accessToken, newRefreshToken, nil
}

func containsIgnoreCase(s, substr string) bool {
	s = toLower(s)
	substr = toLower(substr)
	return contains(s, substr)
}

func toLower(s string) string {
	// 简单的 ASCII 小写转换
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && indexOfSubstring(s, substr) >= 0)
}

func indexOfSubstring(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
