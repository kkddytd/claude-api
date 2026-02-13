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
	// KiroAuthEndpoint 社交登录 Token 刷新端点
	KiroAuthEndpoint = "https://prod.us-east-1.auth.desktop.kiro.dev"
	// AmazonQUsageLimitsEndpoint 配额查询端点
	AmazonQUsageLimitsEndpoint = "https://q.us-east-1.amazonaws.com/getUsageLimits"
)

// KiroClient 处理 Kiro/社交登录相关操作
type KiroClient struct {
	httpClient *http.Client
	cfg        *config.Config
}

// NewKiroClient 创建新的 Kiro 客户端
func NewKiroClient(cfg *config.Config) *KiroClient {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	if cfg.HTTPProxy != "" {
		proxyURL, err := url.Parse(cfg.HTTPProxy)
		if err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	return &KiroClient{
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   60 * time.Second,
		},
		cfg: cfg,
	}
}

// SocialTokenRefreshResult 社交登录 Token 刷新结果
type SocialTokenRefreshResult struct {
	Success      bool   `json:"success"`
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int    `json:"expiresIn"`
	Error        string `json:"error,omitempty"`
}

// RefreshSocialToken 刷新社交登录的 Token（GitHub/Google）
// machineId: 设备标识，用于构建 User-Agent
// 这种类型的账号只需要 refreshToken，不需要 clientId 和 clientSecret
// @author ygw
func (c *KiroClient) RefreshSocialToken(ctx context.Context, refreshToken, machineId string) (*SocialTokenRefreshResult, error) {
	logger.Info("Kiro: 开始刷新社交登录 Token")

	url := KiroAuthEndpoint + "/refreshToken"

	payload := map[string]string{
		"refreshToken": refreshToken,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", BuildKiroUserAgent(machineId))

	startTime := time.Now()
	resp, err := c.httpClient.Do(req)
	duration := time.Since(startTime)

	if err != nil {
		logger.Error("Kiro: Token 刷新请求失败 - 耗时: %v, 错误: %v", duration, err)
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	logger.Debug("Kiro: Token 刷新响应 - 状态码: %d, 耗时: %v", resp.StatusCode, duration)

	if resp.StatusCode >= 400 {
		logger.Error("Kiro: Token 刷新失败 - 状态码: %d, 响应: %s", resp.StatusCode, string(body))
		return &SocialTokenRefreshResult{
			Success: false,
			Error:   fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(body)),
		}, nil
	}

	var result struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresIn    int    `json:"expiresIn"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	logger.Info("Kiro: Token 刷新成功 - 有效期: %d 秒", result.ExpiresIn)

	return &SocialTokenRefreshResult{
		Success:      true,
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresIn:    result.ExpiresIn,
	}, nil
}

// UserInfo 用户信息
type UserInfo struct {
	Email            string  `json:"email"`
	UserID           string  `json:"userId"`
	SubscriptionType string  `json:"subscriptionType"`
	UsageCurrent     float64 `json:"usageCurrent"`
	UsageLimit       float64 `json:"usageLimit"`
	DaysRemaining    int     `json:"daysRemaining"`
	NextResetDate    string  `json:"nextResetDate"`
}

// GetUserInfo 获取用户信息（使用 Amazon Q 的配额 API）
// machineId: 设备标识，用于构建 User-Agent
// @author ygw
func (c *KiroClient) GetUserInfo(ctx context.Context, accessToken, machineId string) (*UserInfo, error) {
	logger.Info("Kiro: 开始获取用户信息")

	url := AmazonQUsageLimitsEndpoint + "?isEmailRequired=true&origin=AI_EDITOR&resourceType=AGENTIC_REQUEST"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	userAgent := BuildKiroUserAgent(machineId)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Amz-Sdk-Invocation-Id", uuid.New().String())

	startTime := time.Now()
	resp, err := c.httpClient.Do(req)
	duration := time.Since(startTime)

	if err != nil {
		logger.Error("Kiro: 获取用户信息请求失败 - 耗时: %v, 错误: %v", duration, err)
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	logger.Debug("Kiro: 用户信息响应 - 状态码: %d, 耗时: %v", resp.StatusCode, duration)

	if resp.StatusCode >= 400 {
		logger.Error("Kiro: 获取用户信息失败 - 状态码: %d, 响应: %s", resp.StatusCode, string(body))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	// 解析 Amazon Q 配额 API 响应
	// 注意: NextDateReset 可能是数字（时间戳）或字符串，使用 interface{} 处理
	var result struct {
		UserInfo struct {
			Email  string `json:"email"`
			UserID string `json:"userId"`
		} `json:"userInfo"`
		UsageBreakdownList []struct {
			ResourceType              string  `json:"resourceType"`
			CurrentUsage              float64 `json:"currentUsage"`
			UsageLimit                float64 `json:"usageLimit"`
			UsageLimitWithPrecision   float64 `json:"usageLimitWithPrecision"`
			CurrentUsageWithPrecision float64 `json:"currentUsageWithPrecision"`
		} `json:"usageBreakdownList"`
		SubscriptionInfo struct {
			SubscriptionTitle string `json:"subscriptionTitle"`
			Type              string `json:"type"`
		} `json:"subscriptionInfo"`
		NextDateReset interface{} `json:"nextDateReset"` // 可能是数字时间戳或字符串
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	// 提取使用量信息
	var usageCurrent, usageLimit float64
	for _, usage := range result.UsageBreakdownList {
		if usage.ResourceType == "CREDIT" || usage.ResourceType == "AGENTIC_REQUEST" {
			usageCurrent = usage.CurrentUsageWithPrecision
			usageLimit = usage.UsageLimitWithPrecision
			if usageLimit == 0 {
				usageLimit = usage.UsageLimit
			}
			if usageCurrent == 0 {
				usageCurrent = usage.CurrentUsage
			}
			break
		}
	}

	// 解析订阅类型
	subscriptionType := "Free"
	title := result.SubscriptionInfo.SubscriptionTitle
	if title != "" {
		switch {
		case kiroContains(title, "PRO"):
			subscriptionType = "Pro"
		case kiroContains(title, "ENTERPRISE"):
			subscriptionType = "Enterprise"
		case kiroContains(title, "TEAMS"):
			subscriptionType = "Teams"
		}
	}

	// 处理 NextDateReset（可能是数字时间戳或字符串）
	var nextResetDate string
	var daysRemaining int
	if result.NextDateReset != nil {
		switch v := result.NextDateReset.(type) {
		case float64:
			// 数字时间戳（毫秒）
			resetTime := time.UnixMilli(int64(v))
			nextResetDate = resetTime.Format("2006-01-02")
			daysRemaining = int(time.Until(resetTime).Hours() / 24)
			if daysRemaining < 0 {
				daysRemaining = 0
			}
		case string:
			// 字符串格式
			nextResetDate = v
			daysRemaining = 30 // 默认值
		default:
			daysRemaining = 30
		}
	}

	userInfo := &UserInfo{
		Email:            result.UserInfo.Email,
		UserID:           result.UserInfo.UserID,
		SubscriptionType: subscriptionType,
		UsageCurrent:     usageCurrent,
		UsageLimit:       usageLimit,
		DaysRemaining:    daysRemaining,
		NextResetDate:    nextResetDate,
	}

	logger.Info("Kiro: 获取用户信息成功 - Email: %s, UserID: %s", userInfo.Email, userInfo.UserID)
	return userInfo, nil
}

// VerifyTokenResult 验证 Token 结果
type VerifyTokenResult struct {
	Success      bool      `json:"success"`
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken"`
	ExpiresIn    int       `json:"expiresIn"`
	UserInfo     *UserInfo `json:"userInfo,omitempty"`
	Error        string    `json:"error,omitempty"`
}

// VerifyAndGetUserInfo 验证 refreshToken 并获取用户信息
// machineId: 设备标识，用于构建 User-Agent
// 这是导入功能的核心方法，通过 refreshToken 获取完整的账号信息
// @author ygw
func (c *KiroClient) VerifyAndGetUserInfo(ctx context.Context, refreshToken, machineId string) (*VerifyTokenResult, error) {
	logger.Info("Kiro: 开始验证 Token 并获取用户信息")

	// Step 1: 刷新 Token 获取 accessToken
	refreshResult, err := c.RefreshSocialToken(ctx, refreshToken, machineId)
	if err != nil {
		return nil, fmt.Errorf("刷新 Token 失败: %w", err)
	}

	if !refreshResult.Success {
		return &VerifyTokenResult{
			Success: false,
			Error:   refreshResult.Error,
		}, nil
	}

	// Step 2: 使用 accessToken 获取用户信息
	userInfo, err := c.GetUserInfo(ctx, refreshResult.AccessToken, machineId)
	if err != nil {
		return &VerifyTokenResult{
			Success:      true,
			AccessToken:  refreshResult.AccessToken,
			RefreshToken: refreshResult.RefreshToken,
			ExpiresIn:    refreshResult.ExpiresIn,
			Error:        fmt.Sprintf("获取用户信息失败: %v", err),
		}, nil
	}

	return &VerifyTokenResult{
		Success:      true,
		AccessToken:  refreshResult.AccessToken,
		RefreshToken: refreshResult.RefreshToken,
		ExpiresIn:    refreshResult.ExpiresIn,
		UserInfo:     userInfo,
	}, nil
}

// kiroContains 简单的字符串包含检查（忽略大小写）
func kiroContains(s, substr string) bool {
	return len(s) >= len(substr) && containsIgnoreCase(s, substr)
}
