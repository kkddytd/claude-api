package api

import (
	"claude-api/internal/auth"
	"claude-api/internal/logger"
	"claude-api/internal/models"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// handleCreateUser 创建新用户
func (s *Server) handleCreateUser(c *gin.Context) {
	logger.Info("创建新用户 - 请求来源: %s", c.ClientIP())

	var req models.UserCreate
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warn("创建用户失败 - 请求格式错误: %v", err)
		c.JSON(400, gin.H{"error": "请求格式错误: " + err.Error()})
		return
	}

	// 生成 API key
	apiKey, err := auth.GenerateAPIKey()
	if err != nil {
		logger.Error("生成 API key 失败: %v", err)
		c.JSON(500, gin.H{"error": "生成 API key ��败"})
		return
	}

	// 设置默认值
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	dailyQuota := 0
	if req.DailyQuota != nil {
		dailyQuota = *req.DailyQuota
	}

	monthlyQuota := 0
	if req.MonthlyQuota != nil {
		monthlyQuota = *req.MonthlyQuota
	}

	// 请求次数限制 @author ygw
	requestQuota := 0
	if req.RequestQuota != nil {
		requestQuota = *req.RequestQuota
	}

	// 每分钟请求频率限制 @author ygw
	rateLimitRPM := 0
	if req.RateLimitRPM != nil {
		rateLimitRPM = *req.RateLimitRPM
	}

	// VIP用户标识 @author ygw
	isVip := false
	if req.IsVip != nil {
		isVip = *req.IsVip
	}

	// 创建用户对象
	user := &models.User{
		ID:              uuid.New().String(),
		Name:            req.Name,
		Email:           req.Email,
		APIKey:          apiKey,
		CreatedAt:       time.Now().Format(time.RFC3339),
		UpdatedAt:       time.Now().Format(time.RFC3339),
		Enabled:         enabled,
		IsVip:           isVip,
		DailyQuota:      dailyQuota,
		MonthlyQuota:    monthlyQuota,
		RequestQuota:    requestQuota,
		RateLimitRPM:    rateLimitRPM,
		TotalTokensUsed: 0,
		TotalRequests:   0,
		TotalCostUSD:    0,
		Notes:           req.Notes,
	}

	// 保存到数据库
	if err := s.db.CreateUser(c.Request.Context(), user); err != nil {
		logger.Error("创建用户失败: %v", err)
		c.JSON(500, gin.H{"error": "创建用户失败"})
		return
	}

	logger.Info("用户已创建: %s (%s) - API Key: %s", user.Name, user.ID, auth.GetAPIKeyPrefix(user.APIKey))
	c.JSON(200, user)
}

// handleListUsers 列出所有用户（包含当日请求数）
func (s *Server) handleListUsers(c *gin.Context) {
	logger.Debug("获取用户列表 - 请求来源: %s", c.ClientIP())

	// 可选的过滤参数
	var enabled *bool
	if enabledStr := c.Query("enabled"); enabledStr != "" {
		e := enabledStr == "true" || enabledStr == "1"
		enabled = &e
	}

	users, err := s.db.ListUsers(c.Request.Context(), enabled)
	if err != nil {
		logger.Error("获取用户列表失败: %v", err)
		c.JSON(500, gin.H{"error": "获取用户列表失败"})
		return
	}

	if users == nil {
		users = []*models.User{}
	}

	// 获取所有用户的当日请求数
	today := time.Now().Format("2006-01-02")
	dailyRequestsMap := s.db.GetUsersDailyRequests(c.Request.Context(), today)

	// 构造返回结果（包含当日请求数）
	type UserWithDailyRequests struct {
		*models.User
		DailyRequests int64 `json:"daily_requests"`
	}

	result := make([]UserWithDailyRequests, 0, len(users))
	for _, u := range users {
		// 隐藏 API Key，只返回掩码
		u.APIKey = "****"
		dailyReqs := int64(0)
		if count, ok := dailyRequestsMap[u.ID]; ok {
			dailyReqs = count
		}
		result = append(result, UserWithDailyRequests{
			User:          u,
			DailyRequests: dailyReqs,
		})
	}

	logger.Debug("返回 %d 个用户", len(result))
	c.JSON(200, result)
}

// handleBatchCreateVIPUsers 批量创建VIP用户
// 默认创建10个VIP用户，用户名为"待分配"，每日请求限制1000次，频率限制5次/分钟
// @author ygw
func (s *Server) handleBatchCreateVIPUsers(c *gin.Context) {
	logger.Info("批量创建VIP用户 - 请求来源: %s", c.ClientIP())

	// 可选参数：创建数量，默认10
	count := 10
	if countStr := c.Query("count"); countStr != "" {
		if n, err := strconv.Atoi(countStr); err == nil && n > 0 && n <= 100 {
			count = n
		}
	}

	createdUsers := make([]*models.User, 0, count)
	for i := 0; i < count; i++ {
		// 生成 API key
		apiKey, err := auth.GenerateAPIKey()
		if err != nil {
			logger.Error("生成 API key 失败: %v", err)
			continue
		}

		// 创建VIP用户对象
		user := &models.User{
			ID:              uuid.New().String(),
			Name:            "待分配",
			APIKey:          apiKey,
			CreatedAt:       time.Now().Format(time.RFC3339),
			UpdatedAt:       time.Now().Format(time.RFC3339),
			Enabled:         true,
			IsVip:           true,          // VIP用户
			DailyQuota:      0,             // Token不限制
			MonthlyQuota:    0,             // Token不限制
			RequestQuota:    1000,          // 每日请求限制1000次
			RateLimitRPM:    10,            // 频率限制10次/分钟 @author ygw
			TotalTokensUsed: 0,
			TotalRequests:   0,
			TotalCostUSD:    0,
		}

		// 保存到数据库
		if err := s.db.CreateUser(c.Request.Context(), user); err != nil {
			logger.Error("创建VIP用户失败: %v", err)
			continue
		}

		createdUsers = append(createdUsers, user)
		logger.Info("VIP用户已创建: %s (%s)", user.Name, user.ID)
	}

	logger.Info("批量创建VIP用户完成 - 成功: %d/%d", len(createdUsers), count)
	c.JSON(200, gin.H{
		"success": true,
		"count":   len(createdUsers),
		"users":   createdUsers,
		"message": "批量创建VIP用户成功",
	})
}

// handleGetUser 获取单个用户信息
func (s *Server) handleGetUser(c *gin.Context) {
	userID := c.Param("id")
	logger.Debug("获取用户信息 - 用户ID: %s - 请求来源: %s", userID, c.ClientIP())

	user, err := s.db.GetUser(c.Request.Context(), userID)
	if err != nil {
		logger.Warn("获取用户失败: %v", err)
		c.JSON(404, gin.H{"error": "用户不存在"})
		return
	}

	// 隐藏 API Key
	user.APIKey = "****"
	c.JSON(200, user)
}

// handleUpdateUser 更新用户信息
func (s *Server) handleUpdateUser(c *gin.Context) {
	userID := c.Param("id")
	logger.Info("更新用户信息 - 用户ID: %s - 请求来源: %s", userID, c.ClientIP())

	var req models.UserUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Warn("更新用户失败 - 请求格式错误: %v", err)
		c.JSON(400, gin.H{"error": "请求格式错误: " + err.Error()})
		return
	}

	// 更新用户
	if err := s.db.UpdateUser(c.Request.Context(), userID, &req); err != nil {
		logger.Error("更新用户失败: %v", err)
		c.JSON(500, gin.H{"error": "更新用户失败"})
		return
	}

	// 获取更新后的用户
	user, err := s.db.GetUser(c.Request.Context(), userID)
	if err != nil {
		logger.Error("获取更新后的用户失败: %v", err)
		c.JSON(500, gin.H{"error": "获取用户失败"})
		return
	}

	logger.Info("用户已更新: %s (%s)", user.Name, user.ID)
	c.JSON(200, user)
}

// handleDeleteUser 删除用户
func (s *Server) handleDeleteUser(c *gin.Context) {
	userID := c.Param("id")
	logger.Info("删除用户 - 用户ID: %s - 请求来源: %s", userID, c.ClientIP())

	if err := s.db.DeleteUser(c.Request.Context(), userID); err != nil {
		logger.Error("删除用户失败: %v", err)
		c.JSON(500, gin.H{"error": "删除用户失败"})
		return
	}

	logger.Info("用户已删除: %s", userID)
	c.JSON(200, gin.H{"message": "用户已删除"})
}

// handleRegenerateAPIKey 重新生成用户的 API key
func (s *Server) handleRegenerateAPIKey(c *gin.Context) {
	userID := c.Param("id")
	logger.Info("重新生成 API key - 用户ID: %s - 请求来源: %s", userID, c.ClientIP())

	// 生成新的 API key
	newAPIKey, err := auth.GenerateAPIKey()
	if err != nil {
		logger.Error("生成 API key 失败: %v", err)
		c.JSON(500, gin.H{"error": "生成 API key 失败"})
		return
	}

	// 更新数据库
	if err := s.db.RegenerateAPIKey(c.Request.Context(), userID, newAPIKey); err != nil {
		logger.Error("更新 API key 失败: %v", err)
		c.JSON(500, gin.H{"error": "更新 API key 失败"})
		return
	}

	logger.Info("API key 已重新生成 - 用户ID: %s - 新Key前缀: %s", userID, auth.GetAPIKeyPrefix(newAPIKey))
	c.JSON(200, gin.H{"api_key": newAPIKey})
}

// handleGetUserStats 获取用户统计信息
func (s *Server) handleGetUserStats(c *gin.Context) {
	userID := c.Param("id")
	logger.Debug("获取用户统计 - 用户ID: %s - 请求来源: %s", userID, c.ClientIP())

	// 获取天数参数���默认30天）
	days := 30
	if daysStr := c.Query("days"); daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
			days = d
		}
	}

	stats, err := s.db.GetUserStats(c.Request.Context(), userID, days)
	if err != nil {
		logger.Error("获取用户统计失败: %v", err)
		c.JSON(500, gin.H{"error": "获取用户统计失败"})
		return
	}


	// 计算美元成本
	// @author ygw
	stats.InputCostUSD, stats.OutputCostUSD, stats.TotalCostUSD = models.CalculateTokenCost(stats.InputTokens, stats.OutputTokens)

	// 计算本月成本（从 daily_usage 中统计本月数据）
	var monthlyInputTokens, monthlyOutputTokens int64
	thisMonth := time.Now().Format("2006-01")
	for _, usage := range stats.DailyUsage {
		if len(usage.Date) >= 7 && usage.Date[:7] == thisMonth {
			monthlyInputTokens += usage.InputTokens
			monthlyOutputTokens += usage.OutputTokens
		}
	}
	_, _, stats.MonthlyCostUSD = models.CalculateTokenCost(monthlyInputTokens, monthlyOutputTokens)
	logger.Debug("返回用户统计 - 用户: %s - 总请求数: %d - 总Token: %d - 总消费: $%.4f", stats.UserName, stats.TotalRequests, stats.TotalTokens, stats.TotalCostUSD)
	c.JSON(200, stats)
}

// handleGetUserIPs 获取用户关联的 IP 列表
// @author ygw
func (s *Server) handleGetUserIPs(c *gin.Context) {
	userID := c.Param("id")
	logger.Debug("获取用户 IP 列表 - 用户ID: %s - 请求来源: %s", userID, c.ClientIP())

	ips, err := s.db.GetIPsByUserID(c.Request.Context(), userID)
	if err != nil {
		logger.Error("获取用户 IP 列表失败: %v", err)
		c.JSON(500, gin.H{"error": "获取用户 IP 列表失败"})
		return
	}

	c.JSON(200, gin.H{
		"ips":   ips,
		"count": len(ips),
	})
}

// handleGetAllUsersStats 获取所有用户的统计概览
func (s *Server) handleGetAllUsersStats(c *gin.Context) {
	logger.Debug("获取所有用户统计 - 请求来源: %s", c.ClientIP())

	// 获取所有用户
	users, err := s.db.ListUsers(c.Request.Context(), nil)
	if err != nil {
		logger.Error("获取用户列表失败: %v", err)
		c.JSON(500, gin.H{"error": "获取用户列表失败"})
		return
	}

	// 为每个用户获取统计信息
	type UserStatsOverview struct {
		UserID       string `json:"user_id"`
		UserName     string `json:"user_name"`
		Enabled      bool   `json:"enabled"`
		TotalTokens  int64  `json:"total_tokens"`
		MonthlyTotal int64  `json:"monthly_total"`
		DailyQuota   int    `json:"daily_quota"`
		MonthlyQuota int    `json:"monthly_quota"`
	}

	var allStats []UserStatsOverview
	thisMonth := time.Now().Format("2006-01")

	for _, user := range users {
		// 获取本月使用量
		monthlyTotal, err := s.db.GetMonthlyUsage(c.Request.Context(), user.ID, thisMonth)
		if err != nil {
			logger.Warn("获取用户月度使用量失败 - 用户ID: %s - 错误: %v", user.ID, err)
			monthlyTotal = 0
		}

		allStats = append(allStats, UserStatsOverview{
			UserID:       user.ID,
			UserName:     user.Name,
			Enabled:      user.Enabled,
			TotalTokens:  user.TotalTokensUsed,
			MonthlyTotal: monthlyTotal,
			DailyQuota:   user.DailyQuota,
			MonthlyQuota: user.MonthlyQuota,
		})
	}

	logger.Debug("返回 %d 个用户的统计信息", len(allStats))
	c.JSON(200, allStats)
}
