package models

import (
	"encoding/json"
	"time"
)

// Account 表示带有 OIDC 凭证的 AWS 账号
// 注意：移除了 not null 约束以兼容旧数据迁移，应用层保证数据完整性
type Account struct {
	ID                string          `gorm:"primaryKey;size:36" json:"id"`
	Label             *string         `gorm:"size:255" json:"label"`
	ClientID          string          `gorm:"column:clientId;size:255" json:"clientId"`
	ClientSecret      string          `gorm:"column:clientSecret;type:text" json:"clientSecret"`
	RefreshToken      *string         `gorm:"column:refreshToken;type:text" json:"refreshToken"`
	AccessToken       *string         `gorm:"column:accessToken;type:text" json:"accessToken"`
	Other             json.RawMessage `gorm:"type:text" json:"other,omitempty"`
	LastRefreshTime   *string         `gorm:"column:last_refresh_time;size:50;index" json:"last_refresh_time"`
	LastRefreshStatus *string         `gorm:"column:last_refresh_status;size:50" json:"last_refresh_status"`
	CreatedAt         string          `gorm:"column:created_at;size:50;index:idx_enabled_created,priority:2" json:"created_at"`
	UpdatedAt         string          `gorm:"column:updated_at;size:50" json:"updated_at"`
	Enabled           bool            `gorm:"default:true;index;index:idx_enabled_created,priority:1" json:"enabled"`
	Status            string          `gorm:"column:status;size:20;default:'normal';index:idx_status" json:"status"`
	ExhaustedAt       *string         `gorm:"column:exhausted_at;size:50" json:"exhausted_at"`
	StatusReason      *string         `gorm:"column:status_reason;size:255" json:"status_reason"`
	ErrorCount        int             `gorm:"column:error_count;default:0" json:"error_count"`
	SuccessCount      int             `gorm:"column:success_count;default:0;index" json:"success_count"`
	QUserID           *string         `gorm:"column:q_user_id;size:255;index" json:"q_user_id"`
	Email             *string         `gorm:"size:255;index" json:"email"`
	AuthMethod        *string         `gorm:"column:auth_method;size:50" json:"auth_method"`
	Region            *string         `gorm:"size:50;default:'us-east-1'" json:"region"`
	MachineID         *string         `gorm:"column:machine_id;size:64" json:"machine_id"`
	Password          *string         `gorm:"column:password;size:255" json:"password"`
	Username          *string         `gorm:"column:username;size:255" json:"username"`
	// 配额相关字段
	UsageCurrent      float64         `gorm:"column:usage_current;default:0" json:"usage_current"`
	UsageLimit        float64         `gorm:"column:usage_limit;default:0" json:"usage_limit"`
	SubscriptionType  *string         `gorm:"column:subscription_type;size:50" json:"subscription_type"`
	QuotaRefreshedAt  *string         `gorm:"column:quota_refreshed_at;size:50" json:"quota_refreshed_at"`
	TokenExpiry       *int64          `gorm:"column:token_expiry" json:"token_expiry"` // 有效时间（Unix时间戳）@author ygw
}

// TableName 指定表名
func (Account) TableName() string {
	return "accounts"
}

// AccountCreate 表示创建新账号的数据
type AccountCreate struct {
	Label        *string                `json:"label"`
	ClientID     string                 `json:"clientId" binding:"required"`
	ClientSecret string                 `json:"clientSecret" binding:"required"`
	RefreshToken *string                `json:"refreshToken"`
	AccessToken  *string                `json:"accessToken"`
	Other        map[string]interface{} `json:"other"`
	Enabled      *bool                  `json:"enabled"`
}

// AccountUpdate 表示更新账号的数据
type AccountUpdate struct {
	Label        *string                `json:"label"`
	ClientID     *string                `json:"clientId"`
	ClientSecret *string                `json:"clientSecret"`
	RefreshToken *string                `json:"refreshToken"`
	AccessToken  *string                `json:"accessToken"`
	Other        map[string]interface{} `json:"other"`
	Enabled      *bool                  `json:"enabled"`
	Status       *string                `json:"status"`
	Email        *string                `json:"email"`
	AuthMethod   *string                `json:"authMethod"`
	Region       *string                `json:"region"`
	QUserID      *string                `json:"qUserId"`
	MachineID    *string                `json:"machineId"`
}

// BatchAccountCreate 表示批量创建账号请求
type BatchAccountCreate struct {
	Accounts []AccountCreate `json:"accounts" binding:"required"`
}

// DirectImportAccount 直接导入账号的数据结构
// @author ygw
type DirectImportAccount struct {
	ClientID     string  `json:"clientId" binding:"required"`
	ClientSecret string  `json:"clientSecret" binding:"required"`
	RefreshToken *string `json:"refreshToken"`
	AccessToken  *string `json:"accessToken"`
	Email        *string `json:"email"`
	Password     *string `json:"password"`
	Username     *string `json:"username"`
	AddedTime    *string `json:"added_time"`
}

// DirectImportRequest 直接导入账号请求（支持批量）
// @author ygw
type DirectImportRequest struct {
	Accounts []DirectImportAccount `json:"accounts" binding:"required"`
}

// TimeFormat 时间格式（带时区）
const TimeFormat = "2006-01-02T15:04:05Z07:00"

// AccountStatus 账号状态枚举
// @author ygw
const (
	AccountStatusNormal    = "normal"    // 正常可用
	AccountStatusDisabled  = "disabled"  // 手动禁用
	AccountStatusSuspended = "suspended" // 被封控（不自动恢复）
	AccountStatusExhausted = "exhausted" // 额度用尽（30天后自动恢复）
	AccountStatusExpired   = "expired"   // Token 过期/失效
)

// IsAccountStatusValid 检查状态值是否有效
// @author ygw
func IsAccountStatusValid(status string) bool {
	switch status {
	case AccountStatusNormal, AccountStatusDisabled, AccountStatusSuspended,
		AccountStatusExhausted, AccountStatusExpired:
		return true
	}
	return false
}

// IsAccountAvailable 检查账号是否可用于请求
// @author ygw
func IsAccountAvailable(status string) bool {
	return status == AccountStatusNormal
}

// CurrentTime 返回当前本地时间的格式字符串
func CurrentTime() string {
	return time.Now().Format(TimeFormat)
}
