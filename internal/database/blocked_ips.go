package database

import (
	"context"
	"claude-api/internal/models"

	"gorm.io/gorm"
)

// IsIPBlocked 检查IP是否被封禁
func (db *DB) IsIPBlocked(ctx context.Context, ip string) (bool, error) {
	var count int64
	if err := db.gorm.WithContext(ctx).Model(&models.BlockedIP{}).Where("ip = ?", ip).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// BlockIP 封禁IP
func (db *DB) BlockIP(ctx context.Context, ip, reason string) error {
	blockedIP := models.BlockedIP{
		IP:        ip,
		Reason:    &reason,
		CreatedAt: models.CurrentTime(),
	}

	// 使用 FirstOrCreate + 更新，确保正确处理字符串主键
	var existing models.BlockedIP
	result := db.gorm.WithContext(ctx).Where("ip = ?", ip).First(&existing)
	if result.Error == gorm.ErrRecordNotFound {
		// 不存在，创建新记录
		return db.gorm.WithContext(ctx).Create(&blockedIP).Error
	}
	// 已存在，更新原因和时间
	return db.gorm.WithContext(ctx).Model(&existing).Updates(map[string]interface{}{
		"reason":     reason,
		"created_at": blockedIP.CreatedAt,
	}).Error
}

// UnblockIP 解封IP
func (db *DB) UnblockIP(ctx context.Context, ip string) error {
	return db.gorm.WithContext(ctx).Where("ip = ?", ip).Delete(&models.BlockedIP{}).Error
}

// GetBlockedIPs 获取所有被封禁的IP
func (db *DB) GetBlockedIPs(ctx context.Context) ([]*models.BlockedIP, error) {
	var ips []*models.BlockedIP
	if err := db.gorm.WithContext(ctx).Order("created_at DESC").Find(&ips).Error; err != nil {
		return nil, err
	}
	return ips, nil
}

// backupBlockedIPs 备份封禁IP（内部方法）
func (db *DB) backupBlockedIPs(ctx context.Context) ([]map[string]interface{}, error) {
	var blockedIPs []*models.BlockedIP
	if err := db.gorm.WithContext(ctx).Find(&blockedIPs).Error; err != nil {
		return nil, err
	}

	var result []map[string]interface{}
	for _, ip := range blockedIPs {
		item := map[string]interface{}{
			"ip":         ip.IP,
			"created_at": ip.CreatedAt,
		}
		if ip.Reason != nil {
			item["reason"] = *ip.Reason
		}
		result = append(result, item)
	}
	return result, nil
}

// backupUsers 备份用户（内部方法）
func (db *DB) backupUsers(ctx context.Context) ([]map[string]interface{}, error) {
	var users []*models.User
	if err := db.gorm.WithContext(ctx).Find(&users).Error; err != nil {
		return nil, err
	}

	var result []map[string]interface{}
	for _, user := range users {
		item := map[string]interface{}{
			"id":                user.ID,
			"name":              user.Name,
			"api_key":           user.APIKey,
			"created_at":        user.CreatedAt,
			"updated_at":        user.UpdatedAt,
			"enabled":           user.Enabled,
			"daily_quota":       user.DailyQuota,
			"monthly_quota":     user.MonthlyQuota,
			"total_tokens_used": user.TotalTokensUsed,
		}
		if user.Email != nil {
			item["email"] = *user.Email
		}
		if user.LastResetDaily != nil {
			item["last_reset_daily"] = *user.LastResetDaily
		}
		if user.LastResetMonthly != nil {
			item["last_reset_monthly"] = *user.LastResetMonthly
		}
		if user.Notes != nil {
			item["notes"] = *user.Notes
		}
		result = append(result, item)
	}
	return result, nil
}

// backupUserTokenUsage 备份用户Token使用记录（内部方法）
func (db *DB) backupUserTokenUsage(ctx context.Context) ([]map[string]interface{}, error) {
	var usages []*models.UserTokenUsage
	if err := db.gorm.WithContext(ctx).Find(&usages).Error; err != nil {
		return nil, err
	}

	var result []map[string]interface{}
	for _, usage := range usages {
		result = append(result, map[string]interface{}{
			"id":            usage.ID,
			"user_id":       usage.UserID,
			"date":          usage.Date,
			"input_tokens":  usage.InputTokens,
			"output_tokens": usage.OutputTokens,
			"total_tokens":  usage.TotalTokens,
			"request_count": usage.RequestCount,
		})
	}
	return result, nil
}

// backupAllSettings 完整备份所有设置项（内部方法）
func (db *DB) backupAllSettings(ctx context.Context) (map[string]interface{}, error) {
	var settings []models.Setting
	if err := db.gorm.WithContext(ctx).Find(&settings).Error; err != nil {
		return nil, err
	}

	result := make(map[string]interface{})
	for _, s := range settings {
		// 跳过 schema_version，恢复时会自动处理
		if s.Key == "schema_version" {
			continue
		}
		result[s.Key] = s.Value
	}
	return result, nil
}

// backupImportedAccounts 备份导入账号记录（内部方法）
func (db *DB) backupImportedAccounts(ctx context.Context) ([]map[string]interface{}, error) {
	// 检查表是否存在
	exists, err := db.tableExists(ctx, "imported_accounts")
	if err != nil || !exists {
		return []map[string]interface{}{}, nil
	}

	var accounts []*models.ImportedAccount
	if err := db.gorm.WithContext(ctx).Find(&accounts).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return []map[string]interface{}{}, nil
		}
		return nil, err
	}

	var result []map[string]interface{}
	for _, acc := range accounts {
		item := map[string]interface{}{
			"id":                     acc.ID,
			"original_refresh_token": acc.OriginalRefreshToken,
			"usage_current":          acc.UsageCurrent,
			"usage_limit":            acc.UsageLimit,
			"imported_at":            acc.ImportedAt,
			"import_source":          acc.ImportSource,
		}
		if acc.Email != nil {
			item["email"] = *acc.Email
		}
		if acc.QUserID != nil {
			item["q_user_id"] = *acc.QUserID
		}
		if acc.AccessToken != nil {
			item["access_token"] = *acc.AccessToken
		}
		if acc.NewRefreshToken != nil {
			item["new_refresh_token"] = *acc.NewRefreshToken
		}
		if acc.SubscriptionType != nil {
			item["subscription_type"] = *acc.SubscriptionType
		}
		if acc.SubscriptionTitle != nil {
			item["subscription_title"] = *acc.SubscriptionTitle
		}
		if acc.AccountID != nil {
			item["account_id"] = *acc.AccountID
		}
		if acc.RawResponse != nil {
			item["raw_response"] = *acc.RawResponse
		}
		result = append(result, item)
	}
	return result, nil
}

