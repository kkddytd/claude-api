package models

// User 表示系统用户
// @author ygw
type User struct {
	ID               string  `gorm:"primaryKey;size:36" json:"id"`
	Name             string  `gorm:"size:255;not null" json:"name"`
	Email            *string `gorm:"size:255" json:"email,omitempty"`
	APIKey           string  `gorm:"column:api_key;size:255;uniqueIndex;not null" json:"api_key"`
	CreatedAt        string  `gorm:"column:created_at;size:50;not null;index" json:"created_at"`
	UpdatedAt        string  `gorm:"column:updated_at;size:50;not null" json:"updated_at"`
	Enabled          bool    `gorm:"default:true;index" json:"enabled"`
	IsVip            bool    `gorm:"column:is_vip;default:false" json:"is_vip"` // VIP用户标识 @author ygw
	DailyQuota       int     `gorm:"column:daily_quota;default:0" json:"daily_quota"`
	MonthlyQuota     int     `gorm:"column:monthly_quota;default:0" json:"monthly_quota"`
	RequestQuota     int     `gorm:"column:request_quota;default:0" json:"request_quota"`         // 每日请求次数限制，0表示不限制 @author ygw
	RateLimitRPM     int     `gorm:"column:rate_limit_rpm;default:0" json:"rate_limit_rpm"`       // 每分钟请求频率限制，0表示不限制 @author ygw
	TotalTokensUsed  int64   `gorm:"column:total_tokens_used;default:0" json:"total_tokens_used"`
	TotalRequests    int64   `gorm:"column:total_requests;default:0" json:"total_requests"`       // 总请求次数 @author ygw
	TotalCostUSD     float64 `gorm:"column:total_cost_usd;default:0" json:"total_cost_usd"`       // 总消费金额（美元）@author ygw
	LastResetDaily   *string `gorm:"column:last_reset_daily;size:50" json:"last_reset_daily,omitempty"`
	LastResetMonthly *string `gorm:"column:last_reset_monthly;size:50" json:"last_reset_monthly,omitempty"`
	Notes            *string `gorm:"type:text" json:"notes,omitempty"`
}

// TableName 指定表名
func (User) TableName() string {
	return "users"
}

// UserCreate 表示创建用户的请求体
// @author ygw
type UserCreate struct {
	Name         string  `json:"name" binding:"required"`
	Email        *string `json:"email"`
	DailyQuota   *int    `json:"daily_quota"`
	MonthlyQuota *int    `json:"monthly_quota"`
	RequestQuota *int    `json:"request_quota"` // 每日请求次数限制 @author ygw
	RateLimitRPM *int    `json:"rate_limit_rpm"` // 每分钟请求频率限制 @author ygw
	Enabled      *bool   `json:"enabled"`
	IsVip        *bool   `json:"is_vip"` // VIP用户标识 @author ygw
	Notes        *string `json:"notes"`
}

// UserUpdate 表示更新用户的请求体
// @author ygw
type UserUpdate struct {
	Name         *string `json:"name"`
	Email        *string `json:"email"`
	DailyQuota   *int    `json:"daily_quota"`
	MonthlyQuota *int    `json:"monthly_quota"`
	RequestQuota *int    `json:"request_quota"` // 每日请求次数限制 @author ygw
	RateLimitRPM *int    `json:"rate_limit_rpm"` // 每分钟请求频率限制 @author ygw
	Enabled      *bool   `json:"enabled"`
	IsVip        *bool   `json:"is_vip"` // VIP用户标识 @author ygw
	Notes        *string `json:"notes"`
}

// UserTokenUsage 表示用户每日 Token 使用量
type UserTokenUsage struct {
	ID           string `gorm:"primaryKey;size:36" json:"id"`
	UserID       string `gorm:"column:user_id;size:36;not null;index:idx_usage_user_date,priority:1" json:"user_id"`
	Date         string `gorm:"size:10;not null;index:idx_usage_user_date,priority:2" json:"date"` // YYYY-MM-DD format
	InputTokens  int64  `gorm:"column:input_tokens;default:0" json:"input_tokens"`
	OutputTokens int64  `gorm:"column:output_tokens;default:0" json:"output_tokens"`
	TotalTokens  int64  `gorm:"column:total_tokens;default:0" json:"total_tokens"`
	RequestCount int64  `gorm:"column:request_count;default:0" json:"request_count"`
}

// TableName 指定表名
func (UserTokenUsage) TableName() string {
	return "user_token_usage"
}

// UserStats 表示用户的聚合统计数据
type UserStats struct {
	UserID         string           `json:"user_id"`
	UserName       string           `json:"user_name"`
	TotalRequests  int64            `json:"total_requests"`
	TotalTokens    int64            `json:"total_tokens"`
	InputTokens    int64            `json:"input_tokens"`
	OutputTokens   int64            `json:"output_tokens"`
	DailyUsage     []UserTokenUsage `json:"daily_usage"`
	MonthlyTotal   int64            `json:"monthly_total"`
	DailyQuota     int              `json:"daily_quota"`
	MonthlyQuota   int              `json:"monthly_quota"`
	QuotaRemaining int64            `json:"quota_remaining"`
	TotalCostUSD   float64          `json:"total_cost_usd"`   // 总消费（美元）
	InputCostUSD   float64          `json:"input_cost_usd"`   // 输入消费（美元）
	OutputCostUSD  float64          `json:"output_cost_usd"`  // 输出消费（美元）
	MonthlyCostUSD float64          `json:"monthly_cost_usd"` // 本月消费（美元）
}

// CalculateTokenCost 计算 token 的美元成本
// 基于 Claude Opus 4.5 定价：输入 $5/M tokens，输出 $25/M tokens
// @author ygw
func CalculateTokenCost(inputTokens, outputTokens int64) (inputCost, outputCost, totalCost float64) {
	const (
		inputPricePerMillion  = 5.0  // $5 per million input tokens (Opus 4.5)
		outputPricePerMillion = 25.0 // $25 per million output tokens (Opus 4.5)
	)

	inputCost = float64(inputTokens) / 1000000.0 * inputPricePerMillion
	outputCost = float64(outputTokens) / 1000000.0 * outputPricePerMillion
	totalCost = inputCost + outputCost

	return inputCost, outputCost, totalCost
}
