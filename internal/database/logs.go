package database

import (
	"context"
	"claude-api/internal/logger"
	"claude-api/internal/models"
	"time"

	"gorm.io/gorm"
)

// CreateRequestLog 创建请求日志
func (db *DB) CreateRequestLog(ctx context.Context, log *models.RequestLog) error {
	return db.gorm.WithContext(ctx).Create(log).Error
}

// BatchCreateRequestLogs 批量写入请求日志（使用事务，大幅提升写入性能）
func (db *DB) BatchCreateRequestLogs(ctx context.Context, logs []*models.RequestLog) error {
	if len(logs) == 0 {
		return nil
	}

	return db.gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, log := range logs {
			if err := tx.Create(log).Error; err != nil {
				logger.Debug("批量写入日志失败（单条）: %v", err)
				// 继续处理其他日志，不因单条失败而中断
			}
		}
		return nil
	})
}

// GetRequestLogs 查询请求日志
func (db *DB) GetRequestLogs(ctx context.Context, filters *models.LogFilters, limit, offset int) ([]*models.RequestLog, error) {
	query := db.gorm.WithContext(ctx).Model(&models.RequestLog{})

	query = applyLogFiltersGorm(query, filters)
	query = query.Order("timestamp DESC").Limit(limit).Offset(offset)

	var logs []*models.RequestLog
	if err := query.Find(&logs).Error; err != nil {
		return nil, err
	}

	return logs, nil
}

// applyLogFiltersGorm 应用日志过滤条件到 GORM 查询
func applyLogFiltersGorm(query *gorm.DB, filters *models.LogFilters) *gorm.DB {
	if filters == nil {
		return query
	}

	if filters.StartTime != nil {
		query = query.Where("timestamp >= ?", *filters.StartTime)
	}
	if filters.EndTime != nil {
		query = query.Where("timestamp <= ?", *filters.EndTime)
	}
	if filters.ClientIP != nil {
		query = query.Where("client_ip = ?", *filters.ClientIP)
	}
	if filters.AccountID != nil {
		query = query.Where("account_id = ?", *filters.AccountID)
	}
	if filters.EndpointType != nil {
		query = query.Where("endpoint_type = ?", *filters.EndpointType)
	}
	if filters.IsSuccess != nil {
		query = query.Where("is_success = ?", *filters.IsSuccess)
	}

	return query
}

// GetRequestLogsCount 获取请求日志总数
func (db *DB) GetRequestLogsCount(ctx context.Context, filters *models.LogFilters) (int64, error) {
	query := db.gorm.WithContext(ctx).Model(&models.RequestLog{})
	query = applyLogFiltersGorm(query, filters)

	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// GetRequestStats 获取请求统计（旧版，无筛选条件）
func (db *DB) GetRequestStats(ctx context.Context, startTime, endTime string) (*models.RequestStats, error) {
	return db.GetRequestStatsWithFilters(ctx, &models.LogFilters{
		StartTime: &startTime,
		EndTime:   &endTime,
	})
}

// GetRequestStatsWithFilters 获取请求统计（支持完整筛选条件）@author ygw
func (db *DB) GetRequestStatsWithFilters(ctx context.Context, filters *models.LogFilters) (*models.RequestStats, error) {
	stats := &models.RequestStats{}

	// 基本统计
	type BasicStats struct {
		TotalRequests     int64
		SuccessRequests   int64
		FailedRequests    int64
		TotalInputTokens  int64
		TotalOutputTokens int64
		AvgDurationMs     float64
	}
	var basicStats BasicStats

	query := db.gorm.WithContext(ctx).Model(&models.RequestLog{}).
		Select(`COUNT(*) as total_requests,
			COALESCE(SUM(CASE WHEN is_success = true THEN 1 ELSE 0 END), 0) as success_requests,
			COALESCE(SUM(CASE WHEN is_success = false THEN 1 ELSE 0 END), 0) as failed_requests,
			COALESCE(SUM(input_tokens), 0) as total_input_tokens,
			COALESCE(SUM(output_tokens), 0) as total_output_tokens,
			COALESCE(AVG(duration_ms), 0) as avg_duration_ms`)
	query = applyLogFiltersGorm(query, filters)
	query.Scan(&basicStats)

	stats.TotalRequests = basicStats.TotalRequests
	stats.SuccessRequests = basicStats.SuccessRequests
	stats.FailedRequests = basicStats.FailedRequests
	stats.TotalInputTokens = basicStats.TotalInputTokens
	stats.TotalOutputTokens = basicStats.TotalOutputTokens
	stats.AvgDurationMs = basicStats.AvgDurationMs

	if stats.TotalRequests > 0 {
		stats.SuccessRate = float64(stats.SuccessRequests) / float64(stats.TotalRequests) * 100
	}

	// Top IPs
	var topIPs []models.IPStat
	queryIPs := db.gorm.WithContext(ctx).Model(&models.RequestLog{}).
		Select("client_ip as ip, COUNT(*) as request_count")
	queryIPs = applyLogFiltersGorm(queryIPs, filters)
	queryIPs.Group("client_ip").
		Order("request_count DESC").
		Limit(10).
		Scan(&topIPs)
	stats.TopIPs = topIPs

	// Top Accounts
	var topAccounts []models.AccountStat
	queryAccounts := db.gorm.WithContext(ctx).Model(&models.RequestLog{}).
		Select("account_id, COUNT(*) as request_count, SUM(input_tokens + output_tokens) as total_tokens").
		Where("account_id IS NOT NULL")
	queryAccounts = applyLogFiltersGorm(queryAccounts, filters)
	queryAccounts.Group("account_id").
		Order("request_count DESC").
		Limit(10).
		Scan(&topAccounts)
	stats.TopAccounts = topAccounts

	return stats, nil
}

// CheckIPRateLimit 检查IP频率限制
func (db *DB) CheckIPRateLimit(ctx context.Context, ip string, windowMinutes int, maxRequests int) (bool, error) {
	var count int64

	// 计算时间窗口
	windowStart := time.Now().Add(-time.Duration(windowMinutes) * time.Minute).Format(models.TimeFormat)

	if err := db.gorm.WithContext(ctx).Model(&models.RequestLog{}).
		Where("client_ip = ? AND timestamp >= ?", ip, windowStart).
		Count(&count).Error; err != nil {
		return false, err
	}

	return count < int64(maxRequests), nil
}

// GetTotalRequestCount 获取总请求次数
func (db *DB) GetTotalRequestCount(ctx context.Context) (int64, error) {
	var count int64
	if err := db.gorm.WithContext(ctx).Model(&models.RequestLog{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// DeleteAllRequestLogs 删除所有请求日志
func (db *DB) DeleteAllRequestLogs(ctx context.Context) error {
	logger.Debug("数据库: 删除所有请求日志")

	if err := db.gorm.WithContext(ctx).Where("1 = 1").Delete(&models.RequestLog{}).Error; err != nil {
		logger.Debug("数据库: 删除请求日志失败: %v", err)
		return err
	}

	logger.Debug("数据库: 所有请求日志已删除")
	return nil
}

// CleanupOldLogs 清理旧日志
func (db *DB) CleanupOldLogs(ctx context.Context, daysToKeep int) (int64, error) {
	cutoffTime := time.Now().AddDate(0, 0, -daysToKeep).Format(models.TimeFormat)

	result := db.gorm.WithContext(ctx).Where("timestamp < ?", cutoffTime).Delete(&models.RequestLog{})
	if result.Error != nil {
		return 0, result.Error
	}

	return result.RowsAffected, nil
}

// GetVisitorIPs 获取所有访问IP列表（去重，按最近访问时间排序）
// @author ygw - 增加用户关联和时间段统计
func (db *DB) GetVisitorIPs(ctx context.Context) ([]models.VisitorIP, error) {
	var ips []models.VisitorIP

	// 获取时间范围
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour).Format(models.TimeFormat)
	oneDayAgo := now.Add(-24 * time.Hour).Format(models.TimeFormat)

	// 构建查询 - 包含用户信息和时间段统计
	err := db.gorm.WithContext(ctx).Model(&models.RequestLog{}).
		Select(`client_ip as ip,
			MAX(timestamp) as last_visit,
			COUNT(*) as request_count,
			SUM(CASE WHEN is_success = true THEN 1 ELSE 0 END) as success_count,
			SUM(CASE WHEN timestamp >= ? THEN 1 ELSE 0 END) as request_count_day,
			SUM(CASE WHEN timestamp >= ? THEN 1 ELSE 0 END) as request_count_hour,
			MAX(user_id) as user_id`, oneDayAgo, oneHourAgo).
		Group("client_ip").
		Order("last_visit DESC").
		Limit(500).
		Scan(&ips).Error

	if err != nil {
		return nil, err
	}

	// 批量查询用户名
	userIDs := make([]string, 0)
	ipList := make([]string, 0, len(ips))
	for _, ip := range ips {
		ipList = append(ipList, ip.IP)
		if ip.UserID != nil && *ip.UserID != "" {
			userIDs = append(userIDs, *ip.UserID)
		}
	}

	if len(userIDs) > 0 {
		userMap := make(map[string]string)
		var users []struct {
			ID   string
			Name string
		}
		db.gorm.WithContext(ctx).Table("users").Select("id, name").Where("id IN ?", userIDs).Scan(&users)
		for _, u := range users {
			userMap[u.ID] = u.Name
		}

		// 填充用户名
		for i := range ips {
			if ips[i].UserID != nil {
				if name, ok := userMap[*ips[i].UserID]; ok {
					ips[i].UserName = &name
				}
			}
		}
	}

	// 批量查询IP配置（备注、限制等）
	if len(ipList) > 0 {
		ipConfigs, err := db.GetIPConfigs(ctx, ipList)
		if err == nil && ipConfigs != nil {
			for i := range ips {
				if config, ok := ipConfigs[ips[i].IP]; ok && config != nil {
					ips[i].Notes = config.Notes
					ips[i].RateLimitRPM = config.RateLimitRPM
					ips[i].DailyRequestLimit = config.DailyRequestLimit
				}
			}
		}
	}

	return ips, nil
}

// GetIPsByUserID 获取指定用户的所有访问 IP
// @author ygw
func (db *DB) GetIPsByUserID(ctx context.Context, userID string) ([]models.VisitorIP, error) {
	var ips []models.VisitorIP

	// 获取时间范围
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour).Format(models.TimeFormat)
	oneDayAgo := now.Add(-24 * time.Hour).Format(models.TimeFormat)

	err := db.gorm.WithContext(ctx).Model(&models.RequestLog{}).
		Select(`client_ip as ip,
			MAX(timestamp) as last_visit,
			COUNT(*) as request_count,
			SUM(CASE WHEN is_success = true THEN 1 ELSE 0 END) as success_count,
			SUM(CASE WHEN timestamp >= ? THEN 1 ELSE 0 END) as request_count_day,
			SUM(CASE WHEN timestamp >= ? THEN 1 ELSE 0 END) as request_count_hour`, oneDayAgo, oneHourAgo).
		Where("user_id = ?", userID).
		Group("client_ip").
		Order("last_visit DESC").
		Scan(&ips).Error

	if err != nil {
		return nil, err
	}

	return ips, nil
}

// ==================== IP配置管理 ====================

// GetIPConfig 获取单个IP的配置
func (db *DB) GetIPConfig(ctx context.Context, ip string) (*models.IPConfig, error) {
	var config models.IPConfig
	err := db.gorm.WithContext(ctx).Where("ip = ?", ip).First(&config).Error
	if err != nil {
		if err.Error() == "record not found" {
			return nil, nil
		}
		return nil, err
	}
	return &config, nil
}

// GetIPConfigs 批量获取IP配置
func (db *DB) GetIPConfigs(ctx context.Context, ips []string) (map[string]*models.IPConfig, error) {
	var configs []models.IPConfig
	err := db.gorm.WithContext(ctx).Where("ip IN ?", ips).Find(&configs).Error
	if err != nil {
		return nil, err
	}

	result := make(map[string]*models.IPConfig)
	for i := range configs {
		result[configs[i].IP] = &configs[i]
	}
	return result, nil
}

// UpsertIPConfig 创建或更新IP配置
func (db *DB) UpsertIPConfig(ctx context.Context, ip string, updates *models.IPConfigUpdate) (*models.IPConfig, error) {
	now := time.Now().Format(models.TimeFormat)

	// 先尝试获取现有配置
	existing, err := db.GetIPConfig(ctx, ip)
	if err != nil {
		return nil, err
	}

	if existing == nil {
		// 创建新配置
		config := &models.IPConfig{
			IP:        ip,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if updates.Notes != nil {
			config.Notes = updates.Notes
		}
		if updates.RateLimitRPM != nil {
			config.RateLimitRPM = *updates.RateLimitRPM
		}
		if updates.DailyRequestLimit != nil {
			config.DailyRequestLimit = *updates.DailyRequestLimit
		}
		if err := db.gorm.WithContext(ctx).Create(config).Error; err != nil {
			return nil, err
		}
		return config, nil
	}

	// 更新现有配置
	updateMap := map[string]interface{}{
		"updated_at": now,
	}
	if updates.Notes != nil {
		updateMap["notes"] = updates.Notes
	}
	if updates.RateLimitRPM != nil {
		updateMap["rate_limit_rpm"] = *updates.RateLimitRPM
	}
	if updates.DailyRequestLimit != nil {
		updateMap["daily_request_limit"] = *updates.DailyRequestLimit
	}

	if err := db.gorm.WithContext(ctx).Model(&models.IPConfig{}).Where("ip = ?", ip).Updates(updateMap).Error; err != nil {
		return nil, err
	}

	return db.GetIPConfig(ctx, ip)
}

// DeleteIPConfig 删除IP配置
func (db *DB) DeleteIPConfig(ctx context.Context, ip string) error {
	return db.gorm.WithContext(ctx).Where("ip = ?", ip).Delete(&models.IPConfig{}).Error
}

// CheckIPDailyLimit 检查IP每日请求限制
func (db *DB) CheckIPDailyLimit(ctx context.Context, ip string, limit int) (bool, int64, error) {
	if limit <= 0 {
		return true, 0, nil
	}

	// 获取今日0点时间
	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).Format(models.TimeFormat)

	var count int64
	err := db.gorm.WithContext(ctx).Model(&models.RequestLog{}).
		Where("client_ip = ? AND timestamp >= ?", ip, todayStart).
		Count(&count).Error
	if err != nil {
		return false, 0, err
	}

	return count < int64(limit), count, nil
}

// applyLogFilters 应用日志过滤条件到查询（兼容旧代码）
func applyLogFilters(filters *models.LogFilters) (string, []interface{}) {
	query := ""
	args := []interface{}{}

	if filters == nil {
		return query, args
	}

	if filters.StartTime != nil {
		query += " AND timestamp >= ?"
		args = append(args, *filters.StartTime)
	}
	if filters.EndTime != nil {
		query += " AND timestamp <= ?"
		args = append(args, *filters.EndTime)
	}
	if filters.ClientIP != nil {
		query += " AND client_ip = ?"
		args = append(args, *filters.ClientIP)
	}
	if filters.AccountID != nil {
		query += " AND account_id = ?"
		args = append(args, *filters.AccountID)
	}
	if filters.EndpointType != nil {
		query += " AND endpoint_type = ?"
		args = append(args, *filters.EndpointType)
	}
	if filters.IsSuccess != nil {
		query += " AND is_success = ?"
		args = append(args, boolToInt(*filters.IsSuccess))
	}

	return query, args
}

