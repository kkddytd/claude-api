package database

import (
	"context"
	"encoding/json"
	"fmt"
	"claude-api/internal/models"
	"strconv"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GetSettings 获取系统设置
func (db *DB) GetSettings(ctx context.Context) (*models.Settings, error) {
	settings := &models.Settings{
		AdminPassword:           "admin",
		DebugLog:                false,
		EnableRequestLog:        false,
		LogRetentionDays:        7,
		EnableIPRateLimit:       false,
		IPRateLimitWindow:       1,
		IPRateLimitMax:          100,
		BlockedIPs:              []string{},
		MaxErrorCount:           db.cfg.MaxErrorCount,
		Port:                    db.cfg.Port,
		LayoutFullWidth:         false,
		AccountSelectionMode:    models.AccountSelectionSequential, // 默认顺序选择
		CompressionEnabled:      false,
		CompressionModel:        models.DefaultCompressionModel,
		QuotaRefreshConcurrency: 20,  // 默认 20 并发
		QuotaRefreshInterval:    120, // 默认 2 分钟
	}

	var settingsList []models.Setting
	if err := db.gorm.WithContext(ctx).Find(&settingsList).Error; err != nil {
		return settings, nil
	}

	for _, s := range settingsList {
		switch s.Key {
		case "admin_password":
			settings.AdminPassword = s.Value
			db.cfg.AdminPassword = s.Value
		case "api_key":
			settings.APIKey = s.Value
			if s.Value == "" {
				db.cfg.OpenAIKeys = []string{}
			} else {
				db.cfg.OpenAIKeys = []string{s.Value}
			}
		case "debug_log":
			settings.DebugLog = s.Value == "true"
		case "enable_request_log":
			settings.EnableRequestLog = s.Value == "true"
		case "log_retention_days":
			fmt.Sscanf(s.Value, "%d", &settings.LogRetentionDays)
		case "enable_ip_rate_limit":
			settings.EnableIPRateLimit = s.Value == "true"
		case "ip_rate_limit_window":
			fmt.Sscanf(s.Value, "%d", &settings.IPRateLimitWindow)
		case "ip_rate_limit_max":
			fmt.Sscanf(s.Value, "%d", &settings.IPRateLimitMax)
		case "blocked_ips":
			if s.Value != "" {
				json.Unmarshal([]byte(s.Value), &settings.BlockedIPs)
			}
		case "max_error_count":
			if v, err := strconv.Atoi(s.Value); err == nil && v > 0 {
				settings.MaxErrorCount = v
				db.cfg.MaxErrorCount = v
			}
		case "port":
			if v, err := strconv.Atoi(s.Value); err == nil && v > 0 {
				settings.Port = v
				settings.PortConfigured = true
				db.cfg.Port = v
			}
		case "layout_full_width":
			settings.LayoutFullWidth = s.Value != "false"
		case "account_selection_mode":
			if s.Value != "" {
				settings.AccountSelectionMode = s.Value
				db.cfg.AccountSelectionMode = s.Value
			}
		// 兼容旧的 random_account_selection 配置
		case "random_account_selection":
			if s.Value == "true" && settings.AccountSelectionMode == models.AccountSelectionSequential {
				settings.AccountSelectionMode = models.AccountSelectionRandom
				db.cfg.AccountSelectionMode = models.AccountSelectionRandom
			}
		case "compression_enabled":
			settings.CompressionEnabled = s.Value == "true"
			db.cfg.CompressionEnabled = s.Value == "true"
		case "compression_model":
			if s.Value != "" {
				settings.CompressionModel = s.Value
			}
		case "announcement_enabled":
			settings.AnnouncementEnabled = s.Value == "true"
		case "announcement_text":
			if s.Value != "" {
				settings.AnnouncementText = s.Value
			}
		case "force_model_enabled":
			settings.ForceModelEnabled = s.Value == "true"
		case "force_model":
			if s.Value != "" {
				settings.ForceModel = s.Value
			}
		case "quota_refresh_concurrency":
			if v, err := strconv.Atoi(s.Value); err == nil && v >= 1 && v <= 50 {
				settings.QuotaRefreshConcurrency = v
			}
		case "quota_refresh_interval":
			if v, err := strconv.Atoi(s.Value); err == nil && v >= 60 && v <= 600 {
				settings.QuotaRefreshInterval = v
			}
		case "http_proxy":
			settings.HTTPProxy = s.Value
			db.cfg.HTTPProxy = s.Value
		case "proxy_pool_enabled":
			settings.ProxyPoolEnabled = s.Value == "true"
			db.cfg.ProxyPoolEnabled = s.Value == "true"
		case "proxy_pool_strategy":
			if s.Value != "" {
				settings.ProxyPoolStrategy = s.Value
				db.cfg.ProxyPoolStrategy = s.Value
			}
		}
	}

	return settings, nil
}

// UpdateSettings 更新系统设置
func (db *DB) UpdateSettings(ctx context.Context, updates *models.SettingsUpdate) error {
	return db.gorm.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		upsertSetting := func(key, value string) error {
			setting := models.Setting{Key: key, Value: value}
			return tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "setting_key"}},
				DoUpdates: clause.AssignmentColumns([]string{"setting_value"}),
			}).Create(&setting).Error
		}

		if updates.AdminPassword != nil {
			if err := upsertSetting("admin_password", *updates.AdminPassword); err != nil {
				return err
			}
			db.cfg.AdminPassword = *updates.AdminPassword
		}

		if updates.APIKey != nil {
			if err := upsertSetting("api_key", *updates.APIKey); err != nil {
				return err
			}
			if *updates.APIKey == "" {
				db.cfg.OpenAIKeys = []string{}
			} else {
				db.cfg.OpenAIKeys = []string{*updates.APIKey}
			}
		}

		if updates.DebugLog != nil {
			if err := upsertSetting("debug_log", boolToString(*updates.DebugLog)); err != nil {
				return err
			}
		}

		if updates.EnableRequestLog != nil {
			if err := upsertSetting("enable_request_log", boolToString(*updates.EnableRequestLog)); err != nil {
				return err
			}
		}

		if updates.LogRetentionDays != nil {
			if err := upsertSetting("log_retention_days", fmt.Sprintf("%d", *updates.LogRetentionDays)); err != nil {
				return err
			}
		}

		if updates.EnableIPRateLimit != nil {
			if err := upsertSetting("enable_ip_rate_limit", boolToString(*updates.EnableIPRateLimit)); err != nil {
				return err
			}
		}

		if updates.IPRateLimitWindow != nil {
			if err := upsertSetting("ip_rate_limit_window", fmt.Sprintf("%d", *updates.IPRateLimitWindow)); err != nil {
				return err
			}
		}

		if updates.IPRateLimitMax != nil {
			if err := upsertSetting("ip_rate_limit_max", fmt.Sprintf("%d", *updates.IPRateLimitMax)); err != nil {
				return err
			}
		}

		if updates.MaxErrorCount != nil {
			maxErr := *updates.MaxErrorCount
			if maxErr < 1 {
				maxErr = 1
			}
			if err := upsertSetting("max_error_count", fmt.Sprintf("%d", maxErr)); err != nil {
				return err
			}
			db.cfg.MaxErrorCount = maxErr
		}

		if updates.Port != nil {
			port := *updates.Port
			if port < 1 || port > 65535 {
				port = 62311
			}
			if err := upsertSetting("port", fmt.Sprintf("%d", port)); err != nil {
				return err
			}
			db.cfg.Port = port
		}

		if updates.LayoutFullWidth != nil {
			if err := upsertSetting("layout_full_width", boolToString(*updates.LayoutFullWidth)); err != nil {
				return err
			}
		}

		if updates.AccountSelectionMode != nil {
			mode := *updates.AccountSelectionMode
			// 验证模式是否有效
			validModes := map[string]bool{
				models.AccountSelectionSequential:     true,
				models.AccountSelectionRandom:         true,
				models.AccountSelectionWeightedRandom: true,
				models.AccountSelectionRoundRobin:     true,
			}
			if !validModes[mode] {
				mode = models.AccountSelectionSequential
			}
			if err := upsertSetting("account_selection_mode", mode); err != nil {
				return err
			}
			db.cfg.AccountSelectionMode = mode
		}

		if updates.CompressionEnabled != nil {
			if err := upsertSetting("compression_enabled", boolToString(*updates.CompressionEnabled)); err != nil {
				return err
			}
			db.cfg.CompressionEnabled = *updates.CompressionEnabled
		}

		if updates.CompressionModel != nil {
			if err := upsertSetting("compression_model", *updates.CompressionModel); err != nil {
				return err
			}
		}

		if updates.AnnouncementEnabled != nil {
			if err := upsertSetting("announcement_enabled", boolToString(*updates.AnnouncementEnabled)); err != nil {
				return err
			}
		}

		if updates.AnnouncementText != nil {
			if err := upsertSetting("announcement_text", *updates.AnnouncementText); err != nil {
				return err
			}
		}

		if updates.ForceModelEnabled != nil {
			if err := upsertSetting("force_model_enabled", boolToString(*updates.ForceModelEnabled)); err != nil {
				return err
			}
		}

		if updates.ForceModel != nil {
			if err := upsertSetting("force_model", *updates.ForceModel); err != nil {
				return err
			}
		}

		if updates.QuotaRefreshConcurrency != nil {
			v := *updates.QuotaRefreshConcurrency
			if v < 1 {
				v = 1
			}
			if v > 50 {
				v = 50
			}
			if err := upsertSetting("quota_refresh_concurrency", fmt.Sprintf("%d", v)); err != nil {
				return err
			}
		}

		if updates.QuotaRefreshInterval != nil {
			v := *updates.QuotaRefreshInterval
			if v < 60 {
				v = 60
			}
			if v > 600 {
				v = 600
			}
			if err := upsertSetting("quota_refresh_interval", fmt.Sprintf("%d", v)); err != nil {
				return err
			}
		}

		if updates.HTTPProxy != nil {
			if err := upsertSetting("http_proxy", *updates.HTTPProxy); err != nil {
				return err
			}
			db.cfg.HTTPProxy = *updates.HTTPProxy
		}

		if updates.ProxyPoolEnabled != nil {
			if err := upsertSetting("proxy_pool_enabled", boolToString(*updates.ProxyPoolEnabled)); err != nil {
				return err
			}
			db.cfg.ProxyPoolEnabled = *updates.ProxyPoolEnabled
		}

		if updates.ProxyPoolStrategy != nil {
			if err := upsertSetting("proxy_pool_strategy", *updates.ProxyPoolStrategy); err != nil {
				return err
			}
			db.cfg.ProxyPoolStrategy = *updates.ProxyPoolStrategy
		}

		if updates.BlockedIPs != nil {
			// 清空现有封禁
			if err := tx.Where("1 = 1").Delete(&models.BlockedIP{}).Error; err != nil {
				return err
			}

			// 添加新的封禁IP
			for _, ip := range *updates.BlockedIPs {
				blockedIP := models.BlockedIP{
					IP:        ip,
					Reason:    strPtr("手动封禁"),
					CreatedAt: models.CurrentTime(),
				}
				if err := tx.Create(&blockedIP).Error; err != nil {
					return err
				}
			}

			// 保存到settings
			blockedIPsJSON, _ := json.Marshal(*updates.BlockedIPs)
			if err := upsertSetting("blocked_ips", string(blockedIPsJSON)); err != nil {
				return err
			}
		}

		return nil
	})
}

// strPtr 返回字符串指针
func strPtr(s string) *string {
	return &s
}
