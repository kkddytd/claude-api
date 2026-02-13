package database

import (
	"context"
	"fmt"
	"claude-api/internal/logger"
	"claude-api/internal/models"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CreateUser 创建新用户
func (db *DB) CreateUser(ctx context.Context, user *models.User) error {
	logger.Debug("数据库: 创建用户 - ID: %s, 名称: %s", user.ID, user.Name)

	if err := db.gorm.WithContext(ctx).Create(user).Error; err != nil {
		return fmt.Errorf("创建用户失败: %w", err)
	}

	logger.Info("用户已创建: %s (%s)", user.Name, user.ID)
	return nil
}

// GetUserByAPIKey 根据 API Key 获取用户
func (db *DB) GetUserByAPIKey(ctx context.Context, apiKey string) (*models.User, error) {
	var user models.User
	err := db.gorm.WithContext(ctx).Where("api_key = ?", apiKey).First(&user).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}
	return &user, nil
}

// GetUser 根据 ID 获取用户
func (db *DB) GetUser(ctx context.Context, id string) (*models.User, error) {
	var user models.User
	err := db.gorm.WithContext(ctx).Where("id = ?", id).First(&user).Error
	if err == gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("用户不存在: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}
	return &user, nil
}

// ListUsers 列出所有用户（可选过滤启用状态）
func (db *DB) ListUsers(ctx context.Context, enabled *bool) ([]*models.User, error) {
	query := db.gorm.WithContext(ctx).Model(&models.User{}).Order("created_at DESC")

	if enabled != nil {
		query = query.Where("enabled = ?", *enabled)
	}

	var users []*models.User
	if err := query.Find(&users).Error; err != nil {
		return nil, fmt.Errorf("查询用户列表失败: %w", err)
	}

	return users, nil
}

// GetUsersByIDs 批量获取用户（用于日志归属查询，性能优化）
// @author ygw
func (db *DB) GetUsersByIDs(ctx context.Context, ids []string) ([]*models.User, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var users []*models.User
	if err := db.gorm.WithContext(ctx).Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, fmt.Errorf("批量查询用户失败: %w", err)
	}
	return users, nil
}

// UpdateUser 更新用户信息
func (db *DB) UpdateUser(ctx context.Context, id string, updates *models.UserUpdate) error {
	logger.Debug("数据库: 更新用户 - ID: %s", id)

	updateMap := map[string]interface{}{
		"updated_at": currentTime(),
	}

	if updates.Name != nil {
		updateMap["name"] = *updates.Name
	}
	if updates.Email != nil {
		updateMap["email"] = *updates.Email
	}
	if updates.DailyQuota != nil {
		updateMap["daily_quota"] = *updates.DailyQuota
	}
	if updates.MonthlyQuota != nil {
		updateMap["monthly_quota"] = *updates.MonthlyQuota
	}
	if updates.Enabled != nil {
		updateMap["enabled"] = *updates.Enabled
	}
	if updates.Notes != nil {
		updateMap["notes"] = *updates.Notes
	}
	// 请求次数限制 @author ygw
	if updates.RequestQuota != nil {
		updateMap["request_quota"] = *updates.RequestQuota
	}
	// 每分钟请求频率限制 @author ygw
	if updates.RateLimitRPM != nil {
		updateMap["rate_limit_rpm"] = *updates.RateLimitRPM
	}
	// VIP用户标识 @author ygw
	if updates.IsVip != nil {
		updateMap["is_vip"] = *updates.IsVip
	}

	result := db.gorm.WithContext(ctx).Model(&models.User{}).Where("id = ?", id).Updates(updateMap)
	if result.Error != nil {
		return fmt.Errorf("更新用户失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("用户不存在: %s", id)
	}

	logger.Info("用户已更新: %s", id)
	return nil
}

// DeleteUser 删除用户及其关联数据
func (db *DB) DeleteUser(ctx context.Context, id string) error {
	logger.Debug("数据库: 删除用户 - ID: %s", id)

	// 开启事务
	return db.gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 删除用户的 Token 使用记录
		if err := tx.Where("user_id = ?", id).Delete(&models.UserTokenUsage{}).Error; err != nil {
			return fmt.Errorf("删除用户Token使用记录失败: %w", err)
		}

		// 删除用户
		result := tx.Where("id = ?", id).Delete(&models.User{})
		if result.Error != nil {
			return fmt.Errorf("删除用户失败: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("用户不存在: %s", id)
		}

		logger.Info("用户已删除: %s", id)
		return nil
	})
}

// RegenerateAPIKey 重新生成用户的 API Key
func (db *DB) RegenerateAPIKey(ctx context.Context, userID string, newAPIKey string) error {
	logger.Debug("数据库: 重新生成API密钥 - 用户ID: %s", userID)

	result := db.gorm.WithContext(ctx).Model(&models.User{}).Where("id = ?", userID).Updates(map[string]interface{}{
		"api_key":    newAPIKey,
		"updated_at": currentTime(),
	})

	if result.Error != nil {
		return fmt.Errorf("更新API密钥失败: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("用户不存在: %s", userID)
	}

	logger.Info("API密钥已重新生成: 用户ID %s", userID)
	return nil
}

// UpdateTokenUsage 更新用户 Token 使用量（每日跟踪和总量）
func (db *DB) UpdateTokenUsage(ctx context.Context, userID string, inputTokens, outputTokens int) error {
	today := time.Now().Format("2006-01-02")
	totalTokens := inputTokens + outputTokens

	return db.gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 查找今日记录
		var usage models.UserTokenUsage
		err := tx.Where("user_id = ? AND date = ?", userID, today).First(&usage).Error

		if err == gorm.ErrRecordNotFound {
			// 创建新记录
			usage = models.UserTokenUsage{
				ID:           uuid.New().String(),
				UserID:       userID,
				Date:         today,
				InputTokens:  int64(inputTokens),
				OutputTokens: int64(outputTokens),
				TotalTokens:  int64(totalTokens),
				RequestCount: 1,
			}
			if err := tx.Create(&usage).Error; err != nil {
				return fmt.Errorf("创建每日使用量记录失败: %w", err)
			}
		} else if err != nil {
			return fmt.Errorf("查询每日使用量失败: %w", err)
		} else {
			// 更新现有记录
			if err := tx.Model(&usage).Updates(map[string]interface{}{
				"input_tokens":  gorm.Expr("input_tokens + ?", inputTokens),
				"output_tokens": gorm.Expr("output_tokens + ?", outputTokens),
				"total_tokens":  gorm.Expr("total_tokens + ?", totalTokens),
				"request_count": gorm.Expr("request_count + 1"),
			}).Error; err != nil {
				return fmt.Errorf("更新每日使用量失败: %w", err)
			}
		}

		// 更新用户总使用量、总请求次数和总消费金额 @author ygw
		_, _, cost := models.CalculateTokenCost(int64(inputTokens), int64(outputTokens))
		if err := tx.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]interface{}{
			"total_tokens_used": gorm.Expr("total_tokens_used + ?", totalTokens),
			"total_requests":    gorm.Expr("total_requests + 1"),
			"total_cost_usd":    gorm.Expr("total_cost_usd + ?", cost),
		}).Error; err != nil {
			return fmt.Errorf("更新总使用量失败: %w", err)
		}

		return nil
	})
}

// CheckUserQuota 检查用户是否超出每日或月度配额
// 返回: (allowed bool, reason string, error)
// @author ygw - 增加请求次数限制检查
func (db *DB) CheckUserQuota(ctx context.Context, userID string) (bool, string, error) {
	user, err := db.GetUser(ctx, userID)
	if err != nil {
		return false, "", err
	}

	today := time.Now().Format("2006-01-02")
	thisMonth := time.Now().Format("2006-01")

	// 检查每日请求次数限制 @author ygw
	if user.RequestQuota > 0 {
		var dailyRequests int64
		err := db.gorm.WithContext(ctx).Model(&models.UserTokenUsage{}).
			Select("COALESCE(request_count, 0)").
			Where("user_id = ? AND date = ?", userID, today).
			Scan(&dailyRequests).Error

		if err != nil && err != gorm.ErrRecordNotFound {
			return false, "", fmt.Errorf("检查每日请求次数失败: %w", err)
		}

		if dailyRequests >= int64(user.RequestQuota) {
			return false, fmt.Sprintf("每日请求次数已用尽 (%d/%d 次)", dailyRequests, user.RequestQuota), nil
		}
	}

	// 检查每日配额
	if user.DailyQuota > 0 {
		var dailyUsage int64
		err := db.gorm.WithContext(ctx).Model(&models.UserTokenUsage{}).
			Select("COALESCE(total_tokens, 0)").
			Where("user_id = ? AND date = ?", userID, today).
			Scan(&dailyUsage).Error

		if err != nil && err != gorm.ErrRecordNotFound {
			return false, "", fmt.Errorf("检查每日配额失败: %w", err)
		}

		if dailyUsage >= int64(user.DailyQuota) {
			return false, fmt.Sprintf("每日配额已用尽 (%d/%d tokens)", dailyUsage, user.DailyQuota), nil
		}
	}

	// 检查月度配额
	if user.MonthlyQuota > 0 {
		var monthlyUsage int64
		err := db.gorm.WithContext(ctx).Model(&models.UserTokenUsage{}).
			Select("COALESCE(SUM(total_tokens), 0)").
			Where("user_id = ? AND date LIKE ?", userID, thisMonth+"%").
			Scan(&monthlyUsage).Error

		if err != nil && err != gorm.ErrRecordNotFound {
			return false, "", fmt.Errorf("检查月度配额失败: %w", err)
		}

		if monthlyUsage >= int64(user.MonthlyQuota) {
			return false, fmt.Sprintf("月度配额已用尽 (%d/%d tokens)", monthlyUsage, user.MonthlyQuota), nil
		}
	}

	return true, "", nil
}

// GetUserStats 获取用户的综合统计数据
func (db *DB) GetUserStats(ctx context.Context, userID string, days int) (*models.UserStats, error) {
	user, err := db.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	stats := &models.UserStats{
		UserID:       user.ID,
		UserName:     user.Name,
		DailyQuota:   user.DailyQuota,
		MonthlyQuota: user.MonthlyQuota,
	}

	// 获取总请求统计
	type RequestStats struct {
		Count        int64
		InputTokens  int64
		OutputTokens int64
	}
	var reqStats RequestStats
	db.gorm.WithContext(ctx).Model(&models.RequestLog{}).
		Select("COUNT(*) as count, COALESCE(SUM(input_tokens), 0) as input_tokens, COALESCE(SUM(output_tokens), 0) as output_tokens").
		Where("user_id = ?", userID).
		Scan(&reqStats)

	stats.TotalRequests = reqStats.Count
	stats.InputTokens = reqStats.InputTokens
	stats.OutputTokens = reqStats.OutputTokens
	stats.TotalTokens = stats.InputTokens + stats.OutputTokens

	// 获取指定天数的每日使用量
	cutoffDate := time.Now().AddDate(0, 0, -days).Format("2006-01-02")

	var dailyUsage []models.UserTokenUsage
	db.gorm.WithContext(ctx).
		Where("user_id = ? AND date >= ?", userID, cutoffDate).
		Order("date DESC").
		Find(&dailyUsage)

	stats.DailyUsage = dailyUsage

	// 计算月度总量
	thisMonth := time.Now().Format("2006-01")
	var monthlyTotal int64
	db.gorm.WithContext(ctx).Model(&models.UserTokenUsage{}).
		Select("COALESCE(SUM(total_tokens), 0)").
		Where("user_id = ? AND date LIKE ?", userID, thisMonth+"%").
		Scan(&monthlyTotal)

	stats.MonthlyTotal = monthlyTotal

	// 计算剩余配额
	if user.MonthlyQuota > 0 {
		stats.QuotaRemaining = int64(user.MonthlyQuota) - monthlyTotal
		if stats.QuotaRemaining < 0 {
			stats.QuotaRemaining = 0
		}
	} else {
		stats.QuotaRemaining = -1 // -1 表示无限制
	}

	return stats, nil
}

// GetMonthlyUsage 获取用户的月度 Token 使用量
func (db *DB) GetMonthlyUsage(ctx context.Context, userID string, month string) (int64, error) {
	var monthlyTotal int64
	err := db.gorm.WithContext(ctx).Model(&models.UserTokenUsage{}).
		Select("COALESCE(SUM(total_tokens), 0)").
		Where("user_id = ? AND date LIKE ?", userID, month+"%").
		Scan(&monthlyTotal).Error

	if err != nil {
		return 0, fmt.Errorf("获取月度使用量失败: %w", err)
	}

	return monthlyTotal, nil
}

// GetUsersDailyRequests 批量获取所有用户的当日请求数
// 返回 map[userID]requestCount
// @author ygw
func (db *DB) GetUsersDailyRequests(ctx context.Context, date string) map[string]int64 {
	result := make(map[string]int64)

	type DailyCount struct {
		UserID       string
		RequestCount int64
	}

	var counts []DailyCount
	db.gorm.WithContext(ctx).Model(&models.UserTokenUsage{}).
		Select("user_id, COALESCE(request_count, 0) as request_count").
		Where("date = ?", date).
		Scan(&counts)

	for _, c := range counts {
		result[c.UserID] = c.RequestCount
	}

	return result
}
