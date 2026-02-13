package database

import (
	"context"
	"encoding/json"
	"fmt"
	"claude-api/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// BackupData 备份所有数据
func (db *DB) BackupData(ctx context.Context) (map[string]interface{}, error) {
	backup := make(map[string]interface{})

	// 备份账号（转换为 []interface{} 以便恢复时使用）
	accounts, err := db.ListAccounts(ctx, nil, "created_at", false)
	if err != nil {
		return nil, err
	}
	// 转换为 map 格式以便 JSON 序列化和反序列化
	accountsData := make([]map[string]interface{}, len(accounts))
	for i, acc := range accounts {
		accMap := map[string]interface{}{
			"id":                  acc.ID,
			"clientId":            acc.ClientID,
			"clientSecret":        acc.ClientSecret,
			"created_at":          acc.CreatedAt,
			"updated_at":          acc.UpdatedAt,
			"enabled":             acc.Enabled,
			"error_count":         acc.ErrorCount,
			"success_count":       acc.SuccessCount,
		}
		if acc.Label != nil {
			accMap["label"] = *acc.Label
		}
		if acc.RefreshToken != nil {
			accMap["refreshToken"] = *acc.RefreshToken
		}
		if acc.AccessToken != nil {
			accMap["accessToken"] = *acc.AccessToken
		}
		if acc.Other != nil {
			accMap["other"] = acc.Other
		}
		if acc.LastRefreshTime != nil {
			accMap["last_refresh_time"] = *acc.LastRefreshTime
		}
		if acc.LastRefreshStatus != nil {
			accMap["last_refresh_status"] = *acc.LastRefreshStatus
		}
		if acc.QUserID != nil {
			accMap["q_user_id"] = *acc.QUserID
		}
		if acc.Email != nil {
			accMap["email"] = *acc.Email
		}
		if acc.AuthMethod != nil {
			accMap["auth_method"] = *acc.AuthMethod
		}
		if acc.Region != nil {
			accMap["region"] = *acc.Region
		}
		if acc.MachineID != nil {
			accMap["machine_id"] = *acc.MachineID
		}
		accountsData[i] = accMap
	}
	backup["accounts"] = accountsData

	// 备份设置（完整备份所有设置项）
	settings, err := db.backupAllSettings(ctx)
	if err != nil {
		return nil, err
	}
	backup["settings"] = settings

	// 备份封禁IP
	blockedIPs, err := db.backupBlockedIPs(ctx)
	if err != nil {
		return nil, err
	}
	backup["blocked_ips"] = blockedIPs

	// 备份用户
	users, err := db.backupUsers(ctx)
	if err != nil {
		return nil, err
	}
	backup["users"] = users

	// 备份用户Token使用记录
	userTokenUsage, err := db.backupUserTokenUsage(ctx)
	if err != nil {
		return nil, err
	}
	backup["user_token_usage"] = userTokenUsage

	// 备份导入账号记录
	importedAccounts, err := db.backupImportedAccounts(ctx)
	if err != nil {
		return nil, err
	}
	backup["imported_accounts"] = importedAccounts

	// 备份版本信息
	backup["backup_version"] = 2
	backup["backup_time"] = models.CurrentTime()

	return backup, nil
}

// RestoreData 恢复数据（覆盖现有数据）
func (db *DB) RestoreData(ctx context.Context, data map[string]interface{}) error {
	return db.gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 清空现有数据
		if err := tx.Where("1 = 1").Delete(&models.Account{}).Error; err != nil {
			return err
		}
		if err := tx.Where("1 = 1").Delete(&models.Setting{}).Error; err != nil {
			return err
		}
		if err := tx.Where("1 = 1").Delete(&models.BlockedIP{}).Error; err != nil {
			return err
		}
		if err := tx.Where("1 = 1").Delete(&models.User{}).Error; err != nil {
			return err
		}
		if err := tx.Where("1 = 1").Delete(&models.UserTokenUsage{}).Error; err != nil {
			return err
		}
		// 清空导入账号表
		tx.Where("1 = 1").Delete(&models.ImportedAccount{})

		// 恢复账号（支持 []interface{} 和 []map[string]interface{} 两种格式）
		var accountsData []interface{}
		switch v := data["accounts"].(type) {
		case []interface{}:
			accountsData = v
		case []map[string]interface{}:
			accountsData = make([]interface{}, len(v))
			for i, m := range v {
				accountsData[i] = m
			}
		}
		for _, item := range accountsData {
			accMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			// 支持驼峰和下划线两种格式
			clientID := getString(accMap, "clientId")
			if clientID == "" {
				clientID = getString(accMap, "client_id")
			}
			clientSecret := getString(accMap, "clientSecret")
			if clientSecret == "" {
				clientSecret = getString(accMap, "client_secret")
			}

			// 跳过无效账号（缺少 clientId 或 clientSecret）
			if clientID == "" || clientSecret == "" {
				continue
			}

			// 如果没有 ID，使用 UUID 生成
			accID := getString(accMap, "id")
			if accID == "" {
				accID = uuid.New().String()
			}

			acc := &models.Account{
				ID:                accID,
				Label:             getStringPtr(accMap, "label"),
				ClientID:          clientID,
				ClientSecret:      clientSecret,
				RefreshToken:      getStringPtrFallback(accMap, "refreshToken", "refresh_token"),
				AccessToken:       getStringPtrFallback(accMap, "accessToken", "access_token"),
				LastRefreshTime:   getStringPtrFallback(accMap, "lastRefreshTime", "last_refresh_time"),
				LastRefreshStatus: getStringPtrFallback(accMap, "lastRefreshStatus", "last_refresh_status"),
				CreatedAt:         getStringFallback(accMap, "createdAt", "created_at"),
				UpdatedAt:         getStringFallback(accMap, "updatedAt", "updated_at"),
				Enabled:           getBool(accMap, "enabled"),
				ErrorCount:        getIntFallback(accMap, "errorCount", "error_count"),
				SuccessCount:      getIntFallback(accMap, "successCount", "success_count"),
				QUserID:           getStringPtrFallback(accMap, "q_user_id", "qUserId"),
				Email:             getStringPtr(accMap, "email"),
				AuthMethod:        getStringPtrFallback(accMap, "auth_method", "authMethod"),
				Region:            getStringPtr(accMap, "region"),
				MachineID:         getStringPtrFallback(accMap, "machine_id", "machineId"),
			}

			// 处理 Other 字段
			if other, ok := accMap["other"]; ok && other != nil {
				otherJSON, _ := json.Marshal(other)
				acc.Other = otherJSON
			}

			if err := tx.Create(acc).Error; err != nil {
				return err
			}
		}

		// 恢复设置
		if settingsData, ok := data["settings"].(map[string]interface{}); ok {
			// 驼峰到下划线的映射（兼容旧版本备份）
			keyMapping := map[string]string{
				"adminPassword":          "admin_password",
				"apiKey":                 "api_key",
				"debugLog":               "debug_log",
				"enableRequestLog":       "enable_request_log",
				"logRetentionDays":       "log_retention_days",
				"enableIPRateLimit":      "enable_ip_rate_limit",
				"ipRateLimitWindow":      "ip_rate_limit_window",
				"ipRateLimitMax":         "ip_rate_limit_max",
				"maxErrorCount":          "max_error_count",
				"layoutFullWidth":        "layout_full_width",
				"accountSelectionMode":   "account_selection_mode",
				"compressionEnabled":     "compression_enabled",
				"compressionModel":       "compression_model",
				"announcementEnabled":    "announcement_enabled",
				"announcementText":       "announcement_text",
				"freeRequestCount":       "free_request_count",
			}
			for key, value := range settingsData {
				// 转换驼峰格式为下划线格式
				dbKey := key
				if mapped, ok := keyMapping[key]; ok {
					dbKey = mapped
				}
				// 跳过 schema_version 和 blockedIPs（blockedIPs 在单独的表中）
				if dbKey == "schema_version" || key == "blockedIPs" || key == "port" {
					continue
				}
				var strValue string
				switch v := value.(type) {
				case string:
					strValue = v
				case bool:
					strValue = boolToString(v)
				case float64:
					strValue = fmt.Sprintf("%v", v)
				default:
					strValue = fmt.Sprintf("%v", v)
				}
				setting := models.Setting{Key: dbKey, Value: strValue}
				if err := tx.Create(&setting).Error; err != nil {
					return err
				}
			}
		}

		// 恢复封禁IP
		if blockedIPsData, ok := data["blocked_ips"].([]interface{}); ok {
			for _, item := range blockedIPsData {
				ipMap, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				ip := getString(ipMap, "ip")
				if ip == "" {
					continue
				}
				reason := getStringPtr(ipMap, "reason")
				createdAt := getStringFallback(ipMap, "created_at", "createdAt")
				if createdAt == "" {
					createdAt = models.CurrentTime()
				}
				blockedIP := models.BlockedIP{
					IP:        ip,
					Reason:    reason,
					CreatedAt: createdAt,
				}
				if err := tx.Create(&blockedIP).Error; err != nil {
					return err
				}
			}
		}

		// 恢复用户
		if usersData, ok := data["users"].([]interface{}); ok {
			for _, item := range usersData {
				userMap, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				id := getString(userMap, "id")
				name := getString(userMap, "name")
				apiKey := getStringFallback(userMap, "api_key", "apiKey")
				if id == "" || name == "" || apiKey == "" {
					continue
				}
				user := models.User{
					ID:               id,
					Name:             name,
					Email:            getStringPtr(userMap, "email"),
					APIKey:           apiKey,
					CreatedAt:        getStringFallback(userMap, "created_at", "createdAt"),
					UpdatedAt:        getStringFallback(userMap, "updated_at", "updatedAt"),
					Enabled:          getBool(userMap, "enabled"),
					DailyQuota:       getIntFallback(userMap, "daily_quota", "dailyQuota"),
					MonthlyQuota:     getIntFallback(userMap, "monthly_quota", "monthlyQuota"),
					TotalTokensUsed:  int64(getIntFallback(userMap, "total_tokens_used", "totalTokensUsed")),
					LastResetDaily:   getStringPtr(userMap, "last_reset_daily"),
					LastResetMonthly: getStringPtr(userMap, "last_reset_monthly"),
					Notes:            getStringPtr(userMap, "notes"),
				}
				if err := tx.Create(&user).Error; err != nil {
					return err
				}
			}
		}

		// 恢复用户Token使用记录
		if usageData, ok := data["user_token_usage"].([]interface{}); ok {
			for _, item := range usageData {
				usageMap, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				id := getString(usageMap, "id")
				userID := getStringFallback(usageMap, "user_id", "userId")
				date := getString(usageMap, "date")
				if id == "" || userID == "" || date == "" {
					continue
				}
				usage := models.UserTokenUsage{
					ID:           id,
					UserID:       userID,
					Date:         date,
					InputTokens:  int64(getIntFallback(usageMap, "input_tokens", "inputTokens")),
					OutputTokens: int64(getIntFallback(usageMap, "output_tokens", "outputTokens")),
					TotalTokens:  int64(getIntFallback(usageMap, "total_tokens", "totalTokens")),
					RequestCount: int64(getIntFallback(usageMap, "request_count", "requestCount")),
				}
				if err := tx.Create(&usage).Error; err != nil {
					return err
				}
			}
		}

		// 恢复导入账号记录
		if importedData, ok := data["imported_accounts"].([]interface{}); ok {
			for _, item := range importedData {
				impMap, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				id := getString(impMap, "id")
				originalRefreshToken := getStringFallback(impMap, "original_refresh_token", "originalRefreshToken")
				if id == "" || originalRefreshToken == "" {
					continue
				}
				importedAcc := models.ImportedAccount{
					ID:                   id,
					OriginalRefreshToken: originalRefreshToken,
					Email:                getStringPtr(impMap, "email"),
					QUserID:              getStringPtrFallback(impMap, "q_user_id", "qUserId"),
					AccessToken:          getStringPtrFallback(impMap, "access_token", "accessToken"),
					NewRefreshToken:      getStringPtrFallback(impMap, "new_refresh_token", "newRefreshToken"),
					SubscriptionType:     getStringPtrFallback(impMap, "subscription_type", "subscriptionType"),
					SubscriptionTitle:    getStringPtrFallback(impMap, "subscription_title", "subscriptionTitle"),
					UsageCurrent:         getFloatFallback(impMap, "usage_current", "usageCurrent"),
					UsageLimit:           getFloatFallback(impMap, "usage_limit", "usageLimit"),
					AccountID:            getStringPtrFallback(impMap, "account_id", "accountId"),
					ImportedAt:           getStringFallback(impMap, "imported_at", "importedAt"),
					RawResponse:          getStringPtrFallback(impMap, "raw_response", "rawResponse"),
					ImportSource:         getStringFallback(impMap, "import_source", "importSource"),
				}
				if importedAcc.ImportSource == "" {
					importedAcc.ImportSource = "token_import"
				}
				if err := tx.Create(&importedAcc).Error; err != nil {
					return err
				}
			}
		}

		return nil
	})
}

