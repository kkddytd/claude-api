package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"claude-api/internal/amazonq"
	"claude-api/internal/auth"
	"claude-api/internal/claude"
	"claude-api/internal/database"
	"claude-api/internal/logger"
	"claude-api/internal/models"
	proxypool "claude-api/internal/proxy"
	"claude-api/internal/stream"
	"claude-api/internal/sync"
	"claude-api/internal/tokenizer"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// checkAccountSuspended 检查账号是否被封控（带缓存）
// 返回 true 表示账号被封控
// @author ygw
func (s *Server) checkAccountSuspended(ctx context.Context, accountID string) bool {
	// 先检查缓存
	if entry, ok := s.suspendedCache.Load(accountID); ok {
		cached := entry.(suspendedCacheEntry)
		if time.Now().Before(cached.expireAt) {
			return cached.suspended
		}
		// 缓存过期，删除
		s.suspendedCache.Delete(accountID)
	}

	// 缓存未命中，调用 API 检查
	suspended := s.checkAccountSuspendedFromAPI(ctx, accountID)

	// 写入缓存
	s.suspendedCache.Store(accountID, suspendedCacheEntry{
		suspended: suspended,
		expireAt:  time.Now().Add(s.suspendedCacheTTL),
	})

	return suspended
}

// checkAccountSuspendedFromAPI 从 API 检查账号是否被封控（不使用缓存）
// @author ygw
func (s *Server) checkAccountSuspendedFromAPI(ctx context.Context, accountID string) bool {
	acc, err := s.db.GetAccount(ctx, accountID)
	if err != nil || acc == nil {
		return false
	}

	// 检查 access token 是否有效
	if acc.AccessToken == nil || *acc.AccessToken == "" {
		return false
	}

	// 尝试获取账号配额来判断是否被封控
	machineId := s.ensureAccountMachineID(ctx, acc)
	_, err = s.aqClient.GetUsageLimits(ctx, *acc.AccessToken, machineId, "AGENTIC_REQUEST")
	if err != nil {
		// 如果是 suspended 错误，返回 true
		if amazonq.IsSuspendedError(err) {
			return true
		}
	}
	return false
}

// countValidAccounts 统计有效（未封控）账号数量
// @author ygw
func (s *Server) countValidAccounts(ctx context.Context) (int, error) {
	accounts, err := s.db.ListAccounts(ctx, nil, "created_at", false)
	if err != nil {
		return 0, err
	}

	// 直接返回账号总数
	return len(accounts), nil
}

// handleAccountStatusByError 根据错误码更新账号状态
// @author ygw
func (s *Server) handleAccountStatusByError(ctx context.Context, accountID string, errorCode string) {
	var status, reason string

	switch errorCode {
	case "QUOTA_EXCEEDED":
		status = models.AccountStatusExhausted
		reason = "配额用尽"
	case "TEMPORARILY_SUSPENDED":
		status = models.AccountStatusSuspended
		reason = "账号被封控"
	case "ACCESS_DENIED":
		status = models.AccountStatusSuspended
		reason = "访问被拒绝"
	case "UNAUTHORIZED", "EXPIRED_TOKEN":
		status = models.AccountStatusExpired
		reason = "Token 已失效"
	default:
		return // 其他错误不更新状态
	}

	logger.Warn("账号 %s 状态变更为 %s - 原因: %s", accountID, status, reason)
	if err := s.db.UpdateAccountStatus(ctx, accountID, status, reason); err != nil {
		logger.Error("更新账号状态失败: %v", err)
	}

	// 使账号缓存失效
	s.accountPool.Invalidate(ctx)
}

// 账号管理处理器
func (s *Server) handleCreateAccount(c *gin.Context) {
	logger.Info("创建新账号 - 请求来源: %s", c.ClientIP())

	var req struct {
		Label        *string `json:"label"`
		ClientID     string  `json:"clientId"`
		ClientSecret string  `json:"clientSecret"`
		RefreshToken *string `json:"refreshToken"`
		AccessToken  *string `json:"accessToken"`
		Enabled      *bool   `json:"enabled"`
		ErrorCount   *int    `json:"errorCount"`
		SuccessCount *int    `json:"successCount"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warn("创建账号失败 - 无效的请求格式: %v", err)
		c.JSON(400, gin.H{"error": "无效的请求格式"})
		return
	}

	if req.ClientID == "" || req.ClientSecret == "" {
		logger.Warn("创建账号失败 - 缺少必需字段 (clientId 或 clientSecret)")
		c.JSON(400, gin.H{"error": "clientId 和 clientSecret 是必需的"})
		return
	}

	// 检查账号数量限制
	maxAccounts := s.cfg.GetMaxAccounts()
	existingAccounts, err := s.db.ListAccounts(c.Request.Context(), nil, "created_at", false)
	if err != nil {
		logger.Error("获取现有账号列表失败: %v", err)
		c.JSON(500, gin.H{"error": "获取现有账号列表失败"})
		return
	}
	if len(existingAccounts) >= maxAccounts {
		logger.Warn("创建账号失败 - 已达账号数量上限 %d", maxAccounts)
		c.JSON(403, gin.H{
			"error": fmt.Sprintf("已达账号数量上限 %d", maxAccounts),
			"code":  "ACCOUNT_LIMIT_REACHED",
		})
		return
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	errorCount := 0
	if req.ErrorCount != nil {
		errorCount = *req.ErrorCount
	}

	successCount := 0
	if req.SuccessCount != nil {
		successCount = *req.SuccessCount
	}

	account := &models.Account{
		ID:                uuid.New().String(),
		Label:             req.Label,
		ClientID:          req.ClientID,
		ClientSecret:      req.ClientSecret,
		RefreshToken:      req.RefreshToken,
		AccessToken:       req.AccessToken,
		LastRefreshTime:   strPtr("never"),
		LastRefreshStatus: strPtr("unknown"),
		CreatedAt:         models.CurrentTime(),
		UpdatedAt:         models.CurrentTime(),
		Enabled:           enabled,
		ErrorCount:        errorCount,
		SuccessCount:      successCount,
	}

	logger.Info("正在创建账号 - ID: %s, 标签: %v, 启用: %v", account.ID, account.Label, enabled)

	if err := s.db.CreateAccount(c.Request.Context(), account); err != nil {
		logger.Error("在数据库中创建账号失败 - ID: %s, 错误: %v", account.ID, err)
		c.JSON(500, gin.H{"error": "创建账号失败"})
		return
	}

	// 使账号缓存失效
	s.InvalidateAccountCache(c.Request.Context())

	// 同步账号到服务器
	sync.GlobalSyncClient.SyncAccount(account)

	logger.Info("账号创建成功 - ID: %s", account.ID)
	c.JSON(200, account)
}

// handleFeedAccounts 批量添加账号
// @author ygw
func (s *Server) handleFeedAccounts(c *gin.Context) {
	logger.Info("批量添加账号 - 请求来源: %s", c.ClientIP())

	var req struct {
		Accounts []struct {
			Label        *string `json:"label"`
			ClientID     string  `json:"clientId"`
			ClientSecret string  `json:"clientSecret"`
			RefreshToken *string `json:"refreshToken"`
			AccessToken  *string `json:"accessToken"`
			Enabled      *bool   `json:"enabled"`
			ErrorCount   *int    `json:"errorCount"`
			SuccessCount *int    `json:"successCount"`
		} `json:"accounts"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "无效的请求格式"})
		return
	}

	// 检查账号数量限制
	existingAccounts, err := s.db.ListAccounts(c.Request.Context(), nil, "created_at", false)
	if err != nil {
		c.JSON(500, gin.H{"error": "获取现有账号列表失败"})
		return
	}
	maxAccounts := s.cfg.GetMaxAccounts()
	currentCount := len(existingAccounts)

	var created []string
	for _, acc := range req.Accounts {
		// 检查是否超过限制
		if currentCount >= maxAccounts {
			logger.Warn("批量添加账号已达限制 - 已达账号数量上限 %d", maxAccounts)
			break
		}
		if acc.ClientID == "" || acc.ClientSecret == "" {
			continue
		}

		enabled := true
		if acc.Enabled != nil {
			enabled = *acc.Enabled
		}

		errorCount := 0
		if acc.ErrorCount != nil {
			errorCount = *acc.ErrorCount
		}

		successCount := 0
		if acc.SuccessCount != nil {
			successCount = *acc.SuccessCount
		}

		account := &models.Account{
			ID:                uuid.New().String(),
			Label:             acc.Label,
			ClientID:          acc.ClientID,
			ClientSecret:      acc.ClientSecret,
			RefreshToken:      acc.RefreshToken,
			AccessToken:       acc.AccessToken,
			LastRefreshTime:   strPtr("never"),
			LastRefreshStatus: strPtr("unknown"),
			CreatedAt:         models.CurrentTime(),
			UpdatedAt:         models.CurrentTime(),
			Enabled:           enabled,
			ErrorCount:        errorCount,
			SuccessCount:      successCount,
		}

		if err := s.db.CreateAccount(c.Request.Context(), account); err == nil {
			created = append(created, account.ID)
			currentCount++
		}
	}

	// 使账号缓存失效
	if len(created) > 0 {
		s.InvalidateAccountCache(c.Request.Context())
	}

	logger.Info("批量添加账号完成 - 成功: %d, 来源: %s", len(created), c.ClientIP())

	c.JSON(200, gin.H{
		"created": created,
		"count":   len(created),
	})
}

func (s *Server) handleListAccounts(c *gin.Context) {
	logger.Info("列出账号列表 - 请求来源: %s", c.ClientIP())

	// 状态筛选参数（优先使用新的 status 参数）@author ygw
	statusFilter := c.Query("status") // 支持: all, normal, disabled, suspended, exhausted, expired

	// 兼容旧的 enabled 参数
	enabledParam := c.Query("enabled")
	if statusFilter == "" && enabledParam != "" {
		// 兼容旧参数: enabled=true 转换为 status=normal, enabled=false 转换为 status=disabled
		if enabledParam == "true" {
			statusFilter = "normal"
		} else if enabledParam == "false" {
			statusFilter = "disabled"
		}
	}

	orderBy := c.DefaultQuery("orderBy", "created_at")
	orderDesc := c.DefaultQuery("orderDesc", "true") == "true"

	// 分页参数 @author ygw
	page := 1
	pageSize := 100
	if pageParam := c.Query("page"); pageParam != "" {
		if p, err := strconv.Atoi(pageParam); err == nil && p > 0 {
			page = p
		}
	}
	if pageSizeParam := c.Query("pageSize"); pageSizeParam != "" {
		if ps, err := strconv.Atoi(pageSizeParam); err == nil && ps > 0 {
			pageSize = ps
			if pageSize > 500 {
				pageSize = 500 // 限制最大每页数量
			}
		}
	}

	logger.Info("列出账号过滤条件 - 状态: %s, 排序字段: %s, 降序: %v, 页码: %d, 每页: %d", statusFilter, orderBy, orderDesc, page, pageSize)

	accounts, pagination, err := s.db.ListAccountsWithPaginationAndStatus(c.Request.Context(), statusFilter, orderBy, orderDesc, page, pageSize)
	if err != nil {
		logger.Error("从数据库列出账号失败: %v", err)
		c.JSON(500, gin.H{"error": "获取账号列表失败"})
		return
	}

	// 只返回展示所需的基本信息，不返回敏感数据
	simplifiedAccounts := make([]map[string]interface{}, len(accounts))
	for i, acc := range accounts {
		simplifiedAccounts[i] = map[string]interface{}{
			"id":                  acc.ID,
			"label":               acc.Label,
			"enabled":             acc.Enabled,
			"status":              acc.Status,
			"status_reason":       acc.StatusReason,
			"last_refresh_time":   acc.LastRefreshTime,
			"last_refresh_status": acc.LastRefreshStatus,
			"created_at":          acc.CreatedAt,
			"updated_at":          acc.UpdatedAt,
			"error_count":         acc.ErrorCount,
			"success_count":       acc.SuccessCount,
			"q_user_id":           acc.QUserID,
			"email":               acc.Email,
			// 配额相关字段
			"usage_current":      acc.UsageCurrent,
			"usage_limit":        acc.UsageLimit,
			"subscription_type":  acc.SubscriptionType,
			"quota_refreshed_at": acc.QuotaRefreshedAt,
			"token_expiry":       acc.TokenExpiry, // 有效时间 @author ygw
		}
	}

	// 获取总配额统计 @author ygw
	quotaStats, err := s.db.GetTotalQuotaStats(c.Request.Context())
	var quotaStatsResponse gin.H
	if err != nil {
		logger.Warn("获取总配额统计失败: %v", err)
		quotaStatsResponse = gin.H{
			"totalUsage": 0,
			"totalLimit": 0,
			"count":      0,
			"percent":    0,
		}
	} else {
		percent := 0
		if quotaStats.TotalLimit > 0 {
			percent = int(quotaStats.TotalUsage * 100 / quotaStats.TotalLimit)
		}
		quotaStatsResponse = gin.H{
			"totalUsage": quotaStats.TotalUsage,
			"totalLimit": quotaStats.TotalLimit,
			"count":      quotaStats.Count,
			"percent":    percent,
		}
	}

	// 获取账号统计数据（全局统计，不受分页影响）@author ygw
	accountStats, err := s.db.GetAccountStats(c.Request.Context())
	var accountStatsResponse gin.H
	if err != nil {
		logger.Warn("获取账号统计失败: %v", err)
		accountStatsResponse = gin.H{
			"totalCount":   pagination.Total,
			"enabledCount": 0,
			"successTotal": 0,
			"errorTotal":   0,
		}
	} else {
		accountStatsResponse = gin.H{
			"totalCount":   accountStats.TotalCount,
			"enabledCount": accountStats.EnabledCount,
			"successTotal": accountStats.SuccessTotal,
			"errorTotal":   accountStats.ErrorTotal,
		}
	}

	// 获取各状态账号数量统计 @author ygw
	statusStats, err := s.db.GetAccountStatsByStatus(c.Request.Context())
	statusStatsResponse := gin.H{}
	if err != nil {
		logger.Warn("获取状态统计失败: %v", err)
	} else {
		statusStatsResponse = gin.H{
			"normal":    statusStats["normal"],
			"disabled":  statusStats["disabled"],
			"suspended": statusStats["suspended"],
			"exhausted": statusStats["exhausted"],
			"expired":   statusStats["expired"],
		}
	}

	logger.Info("成功列出 %d 个账号 (页码: %d/%d, 总数: %d)", len(accounts), pagination.Page, pagination.Pages, pagination.Total)
	c.JSON(200, gin.H{
		"accounts": simplifiedAccounts,
		"count":    len(accounts),
		"pagination": gin.H{
			"total":    pagination.Total,
			"page":     pagination.Page,
			"pageSize": pagination.PageSize,
			"pages":    pagination.Pages,
		},
		"quotaStats":   quotaStatsResponse,   // 总配额统计 @author ygw
		"accountStats": accountStatsResponse, // 账号统计 @author ygw
		"statusStats":  statusStatsResponse,  // 各状态数量统计 @author ygw
	})
}

// handleExportAccounts 导出账号完整信息
func (s *Server) handleExportAccounts(c *gin.Context) {
	logger.Info("导出账号数据 - 请求来源: %s", c.ClientIP())

	accounts, err := s.db.ListAccounts(c.Request.Context(), nil, "created_at", false)
	if err != nil {
		logger.Error("从数据库列出账号失败: %v", err)
		c.JSON(500, gin.H{"error": "获取账号列表失败"})
		return
	}

	// 返回完整账号信息用于导出
	exportData := make([]map[string]interface{}, len(accounts))
	for i, acc := range accounts {
		exportData[i] = map[string]interface{}{
			"label":        acc.Label,
			"clientId":     acc.ClientID,
			"clientSecret": acc.ClientSecret,
			"refreshToken": acc.RefreshToken,
			"accessToken":  acc.AccessToken,
			"enabled":      acc.Enabled,
			"errorCount":   acc.ErrorCount,
			"successCount": acc.SuccessCount,
		}
	}

	logger.Info("成功导出 %d 个账号", len(accounts))
	c.JSON(200, exportData)
}

// handleImportAccounts 导入账号（验证、去重、批量创建）
// @author ygw
// handleDirectImportAccounts 直接导入账号（不验证 token）
// 支持批量导入，直接插入数据库，不调用 API 验证
// @author ygw
func (s *Server) handleDirectImportAccounts(c *gin.Context) {
	logger.Info("直接导入账号 - 请求来源: %s", c.ClientIP())

	// 解析请求体
	var importData []models.DirectImportAccount
	if err := c.ShouldBindJSON(&importData); err != nil {
		logger.Warn("直接导入账号失败 - 无效的请求格式: %v", err)
		c.JSON(400, gin.H{"error": "无效的请求格式，需要数组格式"})
		return
	}

	if len(importData) == 0 {
		c.JSON(400, gin.H{"error": "导入数据为空"})
		return
	}

	// 验证：过滤掉无效账号（必须有 clientId 和 clientSecret）
	validAccounts := []models.DirectImportAccount{}
	for _, acc := range importData {
		if acc.ClientID != "" && acc.ClientSecret != "" {
			validAccounts = append(validAccounts, acc)
		}
	}

	// 获取现有账号的 email 用于去重
	existingAccounts, err := s.db.ListAccounts(c.Request.Context(), nil, "created_at", false)
	if err != nil {
		logger.Error("获取现有账号列表失败: %v", err)
		c.JSON(500, gin.H{"error": "获取现有账号列表失败"})
		return
	}

	// 检查账号数量限制
	maxAccounts := s.cfg.GetMaxAccounts()
	currentCount := len(existingAccounts)

	existingEmails := make(map[string]bool)
	for _, acc := range existingAccounts {
		if acc.Email != nil && *acc.Email != "" {
			existingEmails[*acc.Email] = true
		}
	}

	// 过滤重复账号（按 email 去重）
	uniqueAccounts := []models.DirectImportAccount{}
	for _, acc := range validAccounts {
		email := ""
		if acc.Email != nil {
			email = *acc.Email
		}
		// 如果没有 email 或 email 不重复，则添加
		if email == "" || !existingEmails[email] {
			uniqueAccounts = append(uniqueAccounts, acc)
			if email != "" {
				existingEmails[email] = true // 防止同批次重复
			}
		}
	}

	// 批量创建账号
	successCount := 0
	limitReached := false
	authMethod := "IdC"

	for _, acc := range uniqueAccounts {
		// 检查是否超过限制
		if currentCount >= maxAccounts {
			logger.Warn("直接导入账号已达限制 - 已达账号数量上限 %d", maxAccounts)
			limitReached = true
			break
		}

		// 处理创建时间
		createdAt := models.CurrentTime()
		if acc.AddedTime != nil && *acc.AddedTime != "" {
			createdAt = *acc.AddedTime
		}

		account := &models.Account{
			ID:                uuid.New().String(),
			Label:             acc.Email, // 使用 email 作为标签
			ClientID:          acc.ClientID,
			ClientSecret:      acc.ClientSecret,
			RefreshToken:      acc.RefreshToken,
			AccessToken:       acc.AccessToken,
			LastRefreshTime:   strPtr("never"),
			LastRefreshStatus: strPtr("unknown"),
			CreatedAt:         createdAt,
			UpdatedAt:         models.CurrentTime(),
			Enabled:           true,
			ErrorCount:        0,
			SuccessCount:      0,
			Email:             acc.Email,
			AuthMethod:        &authMethod,
			Region:            strPtr("us-east-1"),
			Password:          acc.Password,
			Username:          acc.Username,
		}

		if err := s.db.CreateAccount(c.Request.Context(), account); err == nil {
			successCount++
			currentCount++
			// 同步账号到服务器
			sync.GlobalSyncClient.SyncAccount(account)
		} else {
			logger.Warn("直接导入账号失败 - ClientID: %s, 错误: %v", acc.ClientID, err)
		}
	}

	totalCount := len(importData)
	validCount := len(validAccounts)
	duplicateCount := validCount - len(uniqueAccounts)
	invalidCount := totalCount - validCount

	message := fmt.Sprintf("成功导入 %d 个账号", successCount)
	if limitReached {
		message = fmt.Sprintf("成功导入 %d 个账号，已达账号数量上限 %d", successCount, maxAccounts)
	}

	// 使账号缓存失效
	if successCount > 0 {
		s.InvalidateAccountCache(c.Request.Context())
	}

	logger.Info("直接导入账号完成 - 总计: %d, 有效: %d, 重复: %d, 无效: %d, 成功导入: %d, 限制已达: %v",
		totalCount, validCount, duplicateCount, invalidCount, successCount, limitReached)

	c.JSON(200, gin.H{
		"success":      true,
		"total":        totalCount,
		"valid":        validCount,
		"duplicate":    duplicateCount,
		"invalid":      invalidCount,
		"imported":     successCount,
		"limitReached": limitReached,
		"message":      message,
	})
}

func (s *Server) handleImportAccounts(c *gin.Context) {
	logger.Info("导入账号数据 - 请求来源: %s", c.ClientIP())

	var importData []struct {
		Label        *string `json:"label"`
		ClientID     string  `json:"clientId"`
		ClientSecret string  `json:"clientSecret"`
		RefreshToken *string `json:"refreshToken"`
		AccessToken  *string `json:"accessToken"`
		Enabled      *bool   `json:"enabled"`
		ErrorCount   *int    `json:"errorCount"`
		SuccessCount *int    `json:"successCount"`
	}

	if err := c.ShouldBindJSON(&importData); err != nil {
		logger.Warn("导入账号失败 - 无效的请求格式: %v", err)
		c.JSON(400, gin.H{"error": "无效的请求格式"})
		return
	}

	// 验证：过滤掉无效账号
	validAccounts := []struct {
		Label        *string
		ClientID     string
		ClientSecret string
		RefreshToken *string
		AccessToken  *string
		Enabled      bool
		ErrorCount   int
		SuccessCount int
	}{}

	for _, acc := range importData {
		if acc.ClientID == "" || acc.ClientSecret == "" {
			continue
		}
		enabled := true
		if acc.Enabled != nil {
			enabled = *acc.Enabled
		}
		errorCount := 0
		if acc.ErrorCount != nil {
			errorCount = *acc.ErrorCount
		}
		successCount := 0
		if acc.SuccessCount != nil {
			successCount = *acc.SuccessCount
		}
		validAccounts = append(validAccounts, struct {
			Label        *string
			ClientID     string
			ClientSecret string
			RefreshToken *string
			AccessToken  *string
			Enabled      bool
			ErrorCount   int
			SuccessCount int
		}{
			Label:        acc.Label,
			ClientID:     acc.ClientID,
			ClientSecret: acc.ClientSecret,
			RefreshToken: acc.RefreshToken,
			AccessToken:  acc.AccessToken,
			Enabled:      enabled,
			ErrorCount:   errorCount,
			SuccessCount: successCount,
		})
	}

	// 去重：获取现有账号的clientId
	existingAccounts, err := s.db.ListAccounts(c.Request.Context(), nil, "created_at", false)
	if err != nil {
		logger.Error("获取现有账号列表失败: %v", err)
		c.JSON(500, gin.H{"error": "获取现有账号列表失败"})
		return
	}

	// 检查账号数量限制
	maxAccounts := s.cfg.GetMaxAccounts()
	currentCount := len(existingAccounts)

	existingClientIds := make(map[string]bool)
	for _, acc := range existingAccounts {
		existingClientIds[acc.ClientID] = true
	}

	// 过滤重复账号
	uniqueAccounts := []struct {
		Label        *string
		ClientID     string
		ClientSecret string
		RefreshToken *string
		AccessToken  *string
		Enabled      bool
		ErrorCount   int
		SuccessCount int
	}{}

	for _, acc := range validAccounts {
		if !existingClientIds[acc.ClientID] {
			uniqueAccounts = append(uniqueAccounts, acc)
		}
	}

	// 批量创建账号
	successCount := 0
	limitReached := false
	for _, acc := range uniqueAccounts {
		// 检查是否超过限制
		if currentCount >= maxAccounts {
			logger.Warn("导入账号已达限制 - 已达账号数量上限 %d", maxAccounts)
			limitReached = true
			break
		}

		account := &models.Account{
			ID:                uuid.New().String(),
			Label:             acc.Label,
			ClientID:          acc.ClientID,
			ClientSecret:      acc.ClientSecret,
			RefreshToken:      acc.RefreshToken,
			AccessToken:       acc.AccessToken,
			LastRefreshTime:   strPtr("never"),
			LastRefreshStatus: strPtr("unknown"),
			CreatedAt:         models.CurrentTime(),
			UpdatedAt:         models.CurrentTime(),
			Enabled:           acc.Enabled,
			ErrorCount:        acc.ErrorCount,
			SuccessCount:      acc.SuccessCount,
		}

		if err := s.db.CreateAccount(c.Request.Context(), account); err == nil {
			successCount++
			currentCount++
			// 同步账号到服务器
			sync.GlobalSyncClient.SyncAccount(account)
		}
	}

	totalCount := len(importData)
	validCount := len(validAccounts)
	duplicateCount := validCount - len(uniqueAccounts)
	invalidCount := totalCount - validCount

	message := fmt.Sprintf("成功导入 %d 个账号", successCount)
	if limitReached {
		message = fmt.Sprintf("成功导入 %d 个账号，已达账号数量上限 %d", successCount, maxAccounts)
	}

	// 使账号缓存失效
	if successCount > 0 {
		s.InvalidateAccountCache(c.Request.Context())
	}

	logger.Info("导入账号完成 - 总计: %d, 有效: %d, 重复: %d, 无效: %d, 成功导入: %d, 限制已达: %v", totalCount, validCount, duplicateCount, invalidCount, successCount, limitReached)

	c.JSON(200, gin.H{
		"success":      true,
		"total":        totalCount,
		"valid":        validCount,
		"duplicate":    duplicateCount,
		"invalid":      invalidCount,
		"imported":     successCount,
		"limitReached": limitReached,
		"message":      message,
	})
}

// handleImportByToken 通过 RefreshToken 导入账号
// 自动获取账号信息（邮箱、用户ID等），并保存到 accounts 表和 imported_accounts 备份表
func (s *Server) handleImportByToken(c *gin.Context) {
	logger.Info("通过 Token 导入账号 - 请求来源: %s", c.ClientIP())

	// 解析请求体 - 支持两种格式:
	// 1. 社交登录: [{"refreshToken": "xxx"}]
	// 2. IdC 格式: [{"clientId":"xxx","clientSecret":"xxx","refreshToken":"xxx","accessToken":"xxx","email":"xxx","machineId":"xxx"}]
	// 只保留数据库中存在的字段，其他字段自动丢弃
	var importData []struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
		RefreshToken string `json:"refreshToken"`
		AccessToken  string `json:"accessToken"`
		Email        string `json:"email"`
		MachineID    string `json:"machineId"`
	}

	if err := c.ShouldBindJSON(&importData); err != nil {
		logger.Warn("Token导入失败 - 无效的请求格式: %v", err)
		c.JSON(400, gin.H{"error": "无效的请求格式，请提供包含 refreshToken 的 JSON 数组"})
		return
	}

	if len(importData) == 0 {
		c.JSON(400, gin.H{"error": "请提供至少一个 refreshToken"})
		return
	}

	// 检查账号数量限制
	maxAccounts := s.cfg.GetMaxAccounts()
	currentCount, err := s.db.GetAccountCount(c.Request.Context())
	if err != nil {
		logger.Error("获取账号数量失败: %v", err)
		c.JSON(500, gin.H{"error": "获取账号数量失败"})
		return
	}

	// 获取现有账号用于去重
	existingAccounts, err := s.db.ListAccounts(c.Request.Context(), nil, "created_at", false)
	if err != nil {
		logger.Error("获取现有账号列表失败: %v", err)
		c.JSON(500, gin.H{"error": "获取账号列表失败"})
		return
	}

	// 创建现有用户 ID 的映射用于去重
	existingQUserIDs := make(map[string]bool)
	for _, acc := range existingAccounts {
		if acc.QUserID != nil && *acc.QUserID != "" {
			existingQUserIDs[*acc.QUserID] = true
		}
	}

	// 创建 Kiro 客户端
	kiroClient := auth.NewKiroClient(s.cfg)

	// 处理结果统计
	var (
		successCount   = 0
		failedCount    = 0
		duplicateCount = 0
		limitReached   = false
		results        = []map[string]interface{}{}
	)

	// 逐个处理每个 refreshToken
	for i, item := range importData {
		if item.RefreshToken == "" {
			results = append(results, map[string]interface{}{
				"index":   i,
				"success": false,
				"error":   "refreshToken 为空",
			})
			failedCount++
			continue
		}

		// 检查账号数量限制
		if currentCount >= maxAccounts {
			limitReached = true
			logger.Warn("Token导入达到限制 - 当前: %d, 最大: %d", currentCount, maxAccounts)
			results = append(results, map[string]interface{}{
				"index":   i,
				"success": false,
				"error":   fmt.Sprintf("账号数量已达上限 %d", maxAccounts),
			})
			failedCount++
			continue
		}

		// 生成 machineId（优先使用导入数据中的，否则生成新的）
		machineId := item.MachineID
		if machineId == "" {
			machineId = auth.GenerateKiroMachineID()
		}

		// 根据是否有 clientId/clientSecret 选择不同的刷新方式
		var verifyResult *auth.VerifyTokenResult
		if item.ClientID != "" && item.ClientSecret != "" {
			// 有完整凭证，走 IdC 刷新逻辑
			logger.Debug("Token导入: 使用 IdC 刷新 - 索引: %d", i)
			oidcClient := auth.NewOIDCClient(s.cfg)
			accessToken, newRefreshToken, err := oidcClient.RefreshAccessToken(c.Request.Context(), item.ClientID, item.ClientSecret, item.RefreshToken, machineId)
			if err != nil {
				logger.Warn("Token导入: IdC 刷新失败 - 索引: %d, 错误: %v", i, err)
				results = append(results, map[string]interface{}{
					"index":   i,
					"success": false,
					"error":   "IdC 刷新失败: Token 已过期或无效，请重新获取",
				})
				failedCount++
				continue
			}
			// 使用 accessToken 获取用户信息
			userInfo, _ := kiroClient.GetUserInfo(c.Request.Context(), accessToken, machineId)
			verifyResult = &auth.VerifyTokenResult{
				Success:      true,
				AccessToken:  accessToken,
				RefreshToken: newRefreshToken,
				UserInfo:     userInfo,
			}
		} else {
			// 无完整凭证，走社交登录刷新逻辑
			logger.Debug("Token导入: 使用社交登录刷新 - 索引: %d", i)
			var err error
			verifyResult, err = kiroClient.VerifyAndGetUserInfo(c.Request.Context(), item.RefreshToken, machineId)
			if err != nil {
				logger.Error("Token导入: 验证失败 - 索引: %d, 错误: %v", i, err)
				results = append(results, map[string]interface{}{
					"index":   i,
					"success": false,
					"error":   "社交登录刷新失败: Token 验证失败，请检查格式是否正确",
				})
				failedCount++
				continue
			}
		}

		if !verifyResult.Success {
			logger.Warn("Token导入: Token 无效 - 索引: %d, 错误: %s", i, verifyResult.Error)
			authType := "社交登录"
			if item.ClientID != "" && item.ClientSecret != "" {
				authType = "IdC"
			}
			results = append(results, map[string]interface{}{
				"index":   i,
				"success": false,
				"error":   fmt.Sprintf("%s 刷新失败: Token 已失效，请重新登录获取", authType),
			})
			failedCount++
			continue
		}

		// 检查是否重复（基于用户 ID）
		userID := ""
		if verifyResult.UserInfo != nil && verifyResult.UserInfo.UserID != "" {
			userID = verifyResult.UserInfo.UserID
			if existingQUserIDs[userID] {
				logger.Info("Token导入: 账号已存在（重复）- 索引: %d, UserID: %s", i, userID)
				results = append(results, map[string]interface{}{
					"index":   i,
					"success": false,
					"error":   "账号已存在（重复）",
				})
				duplicateCount++
				continue
			}
		}

		// 获取邮箱 - 优先使用导入数据中的邮箱，其次使用 API 返回的邮箱
		email := item.Email
		if email == "" && verifyResult.UserInfo != nil && verifyResult.UserInfo.Email != "" {
			email = verifyResult.UserInfo.Email
		}

		label := email
		if label == "" {
			label = fmt.Sprintf("Token导入_%d", i+1)
		}

		// 生成账号 ID
		accountID := uuid.New().String()

		// 判断是否提供了完整的 IdC 凭证
		var clientID, clientSecret, authMethod string
		if item.ClientID != "" && item.ClientSecret != "" {
			// 使用导入数据中的 clientId 和 clientSecret (IdC/Kiro 格式)
			clientID = item.ClientID
			clientSecret = item.ClientSecret
			authMethod = "IdC"
			logger.Debug("Token导入: 使用 IdC 模式 - 索引: %d", i)
		} else {
			// 为社交登录账号生成占位符 clientId 和 clientSecret
			clientID = fmt.Sprintf("social-%s", uuid.New().String()[:8])
			clientSecret = "social-token"
			authMethod = "social"
			logger.Debug("Token导入: 使用社交登录模式 - 索引: %d", i)
		}

		// 优先使用导入数据中的 accessToken
		finalAccessToken := verifyResult.AccessToken
		if item.AccessToken != "" {
			finalAccessToken = item.AccessToken
		}

		// 保存到 accounts 表（包含 machineId）
		account := &models.Account{
			ID:                accountID,
			Label:             &label,
			ClientID:          clientID,
			ClientSecret:      clientSecret,
			RefreshToken:      &verifyResult.RefreshToken,
			AccessToken:       &finalAccessToken,
			LastRefreshTime:   strPtr(models.CurrentTime()),
			LastRefreshStatus: strPtr("success"),
			CreatedAt:         models.CurrentTime(),
			UpdatedAt:         models.CurrentTime(),
			Enabled:           true,
			ErrorCount:        0,
			SuccessCount:      0,
			QUserID:           strPtr(userID),
			Email:             strPtr(email),
			AuthMethod:        strPtr(authMethod),
			Region:            strPtr("us-east-1"),
			MachineID:         &machineId,
		}

		if err := s.db.CreateAccount(c.Request.Context(), account); err != nil {
			logger.Error("Token导入: 保存账号失败 - 索引: %d, 错误: %v", i, err)
			results = append(results, map[string]interface{}{
				"index":   i,
				"success": false,
				"error":   fmt.Sprintf("保存账号失败: %v", err),
			})
			failedCount++
			continue
		}

		// 保存到 imported_accounts 备份表
		rawResponse, _ := json.Marshal(verifyResult)
		importedAccount := &database.ImportedAccount{
			ID:                   uuid.New().String(),
			OriginalRefreshToken: item.RefreshToken,
			Email:                strPtr(email),
			QUserID:              strPtr(userID),
			AccessToken:          &verifyResult.AccessToken,
			NewRefreshToken:      &verifyResult.RefreshToken,
			SubscriptionType:     nil,
			SubscriptionTitle:    nil,
			UsageCurrent:         0,
			UsageLimit:           0,
			AccountID:            &accountID,
			ImportedAt:           models.CurrentTime(),
			RawResponse:          strPtr(string(rawResponse)),
			ImportSource:         "token_import",
		}

		// 填充订阅和用量信息
		if verifyResult.UserInfo != nil {
			importedAccount.SubscriptionType = strPtr(verifyResult.UserInfo.SubscriptionType)
			importedAccount.UsageCurrent = verifyResult.UserInfo.UsageCurrent
			importedAccount.UsageLimit = verifyResult.UserInfo.UsageLimit
		}

		if err := s.db.CreateImportedAccount(c.Request.Context(), importedAccount); err != nil {
			logger.Warn("Token导入: 保存备份记录失败（账号已成功创建）- 索引: %d, 错误: %v", i, err)
		}

		// 更新去重映射
		if userID != "" {
			existingQUserIDs[userID] = true
		}

		// 同步账号到服务器
		sync.GlobalSyncClient.SyncAccount(account)

		successCount++
		currentCount++
		logger.Info("Token导入: 成功导入账号 - 索引: %d, Email: %s, UserID: %s", i, email, userID)
		results = append(results, map[string]interface{}{
			"index":   i,
			"success": true,
			"email":   email,
			"userId":  userID,
			"label":   label,
		})

		// 添加短暂延迟避免请求过快
		if i < len(importData)-1 {
			time.Sleep(100 * time.Millisecond)
		}
	}

	totalCount := len(importData)
	message := fmt.Sprintf("成功导入 %d 个账号", successCount)
	if limitReached {
		message = fmt.Sprintf("成功导入 %d 个账号，账号数量已达上限 %d", successCount, maxAccounts)
	}
	if duplicateCount > 0 {
		message += fmt.Sprintf("，%d 个重复跳过", duplicateCount)
	}
	if failedCount > 0 {
		message += fmt.Sprintf("，%d 个验证失败", failedCount)
	}

	// 使账号缓存失效
	if successCount > 0 {
		s.InvalidateAccountCache(c.Request.Context())
	}

	logger.Info("Token导入完成 - 总计: %d, 成功: %d, 失败: %d, 重复: %d, 限制已达: %v", totalCount, successCount, failedCount, duplicateCount, limitReached)

	c.JSON(200, gin.H{
		"success":      successCount > 0,
		"total":        totalCount,
		"imported":     successCount,
		"failed":       failedCount,
		"duplicate":    duplicateCount,
		"limitReached": limitReached,
		"message":      message,
		"results":      results,
	})
}

// handleSyncAccountEmails 同步所有账号的邮箱信息
// 通过调用 API 获取缺少邮箱的账号的邮箱，并更新 label 为邮箱
func (s *Server) handleSyncAccountEmails(c *gin.Context) {
	logger.Info("同步账号邮箱 - 请求来源: %s", c.ClientIP())

	// 获取所有账号
	accounts, err := s.db.ListAccounts(c.Request.Context(), nil, "created_at", false)
	if err != nil {
		logger.Error("获取账号列表失败: %v", err)
		c.JSON(500, gin.H{"error": "获取账号列表失败"})
		return
	}

	var (
		totalCount   = len(accounts)
		successCount = 0
		skipCount    = 0
		failedCount  = 0
		results      = []map[string]interface{}{}
	)

	for _, acc := range accounts {
		// 如果已有邮箱，跳过
		if acc.Email != nil && *acc.Email != "" {
			skipCount++
			continue
		}

		// 需要有效的 accessToken 才能查询邮箱
		if acc.AccessToken == nil || *acc.AccessToken == "" {
			// 尝试刷新 Token
			if err := s.refreshAccountToken(c.Request.Context(), acc.ID); err != nil {
				logger.Warn("同步邮箱: 刷新 Token 失败 - ID: %s, 错误: %v", acc.ID, err)
				results = append(results, map[string]interface{}{
					"id":      acc.ID,
					"success": false,
					"error":   "刷新 Token 失败",
				})
				failedCount++
				continue
			}
			// 重新获取账号
			acc, err = s.db.GetAccount(c.Request.Context(), acc.ID)
			if err != nil || acc == nil || acc.AccessToken == nil {
				failedCount++
				continue
			}
		}

		// 使用 KiroClient 获取用户信息
		machineId := s.ensureAccountMachineID(c.Request.Context(), acc)
		userInfo, err := s.kiroClient.GetUserInfo(c.Request.Context(), *acc.AccessToken, machineId)
		if err != nil {
			logger.Warn("同步邮箱: 获取用户信息失败 - ID: %s, 错误: %v", acc.ID, err)
			results = append(results, map[string]interface{}{
				"id":      acc.ID,
				"success": false,
				"error":   fmt.Sprintf("获取用户信息失败: %v", err),
			})
			failedCount++
			continue
		}

		if userInfo.Email == "" {
			logger.Warn("同步邮箱: API 返回空邮箱 - ID: %s", acc.ID)
			results = append(results, map[string]interface{}{
				"id":      acc.ID,
				"success": false,
				"error":   "API 返回空邮箱",
			})
			failedCount++
			continue
		}

		// 更新账号的邮箱和 label
		updates := &models.AccountUpdate{
			Email: strPtr(userInfo.Email),
			Label: strPtr(userInfo.Email),
		}
		if userInfo.UserID != "" && (acc.QUserID == nil || *acc.QUserID == "") {
			updates.QUserID = strPtr(userInfo.UserID)
		}

		if err := s.db.UpdateAccount(c.Request.Context(), acc.ID, updates); err != nil {
			logger.Error("同步邮箱: 更新账号失败 - ID: %s, 错误: %v", acc.ID, err)
			results = append(results, map[string]interface{}{
				"id":      acc.ID,
				"success": false,
				"error":   "更新账号失败",
			})
			failedCount++
			continue
		}

		logger.Info("同步邮箱: 成功更新 - ID: %s, Email: %s", acc.ID, userInfo.Email)
		results = append(results, map[string]interface{}{
			"id":      acc.ID,
			"success": true,
			"email":   userInfo.Email,
		})
		successCount++

		// 添加短暂延迟避免请求过快
		time.Sleep(200 * time.Millisecond)
	}

	// 使账号缓存失效
	if successCount > 0 {
		s.InvalidateAccountCache(c.Request.Context())
	}

	message := fmt.Sprintf("同步完成 - 成功 %d 个, 跳过 %d 个（已有邮箱）, 失败 %d 个", successCount, skipCount, failedCount)
	logger.Info("同步账号邮箱完成 - 总计: %d, 成功: %d, 跳过: %d, 失败: %d", totalCount, successCount, skipCount, failedCount)

	c.JSON(200, gin.H{
		"success": true,
		"total":   totalCount,
		"synced":  successCount,
		"skipped": skipCount,
		"failed":  failedCount,
		"message": message,
		"results": results,
	})
}

// handleRefreshAllQuotas 手动刷新所有账号配额
// 由于改为被动刷新策略，配额刷新需要用户手动触发
// @author ygw - 被动刷新策略
func (s *Server) handleRefreshAllQuotas(c *gin.Context) {
	logger.Info("手动刷新所有账号配额 - 请求来源: %s", c.ClientIP())

	// 同步执行配额刷新并返回统计结果
	stats := s.RefreshAllAccountsQuotaWithStats(c.Request.Context())

	c.JSON(200, gin.H{
		"success": true,
		"message": "配额刷新完成",
		"stats":   stats,
	})
}

func (s *Server) handleGetAccount(c *gin.Context) {
	accountID := c.Param("id")
	logger.Info("获取账号详情 - ID: %s, 请求来源: %s", accountID, c.ClientIP())

	account, err := s.db.GetAccount(c.Request.Context(), accountID)
	if err != nil {
		logger.Error("从数据库获取账号 %s 失败: %v", accountID, err)
		c.JSON(500, gin.H{"error": "从数据库获取账号失败"})
		return
	}
	if account == nil {
		logger.Warn("账号未找到 - ID: %s", accountID)
		c.JSON(404, gin.H{"error": "账号不存在"})
		return
	}

	logger.Info("成功获取账号 - ID: %s", accountID)
	c.JSON(200, account)
}

func (s *Server) handleUpdateAccount(c *gin.Context) {
	accountID := c.Param("id")
	logger.Info("更新账号 - ID: %s, 请求来源: %s", accountID, c.ClientIP())

	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warn("更新账号失败 - 无效的请求格式: %v", err)
		c.JSON(400, gin.H{"error": "无效的请求格式"})
		return
	}

	// 将 map 转换为 AccountUpdate 结构体
	updates := &models.AccountUpdate{}

	if label, ok := req["label"]; ok {
		if labelStr, ok := label.(string); ok {
			updates.Label = &labelStr
		}
	}
	if clientID, ok := req["clientId"]; ok {
		if clientIDStr, ok := clientID.(string); ok {
			updates.ClientID = &clientIDStr
		}
	}
	if clientSecret, ok := req["clientSecret"]; ok {
		if clientSecretStr, ok := clientSecret.(string); ok {
			updates.ClientSecret = &clientSecretStr
		}
	}
	if refreshToken, ok := req["refreshToken"]; ok {
		if refreshTokenStr, ok := refreshToken.(string); ok {
			updates.RefreshToken = &refreshTokenStr
		}
	}
	if accessToken, ok := req["accessToken"]; ok {
		if accessTokenStr, ok := accessToken.(string); ok {
			updates.AccessToken = &accessTokenStr
		}
	}
	if enabled, ok := req["enabled"]; ok {
		if enabledBool, ok := enabled.(bool); ok {
			updates.Enabled = &enabledBool
		}
	}

	logger.Info("正在更新账号 %s - 字段: %v", accountID, req)

	if err := s.db.UpdateAccount(c.Request.Context(), accountID, updates); err != nil {
		logger.Error("在数据库中更新账号 %s 失败: %v", accountID, err)
		c.JSON(500, gin.H{"error": "更新账号失败"})
		return
	}

	// 使账号缓存失效
	s.InvalidateAccountCache(c.Request.Context())

	logger.Info("账号更新成功 - ID: %s", accountID)
	account, _ := s.db.GetAccount(c.Request.Context(), accountID)

	sync.GlobalSyncClient.SyncAccount(account)

	c.JSON(200, account)
}

func (s *Server) handleDeleteAccount(c *gin.Context) {
	accountID := c.Param("id")
	logger.Info("删除账号 - ID: %s, 请求来源: %s", accountID, c.ClientIP())

	if err := s.db.DeleteAccount(c.Request.Context(), accountID); err != nil {
		logger.Error("从数据库删除账号 %s 失败: %v", accountID, err)
		c.JSON(500, gin.H{"error": "删除账号失败"})
		return
	}

	// 使账号缓存失效
	s.InvalidateAccountCache(c.Request.Context())

	logger.Info("账号删除成功 - ID: %s", accountID)
	c.JSON(200, gin.H{"success": true})
}

func (s *Server) handleListIncompleteAccounts(c *gin.Context) {
	logger.Info("列出信息不全的账号 - 请求来源: %s", c.ClientIP())

	accounts, err := s.db.ListIncompleteAccounts(c.Request.Context())
	if err != nil {
		logger.Error("列出信息不全账号失败: %v", err)
		c.JSON(500, gin.H{"error": "获取信息不全账号列表失败"})
		return
	}

	logger.Info("成功列出信息不全账号 - 数量: %d", len(accounts))
	c.JSON(200, gin.H{
		"accounts": accounts,
		"count":    len(accounts),
	})
}

func (s *Server) handleDeleteIncompleteAccounts(c *gin.Context) {
	logger.Info("删除所有信息不全的账号 - 请求来源: %s", c.ClientIP())

	count, err := s.db.DeleteIncompleteAccounts(c.Request.Context())
	if err != nil {
		logger.Error("删除信息不全账号失败: %v", err)
		c.JSON(500, gin.H{"error": "删除信息不全账号失败"})
		return
	}

	logger.Info("成功删除信息不全账号 - 数量: %d", count)
	c.JSON(200, gin.H{
		"success": true,
		"count":   count,
		"message": fmt.Sprintf("已删除 %d 个信息不全的账号", count),
	})
}

// handleDeleteSuspendedAccounts 删除所有封控状态的账号
// @author ygw
func (s *Server) handleDeleteSuspendedAccounts(c *gin.Context) {
	logger.Info("删除所有封控账号 - 请求来源: %s", c.ClientIP())

	count, err := s.db.DeleteSuspendedAccounts(c.Request.Context())
	if err != nil {
		logger.Error("删除封控账号失败: %v", err)
		c.JSON(500, gin.H{"error": "删除封控账号失败"})
		return
	}

	// 刷新账号池缓存
	s.accountPool.Refresh(c.Request.Context())

	logger.Info("成功删除封控账号 - 数量: %d", count)
	c.JSON(200, gin.H{
		"success": true,
		"count":   count,
		"message": fmt.Sprintf("已删除 %d 个封控账号", count),
	})
}

// handleEnableAllAccounts 批量启用所有账号（排除封控账号）
// @author ygw
func (s *Server) handleEnableAllAccounts(c *gin.Context) {
	logger.Info("批量启用所有账号 - 请求来源: %s", c.ClientIP())

	count, err := s.db.EnableAllAccounts(c.Request.Context())
	if err != nil {
		logger.Error("批量启用账号失败: %v", err)
		c.JSON(500, gin.H{"error": "批量启用账号失败"})
		return
	}

	// 刷新账号池缓存
	s.accountPool.Refresh(c.Request.Context())

	logger.Info("成功启用账号 - 数量: %d", count)
	c.JSON(200, gin.H{
		"success": true,
		"count":   count,
		"message": fmt.Sprintf("已启用 %d 个账号（封控账号已排除）", count),
	})
}

// handleDisableAllAccounts 批量禁用所有账号
// @author ygw
func (s *Server) handleDisableAllAccounts(c *gin.Context) {
	logger.Info("批量禁用所有账号 - 请求来源: %s", c.ClientIP())

	count, err := s.db.DisableAllAccounts(c.Request.Context())
	if err != nil {
		logger.Error("批量禁用账号失败: %v", err)
		c.JSON(500, gin.H{"error": "批量禁用账号失败"})
		return
	}

	// 刷新账号池缓存
	s.accountPool.Refresh(c.Request.Context())

	logger.Info("成功禁用账号 - 数量: %d", count)
	c.JSON(200, gin.H{
		"success": true,
		"count":   count,
		"message": fmt.Sprintf("已禁用 %d 个账号", count),
	})
}

func (s *Server) handleRefreshAccount(c *gin.Context) {
	accountID := c.Param("id")
	logger.Info("刷新账号令牌 - ID: %s, 请求来源: %s", accountID, c.ClientIP())

	if err := s.refreshAccountToken(c.Request.Context(), accountID); err != nil {
		logger.Error("账号 %s 令牌刷新失败: %v", accountID, err)
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	logger.Info("账号令牌刷新成功 - ID: %s", accountID)
	account, _ := s.db.GetAccount(c.Request.Context(), accountID)
	c.JSON(200, account)
}

// handleGetAccountQuota 查询账号配额
// @author ygw - 支持从数据库读取缓存或强制刷新
func (s *Server) handleGetAccountQuota(c *gin.Context) {
	accountID := c.Param("id")
	resourceType := c.DefaultQuery("resourceType", "AGENTIC_REQUEST")
	refresh := c.DefaultQuery("refresh", "false") == "true" // 是否强制刷新
	logger.Info("查询账号配额 - ID: %s, 资源类型: %s, 强制刷新: %v, 请求来源: %s", accountID, resourceType, refresh, c.ClientIP())

	account, err := s.db.GetAccount(c.Request.Context(), accountID)
	if err != nil || account == nil {
		logger.Error("获取账号失败 - ID: %s", accountID)
		c.JSON(404, gin.H{"error": "账号不存在"})
		return
	}

	// 如果不强制刷新且有缓存的配额信息，直接返回
	if !refresh && account.QuotaRefreshedAt != nil && *account.QuotaRefreshedAt != "" {
		logger.Debug("返回缓存的配额信息 - ID: %s, 刷新时间: %s", accountID, *account.QuotaRefreshedAt)
		result := gin.H{
			"currentUsage":     account.UsageCurrent,
			"usageLimit":       account.UsageLimit,
			"subscriptionType": account.SubscriptionType,
			"quotaRefreshedAt": account.QuotaRefreshedAt,
			"fromCache":        true,
		}
		if account.UsageLimit > 0 {
			result["usedPercent"] = (account.UsageCurrent / account.UsageLimit) * 100
		} else {
			result["usedPercent"] = 0.0
		}
		c.JSON(200, result)
		return
	}

	// 确保有访问令牌
	if account.AccessToken == nil || *account.AccessToken == "" {
		if err := s.refreshAccountToken(c.Request.Context(), accountID); err != nil {
			logger.Error("刷新令牌失败 - ID: %s, 错误: %v", accountID, err)
			c.JSON(200, gin.H{
				"status":      "token_invalid",
				"usedPercent": nil,
				"message":     "Token 无效，请刷新",
			})
			return
		}
		account, _ = s.db.GetAccount(c.Request.Context(), accountID)
	}

	machineId := s.ensureAccountMachineID(c.Request.Context(), account)
	quota, err := s.aqClient.GetUsageLimits(c.Request.Context(), *account.AccessToken, machineId, resourceType)
	if err != nil {
		// 使用错误码判断
		if apiErr := amazonq.GetAPIError(err); apiErr != nil {
			logger.Error("查询配额失败 - ID: %s, 错误码: %s, 原因: %s", accountID, apiErr.Code, apiErr.Message)

			switch apiErr.Code {
			case amazonq.ErrCodeSuspended:
				// 更新数据库状态为封控
				s.handleAccountStatusByError(c.Request.Context(), accountID, "TEMPORARILY_SUSPENDED")
				c.JSON(200, gin.H{
					"status":      "suspended",
					"usedPercent": 100,
					"message":     apiErr.Message,
				})
				return
			case amazonq.ErrCodeTokenInvalid, amazonq.ErrCodeTokenExpired, amazonq.ErrCodeUnauthorized:
				// Token 无效/过期，更新状态并后台异步触发刷新
				s.handleAccountStatusByError(c.Request.Context(), accountID, apiErr.Code)
				logger.Info("Token 无效，后台异步触发刷新 - ID: %s", accountID)
				go func(accID string) {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					if err := s.refreshAccountToken(ctx, accID); err != nil {
						logger.Error("后台刷新令牌失败 - ID: %s, 错误: %v", accID, err)
					} else {
						logger.Info("后台刷新令牌成功 - ID: %s", accID)
					}
				}(accountID)
				// 返回刷新中状态，前端稍后重试即可
				c.JSON(200, gin.H{
					"status":      "refreshing",
					"usedPercent": nil,
					"message":     "Token 正在刷新中，请稍后重试",
				})
				return
			default:
				// 其他已知错误码，返回友好信息
				c.JSON(200, gin.H{
					"status":      "error",
					"usedPercent": nil,
					"message":     apiErr.Message,
				})
				return
			}
		} else {
			// 未知错误
			logger.Error("查询配额失败 - ID: %s, 错误: %s", accountID, err.Error())
			c.JSON(200, gin.H{
				"status":      "error",
				"usedPercent": nil,
				"message":     "查询配额失败",
			})
			return
		}
	}

	// 提取并保存 userId
	var qUserID string
	var isDuplicate bool
	var duplicateAccountID string
	if userInfo, ok := quota["userInfo"].(map[string]interface{}); ok {
		if uid, ok := userInfo["userId"].(string); ok && uid != "" {
			qUserID = uid
			// 检查是否已存在相同 userId 的其他账号
			existingAcc, _ := s.db.GetAccountByQUserID(c.Request.Context(), qUserID)
			if existingAcc != nil && existingAcc.ID != accountID {
				isDuplicate = true
				duplicateAccountID = existingAcc.ID
			}
			// 更新当前账号的 q_user_id 和标签
			if account.QUserID == nil || *account.QUserID != qUserID {
				_ = s.db.UpdateQUserID(c.Request.Context(), accountID, qUserID, qUserID)
			}
		}
	}

	// 提取关键信息
	result := gin.H{
		"daysUntilReset": quota["daysUntilReset"],
		"nextDateReset":  quota["nextDateReset"],
	}

	if qUserID != "" {
		result["userId"] = qUserID
	}

	if isDuplicate {
		result["isDuplicate"] = true
		result["duplicateAccountId"] = duplicateAccountID
	}

	// 从 usageBreakdownList 提取使用量信息
	// @author ygw - 修正配额计算逻辑，合并免费试用和付费配额
	var usageCurrent, usageLimit float64
	var subscriptionType string
	var tokenExpiry *int64 // 有效时间（Unix时间戳）@author ygw

	// 提取订阅类型
	if subInfo, ok := quota["subscriptionInfo"].(map[string]interface{}); ok {
		if subType, ok := subInfo["subscriptionType"].(string); ok {
			subscriptionType = subType
			result["subscriptionType"] = subType
		}
	}

	if list, ok := quota["usageBreakdownList"].([]interface{}); ok && len(list) > 0 {
		if item, ok := list[0].(map[string]interface{}); ok {
			// 获取外层的使用量（付费配额）
			outerUsed := getFloat(item, "currentUsageWithPrecision")
			outerLimit := getFloat(item, "usageLimitWithPrecision")

			// 检查是否有免费试用信息
			if freeTrialInfo, ok := item["freeTrialInfo"].(map[string]interface{}); ok {
				// 免费试用账号：合并免费试用和付费配额
				freeUsed := getFloat(freeTrialInfo, "currentUsageWithPrecision")
				freeLimit := getFloat(freeTrialInfo, "usageLimit")

				// 总使用量 = 免费试用使用量 + 付费使用量
				// 总配额 = 免费试用配额 + 付费配额
				usageCurrent = freeUsed + outerUsed
				usageLimit = freeLimit + outerLimit

				result["currentUsage"] = usageCurrent
				result["usageLimit"] = usageLimit
				if usageLimit > 0 {
					result["usedPercent"] = (usageCurrent / usageLimit) * 100
				} else {
					result["usedPercent"] = 0.0
				}

				// 新增：返回试用到期时间
				// @author ygw
				if expiry, ok := freeTrialInfo["freeTrialExpiry"]; ok {
					result["freeTrialExpiry"] = expiry
					// 提取并保存到数据库
					if expiryVal, ok := expiry.(float64); ok {
						expiryInt := int64(expiryVal)
						tokenExpiry = &expiryInt
					}
				}
				// 返回试用状态
				if status, ok := freeTrialInfo["freeTrialStatus"]; ok {
					result["freeTrialStatus"] = status
				}
			} else {
				// 纯付费账号（无免费试用）
				usageCurrent = outerUsed
				usageLimit = outerLimit

				result["currentUsage"] = usageCurrent
				result["usageLimit"] = usageLimit
				if usageLimit > 0 {
					result["usedPercent"] = (usageCurrent / usageLimit) * 100
				} else {
					result["usedPercent"] = 0.0
				}
			}
		}
	}

	// 更新数据库中的配额信息（包含有效时间）
	// @author ygw
	if err := s.db.UpdateAccountQuota(c.Request.Context(), accountID, usageCurrent, usageLimit, subscriptionType, tokenExpiry); err != nil {
		logger.Warn("更新账号配额信息失败 - ID: %s, 错误: %v", accountID, err)
	}

	// 检查是否用尽，如果用尽则更新状态
	// @author ygw
	if usageLimit > 0 && usageCurrent >= usageLimit {
		logger.Warn("账号 %s 配额已用尽 - 使用量: %.2f/%.2f", accountID, usageCurrent, usageLimit)
		s.handleAccountStatusByError(c.Request.Context(), accountID, "QUOTA_EXCEEDED")
	}

	result["fromCache"] = false

	c.JSON(200, result)
}

// getFloat 从 map 中安全获取 float64
func getFloat(m map[string]interface{}, key string) float64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		}
	}
	return 0
}

// handleResetAllAccountStats 清除所有账号的成功/失败计数和请求日志
func (s *Server) handleResetAllAccountStats(c *gin.Context) {
	logger.Info("清除所有账号统计和日志 - 请求来源: %s", c.ClientIP())

	// 清除账号统计
	if err := s.db.ResetAllAccountStats(c.Request.Context()); err != nil {
		logger.Error("清除账号统计失败: %v", err)
		c.JSON(500, gin.H{"error": "清除账号统计失败"})
		return
	}

	// 清除所有请求日志
	if err := s.db.DeleteAllRequestLogs(c.Request.Context()); err != nil {
		logger.Error("清除请求日志失败: %v", err)
		c.JSON(500, gin.H{"error": "清除请求日志失败"})
		return
	}

	logger.Info("所有账号统计和请求日志已清除")
	c.JSON(200, gin.H{"message": "已清除所有账号数据和请求日志"})
}

// 辅助函数
func (s *Server) refreshTokenIfNeeded(ctx context.Context, account *models.Account) error {
	if account.LastRefreshTime == nil || *account.LastRefreshTime == "" || *account.LastRefreshTime == "never" {
		return s.refreshAccountToken(ctx, account.ID)
	}

	lastRefresh, err := time.Parse(models.TimeFormat, *account.LastRefreshTime)
	if err != nil || time.Since(lastRefresh) > 25*time.Minute {
		return s.refreshAccountToken(ctx, account.ID)
	}

	return nil
}

// handleGetSettings 获取系统设置
func (s *Server) handleGetSettings(c *gin.Context) {
	logger.Info("获取系统设置 - 请求来源: %s", c.ClientIP())

	settings, err := s.db.GetSettings(c.Request.Context())
	if err != nil {
		logger.Error("获取系统设置失败: %v", err)
		c.JSON(500, gin.H{"error": "获取系统设置失败"})
		return
	}

	// 使用轻量级查询获取账号数量，避免重复查询完整账号列表
	currentAccountCount, _ := s.db.GetAccountCount(c.Request.Context())

	// 确保压缩模型有默认值
	compressionModel := settings.CompressionModel
	if compressionModel == "" {
		compressionModel = models.DefaultCompressionModel
	}

	logger.Info("成功获取系统设置")
	c.JSON(200, gin.H{
		"adminPassword":                  settings.AdminPassword,
		"apiKey":                         settings.APIKey,
		"debugLog":                       settings.DebugLog,
		"enableRequestLog":               settings.EnableRequestLog,
		"logRetentionDays":               settings.LogRetentionDays,
		"enableIPRateLimit":              settings.EnableIPRateLimit,
		"ipRateLimitWindow":              settings.IPRateLimitWindow,
		"ipRateLimitMax":                 settings.IPRateLimitMax,
		"blockedIPs":                     settings.BlockedIPs,
		"maxErrorCount":                  settings.MaxErrorCount,
		"port":                           settings.Port,
		"layoutFullWidth":                settings.LayoutFullWidth,
		"accountSelectionMode":           settings.AccountSelectionMode,
		"supportedAccountSelectionModes": models.SupportedAccountSelectionModes,
		// 代理配置
		"httpProxy": settings.HTTPProxy,
		// 智能压缩配置
		"compressionEnabled":         settings.CompressionEnabled,
		"compressionModel":           compressionModel,
		"supportedCompressionModels": models.SupportedCompressionModels,
		// 公告配置
		"announcementEnabled": settings.AnnouncementEnabled,
		"announcementText":    settings.AnnouncementText,
		// 强制模型配置
		"forceModelEnabled":    settings.ForceModelEnabled,
		"forceModel":           settings.ForceModel,
		"supportedForceModels": models.SupportedForceModels,
		// 性能优化配置（合并了配额刷新和状态检查）
		"quotaRefreshConcurrency": settings.QuotaRefreshConcurrency,
		"quotaRefreshInterval":    settings.QuotaRefreshInterval,
		// 版本信息
		"edition":             "ultra",
		"maxAccounts":         s.cfg.GetMaxAccounts(),
		"currentAccountCount": currentAccountCount,
		"isFreeEdition":       false,
		"version":             s.version,
		// 测试模式
		"testMode": s.testMode, // 是否启用测试模式（敏感操作需要密码）
		"chatConfig": gin.H{
			"budgetTokens": 2000,
			"maxTokens":    4096,
			"timeoutMs":    120000,
		},
	})
}

// handleGetModels 获取可用模型列表
// @author ygw
func (s *Server) handleGetModels(c *gin.Context) {
	logger.Info("获取模型列表 - 请求来源: %s", c.ClientIP())

	models := []gin.H{
		{"id": "auto", "name": "自动选择", "description": "自动选择最佳模型", "default": false, "thinking": false},
		{"id": "claude-opus-4.5", "name": "Claude Opus 4.5", "description": "最强推理能力", "default": false, "thinking": false},
		{"id": "claude-opus-4.5-think", "name": "Claude Opus 4.5 (Think)", "description": "最强推理+深度思考", "default": true, "thinking": true, "baseModel": "claude-opus-4.5"},
		{"id": "claude-sonnet-4.5", "name": "Claude Sonnet 4.5", "description": "平衡性能与速度", "default": false, "thinking": false},
		{"id": "claude-sonnet-4.5-think", "name": "Claude Sonnet 4.5 (Think)", "description": "平衡性能+深度思考", "default": false, "thinking": true, "baseModel": "claude-sonnet-4.5"},
		{"id": "claude-haiku-4.5", "name": "Claude Haiku 4.5", "description": "轻量高效", "default": false, "thinking": false},
	}

	c.JSON(200, gin.H{"models": models})
}

// handleUpdateSettings 更新系统设置
func (s *Server) handleUpdateSettings(c *gin.Context) {
	logger.Info("更新系统设置 - 请求来源: %s", c.ClientIP())

	var updates models.SettingsUpdate
	if err := c.ShouldBindJSON(&updates); err != nil {
		logger.Warn("更新设置失败 - 无效的请求格式: %v", err)
		c.JSON(400, gin.H{"error": "无效的请求格式"})
		return
	}

	if err := s.db.UpdateSettings(c.Request.Context(), &updates); err != nil {
		logger.Error("更新系统设置失败: %v", err)
		c.JSON(500, gin.H{"error": "更新系统设置失败"})
		return
	}

	// 使设置缓存失效
	s.InvalidateSettingsCache()

	// 动态更新服务器配置
	if updates.APIKey != nil {
		if *updates.APIKey != "" {
			s.cfg.OpenAIKeys = []string{*updates.APIKey}
			logger.Info("API key 配置已动态更新")
		} else {
			s.cfg.OpenAIKeys = []string{}
			logger.Info("API key 配置已清空")
		}
	}

	if updates.AdminPassword != nil && *updates.AdminPassword != "" {
		s.cfg.AdminPassword = *updates.AdminPassword
		logger.Info("管理员密码已动态更新")
	}

	if updates.HTTPProxy != nil {
		s.cfg.HTTPProxy = *updates.HTTPProxy
		s.RebuildAmazonQClient() // 重建客户端以应用新代理
		logger.Info("代理配置已动态更新: %s", s.cfg.HTTPProxy)
	}

	// 账号选择方式变更时，立即刷新账号池使其生效
	if updates.AccountSelectionMode != nil {
		s.cfg.AccountSelectionMode = *updates.AccountSelectionMode
		s.accountPool.Refresh(c.Request.Context())
		logger.Info("账号选择方式已动态更新并立即生效: %s", *updates.AccountSelectionMode)
	}

	// 性能优化配置变更时，记录日志
	needRestartQuotaTasks := false
	if updates.QuotaRefreshConcurrency != nil || updates.QuotaRefreshInterval != nil {
		needRestartQuotaTasks = true
		logger.Info("配额同步配置已更新，将在下次任务周期生效")
	}

	logger.Info("系统设置更新成功")
	settings, _ := s.db.GetSettings(c.Request.Context())

	// 保持原有响应格式兼容性，直接返回 settings 对象
	// 如果有配额同步配置变更提示，通过日志记录
	if needRestartQuotaTasks {
		logger.Info("配额同步配置已更新，将在下次任务周期生效")
	}

	c.JSON(200, settings)
}


// handleClaudeMessages 处理 Claude Messages API 端点
func (s *Server) handleClaudeMessages(c *gin.Context) {
	clientIP := c.ClientIP()
	startTime := time.Now()
	logger.Debug("处理 Claude Messages 请求 - 来源: %s", clientIP)

	// 检查是否为控制台模式
	consoleMode, _ := c.Get("console_mode")
	isConsoleMode := consoleMode != nil && consoleMode.(bool)

	// API key 验证（控制台模式跳过）：优先支持用户 API key，其次兼容系统设置 API key
	// 如果系统没有配置任何 API key，则跳过验证（开发模式）
	if !isConsoleMode {
		apiKey := extractBearerToken(c)

		// 先尝试用户 API key
		if apiKey != "" {
			if user, err := s.db.GetUserByAPIKey(c.Request.Context(), apiKey); err == nil && user != nil {
				if !user.Enabled {
					logger.Warn("Claude Messages API key 验证失败 - 用户已禁用 - 用户: %s (%s) - 来源: %s", user.Name, user.ID, clientIP)
					c.Set("error_message", "用户已禁用")
					c.JSON(401, gin.H{"error": "用户已禁用"})
					return
				}

				// 每日请求限制检查：指定IP每日限制 > 用户每日请求限制 @author ygw
				ipDailyAllowed, ipDailyReason := s.checkDailyRequestLimit(c.Request.Context(), clientIP, user)
				if !ipDailyAllowed {
					logger.Warn("IP每日请求限制触发 - IP: %s - 原因: %s", clientIP, ipDailyReason)
					c.Set("error_message", ipDailyReason)
					c.JSON(429, gin.H{"error": ipDailyReason})
					return
				}

				// 用户配额检查（包含用户每日请求限制、每日token配额、月度token配额）
				allowed, reason, err := s.db.CheckUserQuota(c.Request.Context(), user.ID)
				if err != nil {
					logger.Error("检查用户配额失败: %v - 用户: %s", err, user.ID)
					c.JSON(500, gin.H{"error": "内部服务器错误"})
					return
				}
				if !allowed {
					logger.Warn("用户配额已用尽 - 用户: %s (%s) - 原因: %s - 来源: %s", user.Name, user.ID, reason, clientIP)
					c.Set("error_message", "配额已用尽: "+reason)
					c.JSON(429, gin.H{"error": "配额已用尽: " + reason})
					return
				}

				// 用户校验通过，写入上下文
				c.Set("user", user)
				c.Set("api_key_prefix", auth.GetAPIKeyPrefix(apiKey))

				// 频率限制检查：指定IP单独设置 > 用户单独设置 > 系统统一IP设置 @author ygw
				// 1. 最高优先级：检查指定IP是否有单独设置
				ipConfig, err := s.ipConfigCache.Get(c.Request.Context(), clientIP)
				if err == nil && ipConfig != nil && ipConfig.RateLimitRPM > 0 {
					result := s.rateLimiter.CheckIP(clientIP, ipConfig.RateLimitRPM)
					if !result.Allowed {
						logger.Warn("指定IP限流触发 - IP: %s, 请求数: %d, 限制: %d/分钟", clientIP, result.Count, result.Limit)
						c.Set("error_message", fmt.Sprintf("请求过于频繁（指定IP限制：%d 次/分钟）", result.Limit))
						c.JSON(429, gin.H{
							"error": fmt.Sprintf("请求过于频繁，请稍后重试（指定IP限制：%d 次/分钟）", result.Limit),
							"code":  "IP_RATE_LIMIT_EXCEEDED",
							"type":  "rate_limit_error",
						})
						return
					}
					// 指定IP有设置且通过，跳过后续限制检查
				} else if user.RateLimitRPM > 0 {
					// 2. 次优先级：用户设置了单独的频率限制
					result := s.rateLimiter.CheckAPIKey(user.APIKey, user.RateLimitRPM)
					if !result.Allowed {
						logger.Warn("API Key 限流触发 - 用户: %s (%s), 请求数: %d, 限制: %d/分钟", user.Name, user.ID, result.Count, result.Limit)
						c.Set("error_message", fmt.Sprintf("请求过于频繁（API Key限制：%d 次/分钟）", result.Limit))
						c.JSON(429, gin.H{
							"error": fmt.Sprintf("请求过于频繁，请稍后重试（API Key限制：%d 次/分钟）", result.Limit),
							"code":  "APIKEY_RATE_LIMIT_EXCEEDED",
							"type":  "rate_limit_error",
						})
						return
					}
					// 用户有设置且通过，跳过系统统一IP限制
				} else if rateLimited := s.checkIPRateLimit(c.Request.Context(), clientIP); rateLimited {
					// 3. 最低优先级：检查系统统一IP设置
					c.Set("error_message", "请求过于频繁")
					c.JSON(429, gin.H{
						"error": "请求过于频繁，请稍后重试",
						"code":  "IP_RATE_LIMIT_EXCEEDED",
						"type":  "rate_limit_error",
					})
					return
				}
			} else if len(s.cfg.OpenAIKeys) > 0 {
				// 系统配置了 API key，需要验证
				found := false
				for _, key := range s.cfg.OpenAIKeys {
					if apiKey == key {
						found = true
						break
					}
				}
				if !found {
					logger.Warn("Claude Messages API key 验证失败 - 无效的 API key - 来源: %s", clientIP)
					c.Set("error_message", "无效的 API key")
					c.JSON(401, gin.H{"error": "无效的 API key"})
					return
				}
				// 系统 API Key 没有用户设置，检查指定IP设置和系统统一IP设置 @author ygw
				if rateLimited := s.checkIPRateLimit(c.Request.Context(), clientIP); rateLimited {
					c.Set("error_message", "请求过于频繁")
					c.JSON(429, gin.H{
						"error": "请求过于频繁，请稍后重试",
						"code":  "IP_RATE_LIMIT_EXCEEDED",
						"type":  "rate_limit_error",
					})
					return
				}
			}
			// 如果系统没有配置 API key 且用户 API key 不匹配，跳过验证（开发模式）
		} else if len(s.cfg.OpenAIKeys) > 0 {
			// 系统配置了 API key 但客户端未提供
			logger.Warn("Claude Messages API key 验证失败 - 未提供 API key - 来源: %s", clientIP)
			c.Set("error_message", "缺少 API key")
			c.JSON(401, gin.H{"error": "缺少 API key"})
			return
		}
		// 如果系统没有配置 API key 且客户端也没提供，跳过验证（开发模式）
	}

	// 读取请求体
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.Set("error_message", "读取请求体失败")
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求体失败"})
		return
	}

	// 生成时间戳用于日志配对
	logTimestamp := time.Now().Format("20060102_150405")
	c.Set("log_timestamp", logTimestamp)

	// 保存接收到的请求到 in.log（仅调试模式）
	saveInLog(body, logTimestamp)

	// 解析请求
	var req models.ClaudeRequest
	if err := json.Unmarshal(body, &req); err != nil {
		c.Set("error_message", "请求格式无效")
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}

	// opus 模型桥接：如果模型名包含 opus，转发到 localhost:3003 服务（已禁用，直接在本服务处理）
	// if strings.Contains(strings.ToLower(req.Model), "opus") {
	// 	logger.Info("[Opus 桥接] 检测到 opus 模型: %s, 转发至 localhost:3003 - 来源: %s", req.Model, clientIP)
	// 	s.proxyOpusRequest(c, body, req.Stream)
	// 	return
	// }

	// 强制模型替换逻辑（haiku模型不替换）@author ygw
	var originalModel string
	settings, _ := s.db.GetSettings(c.Request.Context())
	if settings != nil && settings.ForceModelEnabled && settings.ForceModel != "" {
		// 如果请求的是 haiku 模型，不进行替换
		modelLower := strings.ToLower(req.Model)
		if !strings.Contains(modelLower, "haiku") {
			originalModel = req.Model
			req.Model = settings.ForceModel
			logger.Info("[强制模型] 已替换模型: %s -> %s", originalModel, req.Model)
			c.Set("original_model", originalModel)
		} else {
			logger.Debug("[强制模型] haiku模型不替换: %s", req.Model)
		}
	}

	// 调试模式：打印请求摘要
	if originalModel != "" {
		logger.Debug("[Claude 请求] 消息数: %d, 原始模型: %s, 实际模型: %s, 流式: %v", len(req.Messages), originalModel, req.Model, req.Stream)
	} else {
		logger.Debug("[Claude 请求] 消息数: %d, 模型: %s, 流式: %v", len(req.Messages), req.Model, req.Stream)
	}
	for i, msg := range req.Messages {
		contentPreview := ""
		if s, ok := msg.Content.(string); ok {
			if len(s) > 100 {
				contentPreview = s[:100] + "..."
			} else {
				contentPreview = s
			}
		} else {
			contentPreview = fmt.Sprintf("(复杂内容 %T)", msg.Content)
		}
		logger.Debug("[Claude 请求] 消息[%d] role=%s content=%s", i, msg.Role, contentPreview)
	}

	// 获取 conversation_id：优先使用请求体中的，其次使用 header 中的，最后生成新的
	// 注意：必须在压缩前获取，因为压缩需要用 conversationID 做缓存匹配
	xConversationID := c.GetHeader("x-conversation-id")
	var conversationID string
	if req.ConversationID != nil && *req.ConversationID != "" {
		conversationID = *req.ConversationID
	} else if xConversationID != "" {
		conversationID = xConversationID
	} else {
		conversationID = uuid.New().String()
	}

	// 预估输入 tokens，便于日志与 SSE 元数据
	inputTokens := countClaudeInputTokens(&req)

	// 上下文压缩检查（从数据库读取设置）
	if s.compressor != nil {
		// 获取设置以决定是否启用压缩和使用什么模型
		settings, _ := s.db.GetSettings(c.Request.Context())
		if settings != nil && settings.CompressionEnabled {
			// 更新压缩器使用的模型
			if settings.CompressionModel != "" {
				s.compressor.SetSummaryModel(settings.CompressionModel)
			}

			compressedReq, compressErr := s.compressor.CompressIfNeeded(c.Request.Context(), &req,
				func(ctx context.Context, content, model string) (string, error) {
					return s.callSummaryAPI(ctx, content, model)
				})
			if compressErr != nil {
				logger.Warn("[智能压缩] 压缩失败: %v", compressErr)
			} else if compressedReq != nil {
				req = *compressedReq
				inputTokens = countClaudeInputTokens(&req)
				logger.Info("[智能压缩] 完成 - Token: %d, 消息数: %d", inputTokens, len(req.Messages))
			}
		}
	}

	// 带重试的账号选择和请求
	var acc *models.Account
	var resp *http.Response
	var triedIDs []string
	var lastErr error

	maxRetries := 3
	for retry := 0; retry <= maxRetries; retry++ {
		// 选择账号（排除已尝试的）
		acc, err = s.selectAccountExcluding(c.Request.Context(), triedIDs)
		if err != nil || acc == nil {
			logger.Warn("无可用账号 - 来源: %s, 已尝试: %d", clientIP, len(triedIDs))
			c.Set("error_message", "无可用账号，请先添加并配置账号")
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "无可用账号，请先添加并配置账号"})
			return
		}
		triedIDs = append(triedIDs, acc.ID)

		// 被动刷新策略：确保账号可用（刷新令牌和配额）
		// 执行流程：检查令牌 -> 刷新配额 -> 配额错误时刷新令牌 -> 重试
		// 整个过程对用户透明无感知
		acc, err = s.EnsureAccountReady(c.Request.Context(), acc)
		if err != nil {
			logger.Warn("账号 %s 被动刷新失败: %v，尝试换号", triedIDs[len(triedIDs)-1], err)
			continue
		}

		// 确保有访问令牌
		if acc == nil || acc.AccessToken == nil || *acc.AccessToken == "" {
			logger.Warn("账号被动刷新后仍无访问令牌")
			continue
		}

		// 转换并发送请求
		aqPayload, convErr := claude.ConvertClaudeToAmazonQ(&req, conversationID, false)
		if convErr != nil {
			logger.Error("请求转换失败 - 错误: %v", convErr)
			c.Set("error_message", convErr.Error())
			c.JSON(400, gin.H{"error": convErr.Error()})
			return
		}

		// 调试模式：打印转换后的 Amazon Q 请求体
		if aqPayloadJSON, err := json.MarshalIndent(aqPayload, "", "  "); err == nil {
			logger.Debug("[Claude->AmazonQ] 转换后请求体: %s", string(aqPayloadJSON))
		}

		machineId := s.ensureAccountMachineID(c.Request.Context(), acc)
		resp, err = s.aqClient.SendChatRequest(c.Request.Context(), *acc.AccessToken, machineId, acc.ID, aqPayload, logTimestamp)
		if err != nil {
			lastErr = err

			// 检查是否为客户端主动取消，不触发重试
			if c.Request.Context().Err() == context.Canceled {
				logger.Info("请求被客户端取消，终止重试")
				return
			}

			// 检查是否为不可重试错误
			if amazonq.IsNonRetriable(err) {
				if nrErr, ok := err.(*amazonq.NonRetriableError); ok {
					// 请求本身的错误（如输入过长），换号也没用，直接返回
					if nrErr.IsRequestErr {
						// 如果是上下文超出错误，打印具体的 token 数量
						if nrErr.Code == "CONTENT_LENGTH_EXCEEDS_THRESHOLD" || nrErr.Code == "INPUT_TOO_LONG" {
							inputTokens := countClaudeInputTokens(&req)
							logger.Error("【上下文超出】输入 Token: %d, 消息数: %d, 模型: %s", inputTokens, len(req.Messages), req.Model)
						}
						logger.Warn("检测到请求错误，终止重试 - 错误: %s, 提示: %s", nrErr.Message, nrErr.Hint)
						c.Set("error_message", nrErr.Message)
						c.JSON(http.StatusBadRequest, gin.H{
							"error": nrErr.Message,
							"code":  nrErr.Code,
							"hint":  nrErr.Hint,
						})
						return
					}
					// 账号相关错误，更新统计并尝试换号
					logger.Warn("检测到账号错误: %s - %s，尝试换号", nrErr.Code, nrErr.Message)
					s.QueueStatsUpdate(acc.ID, false)
					s.LogFailedRequest(c, acc, req.Model, req.Stream, nrErr.Message, startTime)
					// 根据错误码更新账号状态
					s.handleAccountStatusByError(c.Request.Context(), acc.ID, nrErr.Code)
					if retry < maxRetries {
						logger.Info("账号 %s 请求失败，换号重试 - 重试次数: %d", acc.ID, retry+1)
					}
					continue
				}
			}

			// 其他错误，更新统计并尝试换号
			s.QueueStatsUpdate(acc.ID, false)
			s.LogFailedRequest(c, acc, req.Model, req.Stream, err.Error(), startTime)
			if retry < maxRetries {
				logger.Info("账号 %s 请求失败，换号重试 - 重试次数: %d", acc.ID, retry+1)
			}
			continue
		}
		break
	}

	// 设置日志记录所需的信息
	c.Set("model", req.Model)
	c.Set("is_stream", req.Stream)
	if acc != nil {
		c.Set("account", acc)
	}

	if resp == nil {
		logger.Error("所有账号均失败 - 来源: %s, 模型: %s, 错误: %v", clientIP, req.Model, lastErr)
		if lastErr != nil {
			c.Set("error_message", lastErr.Error())
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": lastErr.Error()})
		return
	}
	defer resp.Body.Close()

	if req.Stream {
		// 控制台模式使用 UnifiedStreamHandler（前端期望的格式）
		// 标准 Claude API 使用 ClaudeStreamHandler（标准 Claude SSE 格式）
		isThinking := req.Thinking != nil
		if isConsoleMode {
			s.handleConsoleStreamResponse(c, resp, req.Model, conversationID, clientIP, startTime, len(req.Messages), acc, inputTokens, isThinking)
		} else {
			s.handleClaudeStreamResponse(c, resp, req.Model, conversationID, clientIP, startTime, len(req.Messages), acc, inputTokens, isThinking)
		}
	} else {
		s.handleClaudeNonStreamResponse(c, resp, req.Model, conversationID, clientIP, startTime, acc, len(req.Messages), inputTokens)
	}
}

// handleConsoleStreamResponse 处理控制台流式响应（使用前端期望的自定义 SSE 格式）
func (s *Server) handleConsoleStreamResponse(c *gin.Context, resp *http.Response, model, conversationID, clientIP string, startTime time.Time, msgCount int, acc *models.Account, inputTokens int, isThinking bool) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	// 使用 UnifiedStreamHandler 输出前端期望的 SSE 格式（meta, answer_delta, thinking_delta, done）
	handler := stream.NewUnifiedStreamHandler(model, conversationID, inputTokens)
	parser := stream.NewEventStreamParser()

	reader := bufio.NewReader(resp.Body)
	buf := make([]byte, 4096)

	c.Stream(func(w io.Writer) bool {
		n, err := reader.Read(buf)
		if err != nil {
			if err != io.EOF {
				logger.Info("读取流错误 - 来源: %s, 错误: %v", clientIP, err)
				w.Write([]byte(handler.Error(fmt.Sprintf("流读取错误: %v", err))))
			}

			doneEvent := handler.Finish("stop")
			if doneEvent != "" {
				w.Write([]byte(doneEvent))
			}

			duration := time.Since(startTime)
			s.QueueStatsUpdate(acc.ID, true)

			// 使用基于流式事件的 token 计数（更准确）
			outputTokens := handler.OutputTokens()
			c.Set("input_tokens", inputTokens)
			c.Set("output_tokens", outputTokens)

			modelDisplay := model
			if isThinking {
				modelDisplay = model + "-thinking"
			}
			logger.Info("控制台流式响应完成 - 来源: %s, 模型: %s, 消息数: %d, 输入token: %d, 输出token: %d, 耗时: %dms", clientIP, modelDisplay, msgCount, inputTokens, outputTokens, duration.Milliseconds())
			return false
		}

		events, err := parser.Feed(buf[:n])
		if err != nil {
			logger.Info("解析错误 - 来源: %s, 错误: %v", clientIP, err)
			w.Write([]byte(handler.Error(fmt.Sprintf("解析错误: %v", err))))
			return false
		}

		for _, event := range events {
			eventType := event.Headers[":event-type"]
			if eventType == "" {
				eventType = event.Headers["event-type"]
			}

			var payload map[string]interface{}
			if len(event.Payload) > 0 {
				if err := json.Unmarshal(event.Payload, &payload); err != nil {
					logger.Warn("解析事件 payload 失败: %v", err)
				}
			}

			sseEvents := handler.HandleEvent(eventType, payload)
			for _, sse := range sseEvents {
				w.Write([]byte(sse))
			}
		}

		return true
	})
}

func (s *Server) handleClaudeStreamResponse(c *gin.Context, resp *http.Response, model, conversationID, clientIP string, startTime time.Time, msgCount int, acc *models.Account, inputTokens int, isThinking bool) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("x-conversation-id", conversationID)

	// 使用 ClaudeStreamHandler 输出标准 Claude SSE 格式
	handler := stream.NewClaudeStreamHandler(model, inputTokens)
	handler.ConversationID = conversationID
	parser := stream.NewEventStreamParser()

	reader := bufio.NewReader(resp.Body)
	buf := make([]byte, 4096)

	c.Stream(func(w io.Writer) bool {
		n, err := reader.Read(buf)
		if err != nil {
			if err != io.EOF {
				logger.Info("读取流错误 - 来源: %s, 错误: %v", clientIP, err)
				w.Write([]byte(handler.Error(fmt.Sprintf("流读取错误: %v", err))))
			}

			// 输出 Claude 格式的结束事件
			doneEvent := handler.Finish()
			if doneEvent != "" {
				w.Write([]byte(doneEvent))
			}

			duration := time.Since(startTime)
			s.QueueStatsUpdate(acc.ID, true)

			// 使用基于流式事件的 token 计数（更准确）
			outputTokens := handler.OutputTokens()
			c.Set("input_tokens", inputTokens)
			c.Set("output_tokens", outputTokens)

			modelDisplay := model
			if isThinking {
				modelDisplay = model + "-thinking"
			}
			logger.Info("Claude 流式响应完成 - 来源: %s, 模型: %s, 消息数: %d, 输入token: %d, 输出token: %d, 耗时: %dms", clientIP, modelDisplay, msgCount, inputTokens, outputTokens, duration.Milliseconds())
			return false
		}

		events, err := parser.Feed(buf[:n])
		if err != nil {
			logger.Info("解析错误 - 来源: %s, 错误: %v", clientIP, err)
			w.Write([]byte(handler.Error(fmt.Sprintf("解析错误: %v", err))))
			return false
		}

		for _, event := range events {
			eventType := event.Headers[":event-type"]
			if eventType == "" {
				eventType = event.Headers["event-type"]
			}

			var payload map[string]interface{}
			if len(event.Payload) > 0 {
				if err := json.Unmarshal(event.Payload, &payload); err != nil {
					logger.Warn("解析事件 payload 失败: %v", err)
				}
			}

			// // 调试模式：打印原始事件
			// if payloadJSON, err := json.Marshal(payload); err == nil {
			// 	logger.Debug("[Claude 流响应] 事件类型: %s, Payload: %s", eventType, string(payloadJSON))
			// }

			sseEvents := handler.HandleEvent(eventType, payload)
			for _, sse := range sseEvents {
				w.Write([]byte(sse))
			}
		}

		return true
	})
}

func (s *Server) handleClaudeNonStreamResponse(c *gin.Context, resp *http.Response, model, conversationID, clientIP string, startTime time.Time, acc *models.Account, msgCount int, inputTokens int) {
	// 确保响应体被关闭
	defer resp.Body.Close()

	// 设置响应头
	c.Header("x-conversation-id", conversationID)

	parser := stream.NewEventStreamParser()
	var fullContent string

	// 工具调用追踪：支持增量输入累积
	type toolUseTracker struct {
		ID          string
		Name        string
		InputBuffer []string
		Completed   bool
	}
	toolUseMap := make(map[string]*toolUseTracker)
	var toolUseOrder []string // 保持顺序

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("读取响应体失败: %v", err)
		c.JSON(500, gin.H{"error": "读取响应失败"})
		return
	}
	events, err := parser.Feed(body)
	if err != nil {
		logger.Error("解析响应事件失败: %v", err)
		c.JSON(500, gin.H{"error": "解析响应失败"})
		return
	}

	for _, event := range events {
		eventType := event.Headers[":event-type"]
		if eventType == "" {
			eventType = event.Headers["event-type"]
		}

		var payload map[string]interface{}
		if len(event.Payload) > 0 {
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				logger.Warn("解析事件 payload 失败: %v", err)
			}
		}

		switch eventType {
		case "assistantResponseEvent":
			if content, ok := payload["content"].(string); ok {
				fullContent += content
			}
		case "toolUseEvent":
			toolUseID, _ := payload["toolUseId"].(string)
			toolName, _ := payload["name"].(string)
			toolInput := payload["input"]
			isStop, _ := payload["stop"].(bool)

			// 初始化新的工具调用
			if toolUseID != "" && toolName != "" {
				if _, exists := toolUseMap[toolUseID]; !exists {
					toolUseMap[toolUseID] = &toolUseTracker{
						ID:          toolUseID,
						Name:        toolName,
						InputBuffer: []string{},
					}
					toolUseOrder = append(toolUseOrder, toolUseID)
				}
			}

			// 累积工具输入（增量方式）
			if toolUseID != "" && toolInput != nil {
				if tracker, exists := toolUseMap[toolUseID]; exists {
					var fragment string
					switch v := toolInput.(type) {
					case string:
						fragment = v
					default:
						b, _ := json.Marshal(v)
						fragment = string(b)
					}
					if fragment != "" {
						tracker.InputBuffer = append(tracker.InputBuffer, fragment)
					}
				}
			}

			// 标记工具调用完成
			if isStop && toolUseID != "" {
				if tracker, exists := toolUseMap[toolUseID]; exists {
					tracker.Completed = true
				}
			}
		}
	}

	// 构建最终响应内容
	content := []interface{}{}
	if fullContent != "" {
		content = append(content, map[string]interface{}{"type": "text", "text": fullContent})
	}

	// 按顺序添加工具调用
	var toolCalls []map[string]interface{}
	for _, toolUseID := range toolUseOrder {
		tracker := toolUseMap[toolUseID]
		// 合并所有输入片段
		fullInputStr := ""
		for _, s := range tracker.InputBuffer {
			fullInputStr += s
		}

		// 解析 JSON 输入
		var toolInput map[string]interface{}
		if fullInputStr != "" {
			if err := json.Unmarshal([]byte(fullInputStr), &toolInput); err != nil {
				// 如果解析失败，使用原始字符串
				toolInput = map[string]interface{}{"raw": fullInputStr}
			}
		} else {
			toolInput = map[string]interface{}{}
		}

		content = append(content, map[string]interface{}{
			"type":  "tool_use",
			"id":    tracker.ID,
			"name":  tracker.Name,
			"input": toolInput,
		})

		inputJSON, _ := json.Marshal(toolInput)
		toolCalls = append(toolCalls, map[string]interface{}{
			"id":   tracker.ID,
			"type": "function",
			"function": map[string]interface{}{
				"name":      tracker.Name,
				"arguments": string(inputJSON),
			},
		})
	}

	stopReason := "end_turn"
	if len(toolCalls) > 0 {
		stopReason = "tool_use"
	}

	// 非流式响应无法使用 delta 计数，使用 tiktoken 估算
	// 注意：tiktoken 使用 OpenAI 的 cl100k_base 编码，与 Claude 的 tokenizer 存在差异
	outputTokens := estimateTokens(fullContent)

	response := map[string]interface{}{
		"id":              conversationID,
		"type":            "message",
		"role":            "assistant",
		"model":           model,
		"content":         content,
		"stop_reason":     stopReason,
		"conversation_id": conversationID,
		"conversationId":  conversationID,
		"usage": map[string]interface{}{
			"input_tokens":  inputTokens,
			"output_tokens": outputTokens,
		},
	}

	if len(toolCalls) > 0 {
		response["tool_calls"] = toolCalls
	}

	duration := time.Since(startTime)
	s.QueueStatsUpdate(acc.ID, true)

	// 设置 token 数量用于日志记录
	c.Set("input_tokens", inputTokens)
	c.Set("output_tokens", outputTokens)

	logger.Info("Claude 非流式响应完成 - 来源: %s, 模型: %s, 消息数: %d, 输入token: %d, 输出token: %d, 耗时: %dms", clientIP, model, msgCount, inputTokens, outputTokens, duration.Milliseconds())

	c.JSON(http.StatusOK, response)
}

// handleChatCompletions 处理 OpenAI Chat Completions API 端点
func (s *Server) handleChatCompletions(c *gin.Context) {
	clientIP := c.ClientIP()
	startTime := time.Now()
	logger.Debug("处理 Chat Completions 请求 - 来源: %s", clientIP)

	// 从上下文获取账号
	account := getAccount(c)
	if account == nil {
		logger.Error("上下文中未找到账号 - Chat Completions 请求")
		c.JSON(500, gin.H{"error": "上下文中未找到账号"})
		return
	}

	// 读取请求体并保存到 in.log
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求体失败"})
		return
	}

	// 生成时间戳用于日志配对
	logTimestamp := time.Now().Format("20060102_150405")
	c.Set("log_timestamp", logTimestamp)
	saveInLog(body, logTimestamp)

	// 解析请求
	var req models.ChatCompletionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		logger.Warn("无效的 Chat Completions 请求格式: %v", err)
		c.JSON(400, gin.H{"error": fmt.Sprintf("Invalid request: %v", err)})
		return
	}

	// 强制模型替换逻辑（haiku模型不替换）@author ygw
	var originalModelOpenAI string
	settingsOpenAI, _ := s.db.GetSettings(c.Request.Context())
	if settingsOpenAI != nil && settingsOpenAI.ForceModelEnabled && settingsOpenAI.ForceModel != "" {
		// 如果请求的是 haiku 模型，不进行替换
		modelLowerOpenAI := strings.ToLower(req.Model)
		if !strings.Contains(modelLowerOpenAI, "haiku") {
			originalModelOpenAI = req.Model
			req.Model = settingsOpenAI.ForceModel
			logger.Info("[强制模型] OpenAI格式已替换模型: %s -> %s", originalModelOpenAI, req.Model)
			c.Set("original_model", originalModelOpenAI)
		} else {
			logger.Debug("[强制模型] OpenAI格式haiku模型不替换: %s", req.Model)
		}
	}

	// 生成或获取 conversation_id（用于压缩缓存匹配）
	conversationID := uuid.New().String()

	inputTokens := countOpenAIInputTokens(&req)

	// 上下文压缩检查（OpenAI 格式，从数据库读取设置）
	if s.compressor != nil {
		settings, _ := s.db.GetSettings(c.Request.Context())
		if settings != nil && settings.CompressionEnabled {
			// 更新压缩器使用的模型
			if settings.CompressionModel != "" {
				s.compressor.SetSummaryModel(settings.CompressionModel)
			}

			// 先转换为 Claude 格式进行压缩检查
			claudeReqForCheck := convertOpenAIToClaude(&req)
			compressedReq, compressErr := s.compressor.CompressIfNeeded(c.Request.Context(), claudeReqForCheck,
				func(ctx context.Context, content, model string) (string, error) {
					return s.callSummaryAPI(ctx, content, model)
				})
			if compressErr != nil {
				logger.Warn("[智能压缩] OpenAI格式压缩失败: %v", compressErr)
			} else if compressedReq != nil {
				// 将压缩后的 Claude 消息转换回 OpenAI 格式
				req.Messages = convertClaudeMessagesToOpenAI(compressedReq.Messages)
				inputTokens = countOpenAIInputTokens(&req)
				logger.Info("[智能压缩] OpenAI完成 - Token: %d, 消息数: %d", inputTokens, len(req.Messages))
			}
		}
	}

	logger.Info("Chat Completions 请求 - 模型: %s, 流式: %v, 消息数: %d, 工具数: %d", req.Model, req.Stream, len(req.Messages), len(req.Tools))

	// 被动刷新策略：确保账号可用（刷新令牌和配额）
	// 执行流程：检查令牌 -> 刷新配额 -> 配额错误时刷新令牌 -> 重试
	// 整个过程对用户透明无感知
	account, err = s.EnsureAccountReady(c.Request.Context(), account)
	if err != nil {
		logger.Error("账号被动刷新失败: %v", err)
		c.Set("error_message", "账号准备失败: "+err.Error())
		c.JSON(502, gin.H{"error": "账号准备失败，请稍后重试"})
		return
	}
	if account == nil || account.AccessToken == nil || *account.AccessToken == "" {
		logger.Error("账号被动刷新后仍无访问令牌")
		c.Set("error_message", "账号没有访问令牌，请确保账号有有效的刷新令牌")
		c.JSON(503, gin.H{"error": "账号没有访问令牌，请确保账号有有效的刷新令牌"})
		return
	}

	// 转换请求：OpenAI -> Claude -> Amazon Q
	claudeReq := convertOpenAIToClaude(&req)
	aqPayload, convErr := claude.ConvertClaudeToAmazonQ(claudeReq, conversationID, false)
	if convErr != nil {
		logger.Error("请求转换失败 - 错误: %v", convErr)
		c.Set("error_message", convErr.Error())
		c.JSON(400, gin.H{"error": convErr.Error()})
		return
	}

	// 设置日志记录所需的信息
	c.Set("model", req.Model)
	c.Set("is_stream", req.Stream)

	responseID := "chatcmpl-" + uuid.New().String()[:8]
	machineId := s.ensureAccountMachineID(c.Request.Context(), account)
	resp, err := s.aqClient.SendChatRequest(c.Request.Context(), *account.AccessToken, machineId, account.ID, aqPayload, logTimestamp)
	if err != nil {
		logger.Error("OpenAI 请求失败 - 账号: %s, 错误: %v", account.ID, err)

		// 检查是否为不可重试错误
		if amazonq.IsNonRetriable(err) {
			if nrErr, ok := err.(*amazonq.NonRetriableError); ok {
				// 如果是上下文超出错误，打印具体的 token 数量
				if nrErr.Code == "CONTENT_LENGTH_EXCEEDS_THRESHOLD" || nrErr.Code == "INPUT_TOO_LONG" {
					logger.Error("【上下文超出】输入 Token: %d, 消息数: %d, 模型: %s", inputTokens, len(req.Messages), req.Model)
				}
				// 请求错误不计入账号统计
				if !nrErr.IsRequestErr {
					s.QueueStatsUpdate(account.ID, false)
					// 根据错误码更新账号状态
					s.handleAccountStatusByError(c.Request.Context(), account.ID, nrErr.Code)
				}
				c.Set("error_message", nrErr.Message)
				c.JSON(http.StatusBadRequest, gin.H{
					"error": gin.H{
						"message": nrErr.Message,
						"type":    nrErr.Code,
						"hint":    nrErr.Hint,
					},
				})
				return
			}
		}

		s.QueueStatsUpdate(account.ID, false)
		c.Set("error_message", err.Error())
		c.JSON(502, gin.H{"error": fmt.Sprintf("发送请求失败: %v", err)})
		return
	}
	defer resp.Body.Close()

	if req.Stream {
		s.handleOpenAIStreamResponse(c, resp, req.Model, responseID, clientIP, startTime, len(req.Messages), account, inputTokens)
	} else {
		s.handleOpenAINonStreamResponse(c, resp, req.Model, responseID, clientIP, startTime, account, len(req.Messages), inputTokens)
	}
}

func (s *Server) handleOpenAIStreamResponse(c *gin.Context, resp *http.Response, model, responseID, clientIP string, startTime time.Time, msgCount int, acc *models.Account, inputTokens int) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	handler := stream.NewOpenAIStreamHandler(responseID, model)
	parser := stream.NewEventStreamParser()

	reader := bufio.NewReader(resp.Body)
	buf := make([]byte, 4096)

	c.Stream(func(w io.Writer) bool {
		n, err := reader.Read(buf)
		if err != nil {
			if err != io.EOF {
				logger.Info("读取流错误 - 来源: %s, 错误: %v", clientIP, err)
			}
			doneEvent := handler.Finish()
			if doneEvent != "" {
				w.Write([]byte(doneEvent))
			}

			duration := time.Since(startTime)
			s.QueueStatsUpdate(acc.ID, true)

			// 使用基于流式事件的 token 计数（更准确）
			outputTokens := handler.OutputTokens()
			c.Set("input_tokens", inputTokens)
			c.Set("output_tokens", outputTokens)

			logger.Info("OpenAI 流式响应完成 - 来源: %s, 模型: %s, 消息数: %d, 输入token: %d, 输出token: %d, 耗时: %dms", clientIP, model, msgCount, inputTokens, outputTokens, duration.Milliseconds())
			return false
		}

		events, err := parser.Feed(buf[:n])
		if err != nil {
			logger.Info("解析错误 - 来源: %s, 错误: %v", clientIP, err)
			return false
		}

		for _, event := range events {
			eventType := event.Headers[":event-type"]
			if eventType == "" {
				eventType = event.Headers["event-type"]
			}

			var payload map[string]interface{}
			if len(event.Payload) > 0 {
				if err := json.Unmarshal(event.Payload, &payload); err != nil {
					logger.Warn("解析事件 payload 失败: %v", err)
				}
			}

			sseEvents := handler.HandleEvent(eventType, payload)
			for _, sse := range sseEvents {
				w.Write([]byte(sse))
			}
		}

		return true
	})
}

func (s *Server) handleOpenAINonStreamResponse(c *gin.Context, resp *http.Response, model, responseID, clientIP string, startTime time.Time, acc *models.Account, msgCount int, inputTokens int) {
	// 确保响应体被关闭
	defer resp.Body.Close()

	parser := stream.NewEventStreamParser()
	var fullContent string
	var toolCalls []map[string]interface{}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("读取响应体失败: %v", err)
		c.JSON(500, gin.H{"error": map[string]interface{}{
			"message": "读取响应失败",
			"type":    "server_error",
		}})
		return
	}
	events, err := parser.Feed(body)
	if err != nil {
		logger.Error("解析响应事件失败: %v", err)
		c.JSON(500, gin.H{"error": map[string]interface{}{
			"message": "解析响应失败",
			"type":    "server_error",
		}})
		return
	}

	for _, event := range events {
		eventType := event.Headers[":event-type"]
		if eventType == "" {
			eventType = event.Headers["event-type"]
		}

		var payload map[string]interface{}
		if len(event.Payload) > 0 {
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				logger.Warn("解析事件 payload 失败: %v", err)
			}
		}

		if eventType == "assistantResponseEvent" {
			if content, ok := payload["content"].(string); ok {
				fullContent += content
			}
		} else if eventType == "codeReferenceEvent" {
			if toolUses, ok := payload["toolUses"].([]interface{}); ok {
				for _, tu := range toolUses {
					if toolUse, ok := tu.(map[string]interface{}); ok {
						toolCallID, _ := toolUse["toolUseId"].(string)
						toolName, _ := toolUse["name"].(string)
						toolInput, _ := toolUse["input"].(map[string]interface{})

						inputJSON, _ := json.Marshal(toolInput)
						toolCalls = append(toolCalls, map[string]interface{}{
							"id":   toolCallID,
							"type": "function",
							"function": map[string]interface{}{
								"name":      toolName,
								"arguments": string(inputJSON),
							},
						})
					}
				}
			}
		}
	}

	// 使用传入的 inputTokens，输出 token 使用 tokenizer 计算
	promptTokens := inputTokens
	completionTokens := tokenizer.CountTokens(fullContent)

	message := map[string]interface{}{
		"role":    "assistant",
		"content": fullContent,
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls
	}

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

	response := map[string]interface{}{
		"id":      responseID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"message":       message,
				"finish_reason": finishReason,
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
		},
	}

	duration := time.Since(startTime)
	s.QueueStatsUpdate(acc.ID, true)

	// 设置 token 数量用于日志记录
	c.Set("input_tokens", promptTokens)
	c.Set("output_tokens", completionTokens)

	logger.Info("OpenAI 非流式响应完成 - 来源: %s, 模型: %s, 消息数: %d, 输入token: %d, 输出token: %d, 耗时: %dms", clientIP, model, msgCount, promptTokens, completionTokens, duration.Milliseconds())

	c.JSON(http.StatusOK, response)
}

// handleCountTokens 处理令牌计数端点
func (s *Server) handleCountTokens(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取请求体失败"})
		return
	}

	tokenCount := estimateTokens(string(body))
	c.JSON(http.StatusOK, gin.H{"input_tokens": tokenCount})
}

// handleConsoleChatTest 处理控制台聊天测试
func (s *Server) handleConsoleChatTest(c *gin.Context) {
	logger.Info("控制台聊天测试请求 - 来源: %s", c.ClientIP())

	account, err := s.selectAccount(c.Request.Context())
	if err != nil {
		logger.Error("为控制台测试选择账号失败: %v", err)
		c.JSON(503, gin.H{"error": "无可用账号，请先添加并配置账号"})
		return
	}

	c.Set("account", account)
	s.handleChatCompletions(c)
}

// handleConsoleClaudeTest 处理控制台 Claude 测试（支持 thinking 模式）
func (s *Server) handleConsoleClaudeTest(c *gin.Context) {
	logger.Info("控制台 Claude 测试请求（thinking 支持）- 来源: %s", c.ClientIP())

	// 设置控制台模式标志，跳过 API key 验证
	c.Set("console_mode", true)

	// 调用完整的 Claude Messages 处理器
	s.handleClaudeMessages(c)
}

// 辅助函数
func (s *Server) selectAccountExcluding(ctx context.Context, excludeIDs []string) (*models.Account, error) {
	// 使用账号池缓存，避免每次请求查询数据库
	account := s.accountPool.GetAccountExcluding(excludeIDs)
	if account == nil {
		// 缓存为空或所有账号都被排除，尝试刷新缓存后重试
		logger.Debug("账号池无可用账号，尝试刷新缓存")
		s.accountPool.Refresh(ctx)
		account = s.accountPool.GetAccountExcluding(excludeIDs)
	}

	if account == nil {
		return nil, fmt.Errorf("no available accounts")
	}

	return account, nil
}

func estimateTokensFromBuffer(buffer []string) int {
	text := ""
	for _, s := range buffer {
		text += s
	}
	return tokenizer.CountTokens(text)
}

// estimateTokens 已弃用 - 保留用于向后兼容
func estimateTokens(text string) int {
	return tokenizer.CountTokens(text)
}

// countClaudeInputTokens 计算 Claude 请求的输入 token
// 使用 Claude 专用 tokenizer (基于 ai-tokenizer)，准确率约 97%+
func countClaudeInputTokens(req *models.ClaudeRequest) int {
	total := 0
	// 计算 system prompt（Claude 的 system prompt 有额外开销）
	if system, ok := req.System.(string); ok && system != "" {
		total += tokenizer.CountTokens(system)
		total += 3 // system prompt 格式开销
	}
	// 计算消息内容
	for _, msg := range req.Messages {
		total += 4 // role + 格式开销（每条消息的角色和结构化开销）
		if content, ok := msg.Content.(string); ok {
			total += tokenizer.CountTokens(content)
		} else if contentList, ok := msg.Content.([]interface{}); ok {
			for _, block := range contentList {
				if blockMap, ok := block.(map[string]interface{}); ok {
					if blockMap["type"] == "text" {
						if text, ok := blockMap["text"].(string); ok {
							total += tokenizer.CountTokens(text)
						}
					}
				}
			}
		}
	}
	return total
}

// countOpenAIInputTokens 计算 OpenAI 格式请求的输入 token
// 虽然是 OpenAI 格式，但后端仍是 Claude/Amazon Q，因此使用 Claude tokenizer
func countOpenAIInputTokens(req *models.ChatCompletionRequest) int {
	total := 0
	for _, msg := range req.Messages {
		total += 4 // role + 格式开销
		if content, ok := msg.Content.(string); ok {
			total += tokenizer.CountTokens(content)
		}
	}
	return total
}

func boolPtr(b bool) *bool {
	return &b
}

// convertOpenAIToClaude 将 OpenAI 请求转换为 Claude 请求（与 claude-api-main 一致）
func convertOpenAIToClaude(req *models.ChatCompletionRequest) *models.ClaudeRequest {
	var messages []models.ClaudeMessage
	var system string

	for _, msg := range req.Messages {
		if msg.Role == "system" {
			system = extractOpenAIContent(msg.Content)
		} else {
			claudeMsg := models.ClaudeMessage{
				Role:    msg.Role,
				Content: msg.Content,
			}

			// 转换 tool_calls 为 Claude 格式
			if toolCalls, ok := msg.ToolCalls.([]models.ToolCall); ok && len(toolCalls) > 0 {
				var content []interface{}
				if text := extractOpenAIContent(msg.Content); text != "" {
					content = append(content, map[string]interface{}{
						"type": "text",
						"text": text,
					})
				}
				for _, tc := range toolCalls {
					var input map[string]interface{}
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &input); err != nil {
						logger.Warn("解析工具调用参数失败: %v", err)
					}
					content = append(content, map[string]interface{}{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Function.Name,
						"input": input,
					})
				}
				claudeMsg.Content = content
			}

			// 转换 tool_call_id 为 Claude 格式
			if msg.ToolCallID != "" {
				claudeMsg.Content = []interface{}{
					map[string]interface{}{
						"type":        "tool_result",
						"tool_use_id": msg.ToolCallID,
						"content":     extractOpenAIContent(msg.Content),
					},
				}
			}

			messages = append(messages, claudeMsg)
		}
	}

	claudeReq := &models.ClaudeRequest{
		Model:     req.Model,
		Messages:  messages,
		MaxTokens: 4096,
		Stream:    req.Stream,
	}

	if system != "" {
		claudeReq.System = system
	}

	if len(req.Tools) > 0 {
		for _, tool := range req.Tools {
			// 支持 OpenAI 格式
			if tool.Function != nil && tool.Function.Name != "" && tool.Function.Parameters != nil {
				claudeReq.Tools = append(claudeReq.Tools, models.ClaudeTool{
					Name:        tool.Function.Name,
					Description: tool.Function.Description,
					InputSchema: tool.Function.Parameters,
				})
			} else if tool.Name != "" && tool.InputSchema != nil {
				// 支持 Claude 原生格式
				claudeReq.Tools = append(claudeReq.Tools, models.ClaudeTool{
					Name:        tool.Name,
					Description: tool.Description,
					InputSchema: tool.InputSchema,
				})
			}
		}
	}

	return claudeReq
}

// extractOpenAIContent 从 OpenAI 消息内容中提取文本
func extractOpenAIContent(content interface{}) string {
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				if text, ok := m["text"].(string); ok {
					return text
				}
			}
		}
	}
	return ""
}

// convertClaudeMessagesToOpenAI 将 Claude 消息转换为 OpenAI 格式
// @author ygw
func convertClaudeMessagesToOpenAI(claudeMsgs []models.ClaudeMessage) []models.ChatMessage {
	openAIMsgs := make([]models.ChatMessage, 0, len(claudeMsgs))
	for _, msg := range claudeMsgs {
		openAIMsg := models.ChatMessage{
			Role: msg.Role,
		}
		// 提取内容
		if content, ok := msg.Content.(string); ok {
			openAIMsg.Content = content
		} else {
			// 复杂内容，提取文本部分
			openAIMsg.Content = extractTextFromClaudeContent(msg.Content)
		}
		openAIMsgs = append(openAIMsgs, openAIMsg)
	}
	return openAIMsgs
}

// extractTextFromClaudeContent 从 Claude 复杂内容中提取文本
func extractTextFromClaudeContent(content interface{}) string {
	blocks, ok := content.([]interface{})
	if !ok {
		return ""
	}
	var texts []string
	for _, block := range blocks {
		if m, ok := block.(map[string]interface{}); ok {
			if m["type"] == "text" {
				if text, ok := m["text"].(string); ok {
					texts = append(texts, text)
				}
			}
		}
	}
	return strings.Join(texts, "\n")
}

// callSummaryAPI 调用 API 生成摘要（用于上下文压缩）
// @author ygw
func (s *Server) callSummaryAPI(ctx context.Context, content, model string) (string, error) {
	// 选择一个可用账号
	acc, err := s.selectAccountExcluding(ctx, nil)
	if err != nil || acc == nil {
		return "", fmt.Errorf("无可用账号生成摘要")
	}

	// 确保有访问令牌
	if acc.AccessToken == nil || *acc.AccessToken == "" {
		if err := s.refreshAccountToken(ctx, acc.ID); err != nil {
			return "", fmt.Errorf("刷新令牌失败: %w", err)
		}
		acc, _ = s.db.GetAccount(ctx, acc.ID)
	}

	// 构建简化的请求
	req := &models.ClaudeRequest{
		Model: model,
		Messages: []models.ClaudeMessage{
			{Role: "user", Content: content},
		},
		MaxTokens: 4096,
		Stream:    false,
	}

	// 转换为 Amazon Q 格式
	conversationID := uuid.New().String()
	aqPayload, err := claude.ConvertClaudeToAmazonQ(req, conversationID, false)
	if err != nil {
		return "", fmt.Errorf("转换请求失败: %w", err)
	}

	// 发送请求
	machineId := s.ensureAccountMachineID(ctx, acc)
	resp, err := s.aqClient.SendChatRequest(ctx, *acc.AccessToken, machineId, acc.ID, aqPayload, "")
	if err != nil {
		return "", fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 解析响应，提取文本内容
	var result strings.Builder
	eventChan, errChan := amazonq.StreamEventGenerator(ctx, resp)

	for {
		select {
		case event, ok := <-eventChan:
			if !ok {
				return result.String(), nil
			}
			// 提取文本内容 - Amazon Q 使用 assistantResponseEvent 事件类型
			if event.EventType == "assistantResponseEvent" {
				if content, ok := event.Payload["content"].(string); ok {
					result.WriteString(content)
				}
			}
		case err := <-errChan:
			if err != nil {
				return result.String(), err
			}
		case <-ctx.Done():
			return result.String(), ctx.Err()
		}
	}
}

// handleGetLogs 获取请求日志
func (s *Server) handleGetLogs(c *gin.Context) {
	logger.Info("获取请求日志 - 来源: %s", c.ClientIP())

	limit := 100
	offset := 0
	if l := c.Query("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	if o := c.Query("offset"); o != "" {
		fmt.Sscanf(o, "%d", &offset)
	}

	filters := &models.LogFilters{}
	if startTime := c.Query("start_time"); startTime != "" {
		// 将前端传来的时间转换为本地时间格式
		if t, err := time.Parse(time.RFC3339, startTime); err == nil {
			localTime := t.Local().Format(models.TimeFormat)
			filters.StartTime = &localTime
		} else {
			filters.StartTime = &startTime
		}
	}
	if endTime := c.Query("end_time"); endTime != "" {
		if t, err := time.Parse(time.RFC3339, endTime); err == nil {
			localTime := t.Local().Format(models.TimeFormat)
			filters.EndTime = &localTime
		} else {
			filters.EndTime = &endTime
		}
	}
	if clientIP := c.Query("client_ip"); clientIP != "" {
		filters.ClientIP = &clientIP
	}
	if accountID := c.Query("account_id"); accountID != "" {
		filters.AccountID = &accountID
	}
	if endpointType := c.Query("endpoint_type"); endpointType != "" {
		filters.EndpointType = &endpointType
	}
	if isSuccess := c.Query("is_success"); isSuccess != "" {
		success := isSuccess == "true"
		filters.IsSuccess = &success
	}

	logs, err := s.db.GetRequestLogs(c.Request.Context(), filters, limit, offset)
	if err != nil {
		logger.Error("获取请求日志失败: %v", err)
		c.JSON(500, gin.H{"error": "获取请求日志失败"})
		return
	}

	// 获取系统设置中的 API Key（用于判断管理员）
	settings, _ := s.db.GetSettings(c.Request.Context())
	systemAPIKey := ""
	if settings != nil {
		systemAPIKey = settings.APIKey
	}

	// 收集所有用户ID，批量查询用户信息
	userIDs := make(map[string]bool)
	for _, log := range logs {
		if log.UserID != nil && *log.UserID != "" {
			userIDs[*log.UserID] = true
		}
	}

	// 批量获取用户信息（性能优化：一次查询所有用户）
	userMap := make(map[string]*models.User)
	if len(userIDs) > 0 {
		ids := make([]string, 0, len(userIDs))
		for id := range userIDs {
			ids = append(ids, id)
		}
		users, _ := s.db.GetUsersByIDs(c.Request.Context(), ids)
		for _, u := range users {
			userMap[u.ID] = u
		}
	}

	// 为每条日志计算美元成本和用户归属信息
	// @author ygw
	for i := range logs {
		logs[i].CostUSD = logs[i].CalculateCost()

		// 填充用户归属信息
		if logs[i].UserID != nil {
			if user, ok := userMap[*logs[i].UserID]; ok {
				logs[i].UserName = &user.Name
				userType := "normal"
				if user.IsVip {
					userType = "vip"
				}
				logs[i].UserType = &userType
			}
		} else if logs[i].APIKeyPrefix != nil {
			// 检查是否使用系统 API Key（管理员）
			if systemAPIKey != "" && len(systemAPIKey) > 8 {
				prefix := systemAPIKey[:8] + "..."
				if *logs[i].APIKeyPrefix == prefix {
					adminName := "管理员"
					adminType := "admin"
					logs[i].UserName = &adminName
					logs[i].UserType = &adminType
				}
			}
		}
	}

	// 获取总数
	total, err := s.db.GetRequestLogsCount(c.Request.Context(), filters)
	if err != nil {
		logger.Error("获取日志总数失败: %v", err)
		total = 0
	}

	c.JSON(200, gin.H{"logs": logs, "total": total, "limit": limit, "offset": offset})
}

// handleGetStats 获取请求统计（支持完整筛选条件）
// @author ygw
func (s *Server) handleGetStats(c *gin.Context) {
	logger.Debug("获取请求统计 - 来源: %s", c.ClientIP())

	// 构建筛选条件（与 handleGetLogs 一致）
	filters := &models.LogFilters{}

	// 时间范围
	if startTime := c.Query("start_time"); startTime != "" {
		if t, err := time.Parse(time.RFC3339, startTime); err == nil {
			formatted := t.Local().Format(models.TimeFormat)
			filters.StartTime = &formatted
		} else {
			filters.StartTime = &startTime
		}
	} else {
		defaultStart := time.Now().Add(-24 * time.Hour).Format(models.TimeFormat)
		filters.StartTime = &defaultStart
	}

	if endTime := c.Query("end_time"); endTime != "" {
		if t, err := time.Parse(time.RFC3339, endTime); err == nil {
			formatted := t.Local().Format(models.TimeFormat)
			filters.EndTime = &formatted
		} else {
			filters.EndTime = &endTime
		}
	} else {
		defaultEnd := models.CurrentTime()
		filters.EndTime = &defaultEnd
	}

	// 客户端IP筛选
	if clientIP := c.Query("client_ip"); clientIP != "" {
		filters.ClientIP = &clientIP
	}

	// 账号ID筛选
	if accountID := c.Query("account_id"); accountID != "" {
		filters.AccountID = &accountID
	}

	// 端点类型筛选
	if endpointType := c.Query("endpoint_type"); endpointType != "" {
		filters.EndpointType = &endpointType
	}

	// 成功状态筛选
	if isSuccessStr := c.Query("is_success"); isSuccessStr != "" {
		isSuccess := isSuccessStr == "true" || isSuccessStr == "1"
		filters.IsSuccess = &isSuccess
	}

	stats, err := s.db.GetRequestStatsWithFilters(c.Request.Context(), filters)
	if err != nil {
		logger.Error("获取请求统计失败: %v", err)
		c.JSON(500, gin.H{"error": "获取请求统计失败"})
		return
	}

	// 计算美元成本
	stats.InputCostUSD, stats.OutputCostUSD, stats.TotalCostUSD = models.CalculateTokenCost(stats.TotalInputTokens, stats.TotalOutputTokens)

	c.JSON(200, stats)
}

// handleCleanupLogs 清理旧日志
func (s *Server) handleCleanupLogs(c *gin.Context) {
	logger.Info("清理旧日志 - 来源: %s", c.ClientIP())

	var req struct {
		DaysToKeep int `json:"days_to_keep"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.DaysToKeep <= 0 {
		req.DaysToKeep = 30
	}

	count, err := s.db.CleanupOldLogs(c.Request.Context(), req.DaysToKeep)
	if err != nil {
		logger.Error("清理旧日志失败: %v", err)
		c.JSON(500, gin.H{"error": "清理旧日志失败"})
		return
	}

	logger.Info("清理旧日志成功 - 删除数量: %d", count)
	c.JSON(200, gin.H{"deleted": count, "message": fmt.Sprintf("已删除 %d 条日志", count)})
}

// handleGetBlockedIPs 获取被封禁的IP列表
func (s *Server) handleGetBlockedIPs(c *gin.Context) {
	logger.Info("获取被封禁IP列表 - 来源: %s", c.ClientIP())

	ips, err := s.db.GetBlockedIPs(c.Request.Context())
	if err != nil {
		logger.Error("获取被封禁IP列表失败: %v", err)
		c.JSON(500, gin.H{"error": "获取被封禁IP列表失败"})
		return
	}

	c.JSON(200, gin.H{"blocked_ips": ips})
}

// handleBlockIP 封禁IP
func (s *Server) handleBlockIP(c *gin.Context) {
	logger.Info("封禁IP - 来源: %s", c.ClientIP())

	var req struct {
		IP     string  `json:"ip" binding:"required"`
		Reason *string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warn("封禁IP失败 - 无效的请求格式: %v", err)
		c.JSON(400, gin.H{"error": "无效的请求格式"})
		return
	}

	reason := "手动封禁"
	if req.Reason != nil {
		reason = *req.Reason
	}

	if err := s.db.BlockIP(c.Request.Context(), req.IP, reason); err != nil {
		logger.Error("封禁IP失败 - IP: %s, 错误: %v", req.IP, err)
		c.JSON(500, gin.H{"error": "封禁IP失败"})
		return
	}

	// 立即更新缓存
	s.blockedIPCache.Store(req.IP, true)

	logger.Info("IP封禁成功 - IP: %s, 原因: %s", req.IP, reason)
	c.JSON(200, gin.H{"message": "IP封禁成功", "ip": req.IP})
}

// handleGetVisitorIPs 获取所有访问IP列表
func (s *Server) handleGetVisitorIPs(c *gin.Context) {
	logger.Info("获取访问IP列表 - 来源: %s", c.ClientIP())

	ips, err := s.db.GetVisitorIPs(c.Request.Context())
	if err != nil {
		logger.Error("获取访问IP列表失败: %v", err)
		c.JSON(500, gin.H{"error": "获取访问IP列表失败"})
		return
	}

	c.JSON(200, gin.H{"visitor_ips": ips})
}

// handleUnblockIP 解封IP
func (s *Server) handleUnblockIP(c *gin.Context) {
	logger.Info("解封IP - 来源: %s", c.ClientIP())

	var req struct {
		IP string `json:"ip" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warn("解封IP失败 - 无效的请求格式: %v", err)
		c.JSON(400, gin.H{"error": "无效的请求格式"})
		return
	}

	if err := s.db.UnblockIP(c.Request.Context(), req.IP); err != nil {
		logger.Error("解封IP失败 - IP: %s, 错误: %v", req.IP, err)
		c.JSON(500, gin.H{"error": "解封IP失败"})
		return
	}

	// 立即从缓存中移除
	s.blockedIPCache.Delete(req.IP)

	logger.Info("IP解封成功 - IP: %s", req.IP)
	c.JSON(200, gin.H{"message": "IP解封成功", "ip": req.IP})
}

// handleGetIPConfig 获取单个IP的配置
func (s *Server) handleGetIPConfig(c *gin.Context) {
	ip := c.Param("ip")
	if ip == "" {
		c.JSON(400, gin.H{"error": "IP地址不能为空"})
		return
	}

	config, err := s.db.GetIPConfig(c.Request.Context(), ip)
	if err != nil {
		logger.Error("获取IP配置失败 - IP: %s, 错误: %v", ip, err)
		c.JSON(500, gin.H{"error": "获取IP配置失败"})
		return
	}

	if config == nil {
		// 返回空配置
		c.JSON(200, gin.H{
			"ip":                  ip,
			"notes":               nil,
			"rate_limit_rpm":      0,
			"daily_request_limit": 0,
		})
		return
	}

	c.JSON(200, config)
}

// handleUpdateIPConfig 更新IP配置（备注、限制等）
func (s *Server) handleUpdateIPConfig(c *gin.Context) {
	ip := c.Param("ip")
	if ip == "" {
		c.JSON(400, gin.H{"error": "IP地址不能为空"})
		return
	}

	var updates models.IPConfigUpdate
	if err := c.ShouldBindJSON(&updates); err != nil {
		logger.Warn("更新IP配置失败 - 无效的请求格式: %v", err)
		c.JSON(400, gin.H{"error": "无效的请求格式"})
		return
	}

	config, err := s.db.UpsertIPConfig(c.Request.Context(), ip, &updates)
	if err != nil {
		logger.Error("更新IP配置失败 - IP: %s, 错误: %v", ip, err)
		c.JSON(500, gin.H{"error": "更新IP配置失败"})
		return
	}

	logger.Info("IP配置更新成功 - IP: %s", ip)
	c.JSON(200, config)
}

// handleServerLogsStream 实时推送服务日志（SSE）
func (s *Server) handleServerLogsStream(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	// 发送连接成功消息
	c.SSEvent("connected", "ok")
	c.Writer.Flush()

	logCh := logger.Subscribe()
	defer logger.Unsubscribe(logCh)

	c.Stream(func(w io.Writer) bool {
		select {
		case msg, ok := <-logCh:
			if !ok {
				return false
			}
			c.SSEvent("log", msg)
			return true
		case <-c.Request.Context().Done():
			return false
		}
	})
}

// ==================== Claude Code 配置 ====================

// ClaudeCodeSettings Claude Code 配置文件结构
type ClaudeCodeSettings struct {
	Env map[string]string `json:"env,omitempty"`
}

// handleGetClaudeCodeConfig 获取当前 Claude Code 配置
func (s *Server) handleGetClaudeCodeConfig(c *gin.Context) {
	config, err := readClaudeCodeConfig()
	if err != nil {
		c.JSON(200, gin.H{
			"configured": false,
			"baseUrl":    "",
			"apiKey":     "",
			"error":      err.Error(),
		})
		return
	}

	baseUrl := ""
	apiKey := ""
	if config.Env != nil {
		if v, ok := config.Env["ANTHROPIC_BASE_URL"]; ok {
			baseUrl = v
		}
		if v, ok := config.Env["ANTHROPIC_AUTH_TOKEN"]; ok {
			apiKey = v
		}
	}

	c.JSON(200, gin.H{
		"configured": baseUrl != "" && apiKey != "",
		"baseUrl":    baseUrl,
		"apiKey":     apiKey,
	})
}

// handleSaveClaudeCodeConfig 保存 Claude Code 配置
func (s *Server) handleSaveClaudeCodeConfig(c *gin.Context) {
	var req struct {
		BaseUrl string `json:"baseUrl"`
		ApiKey  string `json:"apiKey"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "无效的请求格式"})
		return
	}

	if req.BaseUrl == "" || req.ApiKey == "" {
		c.JSON(400, gin.H{"error": "baseUrl 和 apiKey 不能为空"})
		return
	}

	if err := writeClaudeCodeConfig(req.BaseUrl, req.ApiKey); err != nil {
		logger.Error("保存 Claude Code 配置失败: %v", err)
		c.JSON(500, gin.H{"error": "保存配置失败: " + err.Error()})
		return
	}

	logger.Info("Claude Code 配置已更新 - BaseUrl: %s", req.BaseUrl)
	c.JSON(200, gin.H{"success": true})
}

// readClaudeCodeConfig 读取 Claude Code 配置文件
func readClaudeCodeConfig() (*ClaudeCodeSettings, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("获取用户目录失败: %w", err)
	}

	configPath := filepath.Join(homeDir, ".claude", "settings.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &ClaudeCodeSettings{}, nil
		}
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var config ClaudeCodeSettings
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	return &config, nil
}

// writeClaudeCodeConfig 写入 Claude Code 配置文件
func writeClaudeCodeConfig(baseUrl, apiKey string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("获取用户目录失败: %w", err)
	}

	claudeDir := filepath.Join(homeDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	configPath := filepath.Join(claudeDir, "settings.json")

	// 读取现有配置
	var existingData map[string]interface{}
	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, &existingData); err != nil {
			logger.Warn("解析现有配置文件失败: %v", err)
		}
	}
	if existingData == nil {
		existingData = make(map[string]interface{})
	}

	// 更新 env 字段
	env, ok := existingData["env"].(map[string]interface{})
	if !ok {
		env = make(map[string]interface{})
	}
	env["ANTHROPIC_BASE_URL"] = baseUrl
	env["ANTHROPIC_AUTH_TOKEN"] = apiKey
	existingData["env"] = env

	// 写入文件
	data, err := json.MarshalIndent(existingData, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	return nil
}

// ==================== Droid 配置 ====================

// DroidSettings Droid 配置文件结构
// @author ygw
type DroidSettings struct {
	Env map[string]string `json:"env,omitempty"`
}

// handleGetDroidConfig 获取当前 Droid 配置
// @author ygw
func (s *Server) handleGetDroidConfig(c *gin.Context) {
	config, err := readDroidConfig()
	if err != nil {
		c.JSON(200, gin.H{
			"configured": false,
			"baseUrl":    "",
			"apiKey":     "",
			"error":      err.Error(),
		})
		return
	}

	var baseUrl, apiKey string
	if config.Env != nil {
		if v, ok := config.Env["ANTHROPIC_BASE_URL"]; ok {
			baseUrl = v
		}
		if v, ok := config.Env["ANTHROPIC_AUTH_TOKEN"]; ok {
			apiKey = v
		}
	}

	c.JSON(200, gin.H{
		"configured": baseUrl != "" && apiKey != "",
		"baseUrl":    baseUrl,
		"apiKey":     apiKey,
	})
}

// handleSaveDroidConfig 保存 Droid 配置
// @author ygw
func (s *Server) handleSaveDroidConfig(c *gin.Context) {
	var req struct {
		BaseUrl string `json:"baseUrl"`
		ApiKey  string `json:"apiKey"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "无效的请求格式"})
		return
	}

	if req.BaseUrl == "" || req.ApiKey == "" {
		c.JSON(400, gin.H{"error": "baseUrl 和 apiKey 不能为空"})
		return
	}

	if err := writeDroidConfig(req.BaseUrl, req.ApiKey); err != nil {
		logger.Error("保存 Droid 配置失败: %v", err)
		c.JSON(500, gin.H{"error": "保存配置失败: " + err.Error()})
		return
	}

	logger.Info("Droid 配置已更新 - BaseUrl: %s", req.BaseUrl)
	c.JSON(200, gin.H{"success": true})
}

// readDroidConfig 读取 Droid 配置文件
// @author ygw
func readDroidConfig() (*DroidSettings, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("获取用户目录失败: %w", err)
	}

	configPath := filepath.Join(homeDir, ".droid", "settings.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &DroidSettings{}, nil
		}
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var config DroidSettings
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	return &config, nil
}

// writeDroidConfig 写入 Droid 配置文件
// @author ygw
func writeDroidConfig(baseUrl, apiKey string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("获取用户目录失败: %w", err)
	}

	droidDir := filepath.Join(homeDir, ".droid")
	if err := os.MkdirAll(droidDir, 0755); err != nil {
		return fmt.Errorf("创建目录失败: %w", err)
	}

	configPath := filepath.Join(droidDir, "settings.json")

	// 读取现有配置
	var existingData map[string]interface{}
	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, &existingData); err != nil {
			logger.Warn("解析现有配置文件失败: %v", err)
		}
	}
	if existingData == nil {
		existingData = make(map[string]interface{})
	}

	// 更新 env 字段
	env, ok := existingData["env"].(map[string]interface{})
	if !ok {
		env = make(map[string]interface{})
	}
	env["ANTHROPIC_BASE_URL"] = baseUrl
	env["ANTHROPIC_AUTH_TOKEN"] = apiKey
	existingData["env"] = env

	// 写入文件
	data, err := json.MarshalIndent(existingData, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	return nil
}

// saveInLog 保存接收到的请求到 logs/awsq_logs/时间_in.log（仅调试模式）
func saveInLog(reqBody []byte, timestamp string) {
	if !logger.IsDebugEnabled() {
		return
	}

	data := make([]byte, len(reqBody))
	copy(data, reqBody)

	go func() {
		logDir := filepath.Join("logs", "awsq_logs")
		if err := os.MkdirAll(logDir, 0755); err != nil {
			logger.Error("创建日志目录失败: %v", err)
			return
		}

		filename := fmt.Sprintf("%s_in.log", timestamp)
		filePath := filepath.Join(logDir, filename)

		var prettyJSON []byte
		var buf map[string]interface{}
		if json.Unmarshal(data, &buf) == nil {
			prettyJSON, _ = json.MarshalIndent(buf, "", "  ")
		} else {
			prettyJSON = data
		}

		if err := os.WriteFile(filePath, prettyJSON, 0644); err != nil {
			logger.Error("保存in日志失败: %v", err)
			return
		}

		logger.Debug("请求体已保存到: %s", filePath)
	}()
}

// handleGetOnlineStats 获取在线用户统计
// @author ygw
func (s *Server) handleGetOnlineStats(c *gin.Context) {
	count := s.onlineTracker.GetOnlineCount()
	c.JSON(200, gin.H{
		"online_count": count,
		"timestamp":    time.Now().Unix(),
	})
}

// handleAwsLatency 检测服务器到 AWS 的网络延迟
// 使用 HEAD 请求测量到 Amazon Q 端点的往返时间
// @author ygw
func (s *Server) handleAwsLatency(c *gin.Context) {
	// AWS CodeWhisperer/Amazon Q 端点
	endpoint := "https://codewhisperer.us-east-1.amazonaws.com"
	timeout := 10 * time.Second

	// 创建带超时的 HTTP 客户端
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DisableKeepAlives: true, // 每次都建立新连接，测量真实延迟
		},
	}

	// 测量延迟
	startTime := time.Now()
	req, err := http.NewRequestWithContext(c.Request.Context(), "HEAD", endpoint, nil)
	if err != nil {
		c.JSON(500, gin.H{
			"error":   "创建请求失败",
			"latency": -1,
			"status":  "error",
		})
		return
	}

	resp, err := client.Do(req)
	latency := time.Since(startTime).Milliseconds()

	if err != nil {
		// 超时或网络错误
		c.JSON(200, gin.H{
			"latency": -1,
			"status":  "error",
			"message": "连接超时或网络错误",
		})
		return
	}
	defer resp.Body.Close()

	// 根据延迟判断状态
	var status string
	if latency < 200 {
		status = "good"
	} else if latency < 500 {
		status = "medium"
	} else {
		status = "poor"
	}

	c.JSON(200, gin.H{
		"latency":   latency,
		"status":    status,
		"timestamp": time.Now().Unix(),
	})
}

// ==================== 代理池管理 ====================

// handleListProxies 获取代理列表
// @author ygw
func (s *Server) handleListProxies(c *gin.Context) {
	logger.Info("获取代理列表 - 来源: %s", c.ClientIP())

	proxies, err := s.db.GetProxies(c.Request.Context())
	if err != nil {
		logger.Error("获取代理列表失败: %v", err)
		c.JSON(500, gin.H{"error": "获取代理列表失败"})
		return
	}

	c.JSON(200, gin.H{"proxies": proxies, "total": len(proxies)})
}

// handleCreateProxy 创建代理
// @author ygw
func (s *Server) handleCreateProxy(c *gin.Context) {
	logger.Info("创建代理 - 来源: %s", c.ClientIP())

	var req models.ProxyCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warn("创建代理失败 - 无效的请求格式: %v", err)
		c.JSON(400, gin.H{"error": "无效的请求格式"})
		return
	}

	// 验证代理 URL 格式
	if err := proxypool.ValidateProxyURL(req.URL); err != nil {
		logger.Warn("创建代理失败 - URL 格式错误: %v", err)
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	proxy := &models.Proxy{
		URL:     req.URL,
		Name:    req.Name,
		Enabled: true,
		Weight:  1,
	}
	if req.Enabled != nil {
		proxy.Enabled = *req.Enabled
	}
	if req.Weight != nil && *req.Weight > 0 {
		proxy.Weight = *req.Weight
	}

	if err := s.db.CreateProxy(c.Request.Context(), proxy); err != nil {
		logger.Error("创建代理失败: %v", err)
		c.JSON(500, gin.H{"error": "创建代理失败"})
		return
	}

	// 重新加载代理池
	s.reloadProxyPool()

	logger.Info("代理创建成功 - ID: %d, URL: %s", proxy.ID, proxy.URL)
	c.JSON(200, gin.H{"message": "代理创建成功", "proxy": proxy})
}

// handleUpdateProxy 更新代理
// @author ygw
func (s *Server) handleUpdateProxy(c *gin.Context) {
	idStr := c.Param("id")
	var id int64
	fmt.Sscanf(idStr, "%d", &id)

	logger.Info("更新代理 - ID: %d, 来源: %s", id, c.ClientIP())

	var req models.ProxyUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warn("更新代理失败 - 无效的请求格式: %v", err)
		c.JSON(400, gin.H{"error": "无效的请求格式"})
		return
	}

	updates := make(map[string]interface{})
	if req.URL != nil {
		// 验证代理 URL 格式
		if err := proxypool.ValidateProxyURL(*req.URL); err != nil {
			logger.Warn("更新代理失败 - URL 格式错误: %v", err)
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		updates["url"] = *req.URL
	}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if req.Weight != nil {
		updates["weight"] = *req.Weight
	}

	if len(updates) == 0 {
		c.JSON(400, gin.H{"error": "没有要更新的字段"})
		return
	}

	if err := s.db.UpdateProxy(c.Request.Context(), id, updates); err != nil {
		logger.Error("更新代理失败 - ID: %d, 错误: %v", id, err)
		c.JSON(500, gin.H{"error": "更新代理失败"})
		return
	}

	// 重新加载代理池
	s.reloadProxyPool()

	logger.Info("代理更新成功 - ID: %d", id)
	c.JSON(200, gin.H{"message": "代理更新成功"})
}

// handleDeleteProxy 删除代理
// @author ygw
func (s *Server) handleDeleteProxy(c *gin.Context) {
	idStr := c.Param("id")
	var id int64
	fmt.Sscanf(idStr, "%d", &id)

	logger.Info("删除代理 - ID: %d, 来源: %s", id, c.ClientIP())

	if err := s.db.DeleteProxy(c.Request.Context(), id); err != nil {
		logger.Error("删除代理失败 - ID: %d, 错误: %v", id, err)
		c.JSON(500, gin.H{"error": "删除代理失败"})
		return
	}

	// 重新加载代理池
	s.reloadProxyPool()

	logger.Info("代理删除成功 - ID: %d", id)
	c.JSON(200, gin.H{"message": "代理删除成功"})
}

// handleToggleProxy 切换代理启用状态
// @author ygw
func (s *Server) handleToggleProxy(c *gin.Context) {
	idStr := c.Param("id")
	var id int64
	fmt.Sscanf(idStr, "%d", &id)

	logger.Info("切换代理状态 - ID: %d, 来源: %s", id, c.ClientIP())

	// 获取当前代理
	proxy, err := s.db.GetProxyByID(c.Request.Context(), id)
	if err != nil {
		logger.Error("获取代理失败 - ID: %d, 错误: %v", id, err)
		c.JSON(500, gin.H{"error": "获取代理失败"})
		return
	}

	// 切换状态
	newEnabled := !proxy.Enabled
	if err := s.db.UpdateProxy(c.Request.Context(), id, map[string]interface{}{"enabled": newEnabled}); err != nil {
		logger.Error("切换代理状态失败 - ID: %d, 错误: %v", id, err)
		c.JSON(500, gin.H{"error": "切换代理状态失败"})
		return
	}

	// 重新加载代理池
	s.reloadProxyPool()

	logger.Info("代理状态切换成功 - ID: %d, 新状态: %v", id, newEnabled)
	c.JSON(200, gin.H{"message": "代理状态切换成功", "enabled": newEnabled})
}

// proxyOpusRequest 将 opus 模型请求桥接到 localhost:3003 服务（已禁用）
// 支持流式和非流式响应，将 Claude 标准 SSE 格式转换为控制台期望的格式
// func (s *Server) proxyOpusRequest(c *gin.Context, body []byte, isStream bool) {
// 	const opusBackendURL = "http://localhost:3003/v1/messages"
// 	clientIP := c.ClientIP()
//
// 	// 检查是否为控制台模式
// 	consoleMode, _ := c.Get("console_mode")
// 	isConsoleMode := consoleMode != nil && consoleMode.(bool)
//
// 	// 创建代理请求
// 	proxyReq, err := http.NewRequestWithContext(c.Request.Context(), "POST", opusBackendURL, strings.NewReader(string(body)))
// 	if err != nil {
// 		logger.Error("[Opus 桥接] 创建请求失败: %v - 来源: %s", err, clientIP)
// 		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建代理请求失败"})
// 		return
// 	}
//
// 	// 复制原始请求的 headers
// 	for key, values := range c.Request.Header {
// 		for _, value := range values {
// 			proxyReq.Header.Add(key, value)
// 		}
// 	}
// 	// 确保 Content-Type 正确
// 	proxyReq.Header.Set("Content-Type", "application/json")
//
// 	// 发送请求到后端服务（流式响应不设置超时）
// 	client := &http.Client{}
// 	if !isStream {
// 		client.Timeout = 5 * time.Minute
// 	}
// 	resp, err := client.Do(proxyReq)
// 	if err != nil {
// 		logger.Error("[Opus 桥接] 请求后端失败: %v - 来源: %s", err, clientIP)
// 		c.JSON(http.StatusBadGateway, gin.H{"error": "后端服务不可用: " + err.Error()})
// 		return
// 	}
// 	defer resp.Body.Close()
//
// 	logger.Debug("[Opus 桥接] 后端响应状态: %d, Content-Type: %s, 控制台模式: %v - 来源: %s", resp.StatusCode, resp.Header.Get("Content-Type"), isConsoleMode, clientIP)
//
// 	if isStream {
// 		// 流式响应
// 		c.Writer.Header().Set("Content-Type", "text/event-stream")
// 		c.Writer.Header().Set("Cache-Control", "no-cache")
// 		c.Writer.Header().Set("Connection", "keep-alive")
// 		c.Writer.Header().Set("X-Accel-Buffering", "no")
// 		c.Writer.WriteHeader(resp.StatusCode)
//
// 		flusher, ok := c.Writer.(http.Flusher)
// 		if !ok {
// 			logger.Error("[Opus 桥接] 响应不支持 Flusher - 来源: %s", clientIP)
// 			return
// 		}
//
// 		if isConsoleMode {
// 			// 控制台模式：将 Claude 标准 SSE 转换为控制台格式
// 			s.proxyOpusStreamToConsole(c, resp.Body, flusher, clientIP)
// 		} else {
// 			// 非控制台模式：直接透传 Claude 标准 SSE
// 			s.proxyOpusStreamDirect(c, resp.Body, flusher, clientIP)
// 		}
// 		logger.Info("[Opus 桥接] 流式响应完成 - 来源: %s", clientIP)
// 	} else {
// 		// 非流式响应：直接返回 JSON
// 		// 复制响应 headers
// 		for key, values := range resp.Header {
// 			if strings.ToLower(key) == "transfer-encoding" {
// 				continue
// 			}
// 			for _, value := range values {
// 				c.Writer.Header().Add(key, value)
// 			}
// 		}
// 		c.Writer.WriteHeader(resp.StatusCode)
// 		written, err := io.Copy(c.Writer, resp.Body)
// 		if err != nil {
// 			logger.Error("[Opus 桥接] 复制响应失败: %v - 来源: %s", err, clientIP)
// 		}
// 		logger.Info("[Opus 桥接] 非流式响应完成 - 写入: %d 字节 - 来源: %s", written, clientIP)
// 	}
// }

// proxyOpusStreamDirect 直接透传 SSE 数据（用于非控制台客户端）（已禁用）
// func (s *Server) proxyOpusStreamDirect(c *gin.Context, body io.Reader, flusher http.Flusher, clientIP string) {
// 	buf := make([]byte, 4096)
// 	for {
// 		n, err := body.Read(buf)
// 		if n > 0 {
// 			_, writeErr := c.Writer.Write(buf[:n])
// 			if writeErr != nil {
// 				logger.Warn("[Opus 桥接] 写入客户端失败: %v - 来源: %s", writeErr, clientIP)
// 				break
// 			}
// 			flusher.Flush()
// 		}
// 		if err != nil {
// 			if err != io.EOF {
// 				logger.Warn("[Opus 桥接] 读取流失败: %v - 来源: %s", err, clientIP)
// 			}
// 			break
// 		}
// 	}
// }

// proxyOpusStreamToConsole 将 Claude 标准 SSE 转换为控制台格式（已禁用）
// Claude 格式: event: message_start/content_block_start/content_block_delta/message_stop
// 控制台格式: event: meta/thinking_delta/answer_delta/done
// func (s *Server) proxyOpusStreamToConsole(c *gin.Context, body io.Reader, flusher http.Flusher, clientIP string) {
// 	reader := bufio.NewReader(body)
// 	var eventType string
// 	var dataLines []string
// 	metaSent := false
// 	var inputTokens, outputTokens int
//
// 	writeSSE := func(event string, data interface{}) {
// 		jsonData, _ := json.Marshal(data)
// 		fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, string(jsonData))
// 		flusher.Flush()
// 	}
//
// 	for {
// 		line, err := reader.ReadString('\n')
// 		if err != nil {
// 			if err != io.EOF {
// 				logger.Warn("[Opus 桥接] 读取流失败: %v - 来源: %s", err, clientIP)
// 			}
// 			break
// 		}
//
// 		line = strings.TrimRight(line, "\r\n")
//
// 		if strings.HasPrefix(line, "event:") {
// 			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
// 		} else if strings.HasPrefix(line, "data:") {
// 			dataLines = append(dataLines, strings.TrimPrefix(line, "data:"))
// 		} else if line == "" && len(dataLines) > 0 {
// 			// 完整事件接收完成，开始处理
// 			dataStr := strings.TrimSpace(strings.Join(dataLines, "\n"))
// 			dataLines = nil
//
// 			if dataStr == "" {
// 				continue
// 			}
//
// 			var data map[string]interface{}
// 			if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
// 				logger.Warn("[Opus 桥接] 解析 SSE 数据失败: %v - 来源: %s", err, clientIP)
// 				continue
// 			}
//
// 			// 根据事件类型转换
// 			switch eventType {
// 			case "message_start":
// 				// 发送 meta 事件
// 				if !metaSent {
// 					metaSent = true
// 					if msg, ok := data["message"].(map[string]interface{}); ok {
// 						model, _ := msg["model"].(string)
// 						msgID, _ := msg["id"].(string)
// 						// 提取 usage 信息
// 						if usage, ok := msg["usage"].(map[string]interface{}); ok {
// 							if it, ok := usage["input_tokens"].(float64); ok {
// 								inputTokens = int(it)
// 							}
// 						}
// 						writeSSE("meta", map[string]interface{}{
// 							"type":            "meta",
// 							"model":           model,
// 							"conversation_id": msgID,
// 							"input_tokens":    inputTokens,
// 						})
// 					}
// 				}
//
// 			case "content_block_start":
// 				// 检查 block 类型（不需要输出）
// 				if block, ok := data["content_block"].(map[string]interface{}); ok {
// 					blockType, _ := block["type"].(string)
// 					logger.Debug("[Opus 桥接] content_block_start type=%s - 来源: %s", blockType, clientIP)
// 				}
//
// 			case "content_block_delta":
// 				// 转换 delta
// 				if delta, ok := data["delta"].(map[string]interface{}); ok {
// 					deltaType, _ := delta["type"].(string)
// 					switch deltaType {
// 					case "thinking_delta":
// 						if thinking, ok := delta["thinking"].(string); ok && thinking != "" {
// 							writeSSE("thinking_delta", map[string]interface{}{
// 								"type":     "thinking_delta",
// 								"thinking": thinking,
// 							})
// 						}
// 					case "text_delta":
// 						if text, ok := delta["text"].(string); ok && text != "" {
// 							writeSSE("answer_delta", map[string]interface{}{
// 								"type": "answer_delta",
// 								"text": text,
// 							})
// 						}
// 					}
// 				}
//
// 			case "message_delta":
// 				// 提取 output_tokens
// 				if usage, ok := data["usage"].(map[string]interface{}); ok {
// 					if ot, ok := usage["output_tokens"].(float64); ok {
// 						outputTokens = int(ot)
// 					}
// 				}
//
// 			case "message_stop":
// 				// 发送 done 事件
// 				writeSSE("done", map[string]interface{}{
// 					"type":          "done",
// 					"finish_reason": "stop",
// 					"usage": map[string]int{
// 						"input_tokens":  inputTokens,
// 						"output_tokens": outputTokens,
// 					},
// 				})
// 			}
// 		}
// 	}
// }

