package models

// ImportedAccount 表示导入的账号备份记录
// 注意：移除了 not null 约束以兼容旧数据迁移
type ImportedAccount struct {
	ID                   string  `gorm:"primaryKey;size:36" json:"id"`
	OriginalRefreshToken string  `gorm:"column:original_refresh_token;type:text" json:"original_refresh_token"`
	Email                *string `gorm:"size:255;index" json:"email"`
	QUserID              *string `gorm:"column:q_user_id;size:255;index" json:"q_user_id"`
	AccessToken          *string `gorm:"column:access_token;type:text" json:"access_token"`
	NewRefreshToken      *string `gorm:"column:new_refresh_token;type:text" json:"new_refresh_token"`
	SubscriptionType     *string `gorm:"column:subscription_type;size:50" json:"subscription_type"`
	SubscriptionTitle    *string `gorm:"column:subscription_title;size:255" json:"subscription_title"`
	UsageCurrent         float64 `gorm:"column:usage_current;default:0" json:"usage_current"`
	UsageLimit           float64 `gorm:"column:usage_limit;default:0" json:"usage_limit"`
	AccountID            *string `gorm:"column:account_id;size:36;index" json:"account_id"`
	ImportedAt           string  `gorm:"column:imported_at;size:50;index" json:"imported_at"`
	RawResponse          *string `gorm:"column:raw_response;type:text" json:"raw_response"`
	ImportSource         string  `gorm:"column:import_source;size:50;default:'token_import'" json:"import_source"`
}

// TableName 指定表名
func (ImportedAccount) TableName() string {
	return "imported_accounts"
}

