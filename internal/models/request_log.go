package models

// RequestLog 请求日志
// 注意：移除了 not null 约束以兼容旧数据迁移
type RequestLog struct {
	ID            string  `gorm:"primaryKey;size:36" json:"id"`
	Timestamp     string  `gorm:"size:50;index:idx_logs_timestamp;index:idx_logs_ip_time,priority:2;index:idx_logs_account_time,priority:2;index:idx_logs_endpoint,priority:2;index:idx_logs_success,priority:2" json:"timestamp"`
	ClientIP      string  `gorm:"column:client_ip;size:45;index:idx_logs_ip_time,priority:1" json:"client_ip"`
	Method        string  `gorm:"size:10" json:"method"`
	Path          string  `gorm:"size:255" json:"path"`
	EndpointType  *string `gorm:"column:endpoint_type;size:50;index:idx_logs_endpoint,priority:1" json:"endpoint_type,omitempty"`
	AccountID     *string `gorm:"column:account_id;size:36;index:idx_logs_account_time,priority:1" json:"account_id,omitempty"`
	UserID        *string `gorm:"column:user_id;size:36;index:idx_logs_user_time" json:"user_id,omitempty"`
	APIKeyPrefix  *string `gorm:"column:api_key_prefix;size:20" json:"api_key_prefix,omitempty"`
	Model         *string `gorm:"size:100" json:"model,omitempty"`
	OriginalModel *string `gorm:"column:original_model;size:100" json:"original_model,omitempty"`
	StatusCode    int     `gorm:"column:status_code" json:"status_code"`
	IsSuccess     bool    `gorm:"column:is_success;index:idx_logs_success,priority:1" json:"is_success"`
	IsStream      *bool   `gorm:"column:is_stream" json:"is_stream,omitempty"`
	InputTokens   int     `gorm:"column:input_tokens;default:0" json:"input_tokens"`
	OutputTokens  int     `gorm:"column:output_tokens;default:0" json:"output_tokens"`
	DurationMs    int64   `gorm:"column:duration_ms" json:"duration_ms"`
	ErrorMessage  *string `gorm:"column:error_message;type:text" json:"error_message,omitempty"`
	UserAgent     *string `gorm:"column:user_agent;size:500" json:"user_agent,omitempty"`
	CostUSD       float64 `gorm:"-" json:"cost_usd"` // 美元成本（不存储到数据库，动态计算）
	// 用户归属信息（不存储到数据库，动态查询）
	UserName     *string `gorm:"-" json:"user_name,omitempty"`     // 用户名
	UserType     *string `gorm:"-" json:"user_type,omitempty"`     // 用户类型：admin/vip/normal
}

// CalculateCost 计算请求的美元成本
// @author ygw
func (r *RequestLog) CalculateCost() float64 {
	_, _, cost := CalculateTokenCost(int64(r.InputTokens), int64(r.OutputTokens))
	return cost
}

// TableName 指定表名
func (RequestLog) TableName() string {
	return "request_logs"
}

// RequestStats 请求统计
type RequestStats struct {
	TotalRequests     int64         `json:"total_requests"`
	SuccessRequests   int64         `json:"success_requests"`
	FailedRequests    int64         `json:"failed_requests"`
	SuccessRate       float64       `json:"success_rate"`
	TotalInputTokens  int64         `json:"total_input_tokens"`
	TotalOutputTokens int64         `json:"total_output_tokens"`
	AvgDurationMs     float64       `json:"avg_duration_ms"`
	TopIPs            []IPStat      `json:"top_ips"`
	TopAccounts       []AccountStat `json:"top_accounts"`
	TotalCostUSD      float64       `json:"total_cost_usd"`      // 总消费（美元）
	InputCostUSD      float64       `json:"input_cost_usd"`      // 输入消费（美元）
	OutputCostUSD     float64       `json:"output_cost_usd"`     // 输出消费（美元）
}

// IPStat IP统计
type IPStat struct {
	IP           string `json:"ip"`
	RequestCount int64  `json:"request_count"`
}

// AccountStat 账号统计
type AccountStat struct {
	AccountID    string `json:"account_id"`
	RequestCount int64  `json:"request_count"`
	TotalTokens  int64  `json:"total_tokens"`
}

// BlockedIP 被封禁的IP
type BlockedIP struct {
	IP        string  `gorm:"primaryKey;size:45" json:"ip"`
	Reason    *string `gorm:"type:text" json:"reason,omitempty"`
	CreatedAt string  `gorm:"column:created_at;size:50;not null;index" json:"created_at"`
}

// TableName 指定表名
func (BlockedIP) TableName() string {
	return "blocked_ips"
}

// VisitorIP 访问IP统计
// @author ygw - 增加用户关联和时间段统计
type VisitorIP struct {
	IP                string  `json:"ip"`
	LastVisit         string  `json:"last_visit"`
	RequestCount      int64   `json:"request_count"`
	SuccessCount      int64   `json:"success_count"`
	// 用户关联信息 @author ygw
	UserID            *string `json:"user_id,omitempty"`
	UserName          *string `json:"user_name,omitempty"`
	// 时间段请求统计 @author ygw
	RequestCountDay   int64   `json:"request_count_day"`    // 最近一天的请求数
	RequestCountHour  int64   `json:"request_count_hour"`   // 最近一小时的请求数
	// IP配置信息（从ip_configs表关联）
	Notes             *string `json:"notes,omitempty"`              // 备注
	RateLimitRPM      int     `json:"rate_limit_rpm"`               // 每分钟请求频率限制，0表示不限制
	DailyRequestLimit int     `json:"daily_request_limit"`          // 每日请求次数限制，0表示不限制
}

// IPConfig IP配置
// 用于存储单个IP的备注和限制配置
type IPConfig struct {
	IP                string  `gorm:"primaryKey;size:45" json:"ip"`
	Notes             *string `gorm:"type:text" json:"notes,omitempty"`                  // 备注
	RateLimitRPM      int     `gorm:"default:0" json:"rate_limit_rpm"`                   // 每分钟请求频率限制，0表示不限制
	DailyRequestLimit int     `gorm:"default:0" json:"daily_request_limit"`              // 每日请求次数限制，0表示不限制
	CreatedAt         string  `gorm:"column:created_at;size:50;not null" json:"created_at"`
	UpdatedAt         string  `gorm:"column:updated_at;size:50;not null" json:"updated_at"`
}

// TableName 指定表名
func (IPConfig) TableName() string {
	return "ip_configs"
}

// IPConfigUpdate IP配置更新请求
type IPConfigUpdate struct {
	Notes             *string `json:"notes"`
	RateLimitRPM      *int    `json:"rate_limit_rpm"`
	DailyRequestLimit *int    `json:"daily_request_limit"`
}

// LogFilters 日志查询过滤器
type LogFilters struct {
	StartTime    *string `json:"start_time"`
	EndTime      *string `json:"end_time"`
	ClientIP     *string `json:"client_ip"`
	AccountID    *string `json:"account_id"`
	EndpointType *string `json:"endpoint_type"`
	IsSuccess    *bool   `json:"is_success"`
}
