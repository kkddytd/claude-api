package models

// Setting 表示数据库中的键值对设置
// 注意：使用 setting_key 而不是 key，因为 key 是 MySQL 保留字
type Setting struct {
	Key   string `gorm:"column:setting_key;primaryKey;size:100" json:"key"`
	Value string `gorm:"column:setting_value;type:text" json:"value"`
}

// TableName 指定表名
func (Setting) TableName() string {
	return "settings"
}

// AccountSelectionMode 账号选择方式常量
const (
	AccountSelectionSequential     = "sequential"      // 顺序选择（默认）
	AccountSelectionRandom         = "random"          // 随机选择
	AccountSelectionWeightedRandom = "weighted_random" // 加权随机选择
	AccountSelectionRoundRobin     = "round_robin"     // 轮询选择
)

// SupportedAccountSelectionModes 支持的账号选择方式列表
var SupportedAccountSelectionModes = []map[string]string{
	{"value": AccountSelectionSequential, "label": "顺序选择", "description": "按账号创建时间顺序选择"},
	{"value": AccountSelectionRandom, "label": "随机选择", "description": "随机选择一个账号"},
	{"value": AccountSelectionWeightedRandom, "label": "加权随机", "description": "根据配额剩余、使用时间等因素加权选择"},
	{"value": AccountSelectionRoundRobin, "label": "轮询选择", "description": "顺序轮流使用每个账号"},
}

// Settings 表示系统配置（用于 API 响应）
type Settings struct {
	AdminPassword        string   `json:"adminPassword"`
	APIKey               string   `json:"apiKey"`
	DebugLog             bool     `json:"debugLog"`
	EnableRequestLog     bool     `json:"enableRequestLog"`
	LogRetentionDays     int      `json:"logRetentionDays"`
	EnableIPRateLimit    bool     `json:"enableIPRateLimit"`
	IPRateLimitWindow    int      `json:"ipRateLimitWindow"`
	IPRateLimitMax       int      `json:"ipRateLimitMax"`
	BlockedIPs           []string `json:"blockedIPs"`
	MaxErrorCount        int      `json:"maxErrorCount"`
	Port                 int      `json:"port"`
	PortConfigured       bool     `json:"-"` // 标记用户是否配置过端口（不序列化到JSON）
	LayoutFullWidth      bool     `json:"layoutFullWidth"`
	AccountSelectionMode string   `json:"accountSelectionMode"` // 账号选择方式: sequential, random, weighted_random, round_robin
	// 代理配置
	HTTPProxy string `json:"httpProxy"` // HTTP/HTTPS/SOCKS5 代理地址
	// 代理池配置
	ProxyPoolEnabled   bool   `json:"proxyPoolEnabled"`   // 是否启用代理池
	ProxyPoolStrategy  string `json:"proxyPoolStrategy"`  // 代理选择策略: round_robin, random, weighted
	// 智能压缩相关配置
	CompressionEnabled bool   `json:"compressionEnabled"` // 是否启用智能压缩
	CompressionModel   string `json:"compressionModel"`   // 压缩使用的模型
	// 公告配置
	AnnouncementEnabled bool   `json:"announcementEnabled"` // 是否启用公告
	AnnouncementText    string `json:"announcementText"`    // 公告内容
	// 强制模型配置
	ForceModelEnabled bool   `json:"forceModelEnabled"` // 是否启用强制模型
	ForceModel        string `json:"forceModel"`        // 强制使用的模型
	// 性能优化配置（合并了配额刷新和状态检查）
	QuotaRefreshConcurrency int `json:"quotaRefreshConcurrency"` // 配额刷新并发数 (1-50)
	QuotaRefreshInterval    int `json:"quotaRefreshInterval"`    // 配额刷新间隔（秒，60-600）
}

// SettingsUpdate 表示更新设置的数据
type SettingsUpdate struct {
	AdminPassword        *string   `json:"adminPassword"`
	APIKey               *string   `json:"apiKey"`
	DebugLog             *bool     `json:"debugLog"`
	EnableRequestLog     *bool     `json:"enableRequestLog"`
	LogRetentionDays     *int      `json:"logRetentionDays"`
	EnableIPRateLimit    *bool     `json:"enableIPRateLimit"`
	IPRateLimitWindow    *int      `json:"ipRateLimitWindow"`
	IPRateLimitMax       *int      `json:"ipRateLimitMax"`
	BlockedIPs           *[]string `json:"blockedIPs"`
	MaxErrorCount        *int      `json:"maxErrorCount"`
	Port                 *int      `json:"port"`
	LayoutFullWidth      *bool     `json:"layoutFullWidth"`
	AccountSelectionMode *string   `json:"accountSelectionMode"` // 账号选择方式
	// 代理配置
	HTTPProxy *string `json:"httpProxy"`
	// 代理池配置
	ProxyPoolEnabled  *bool   `json:"proxyPoolEnabled"`
	ProxyPoolStrategy *string `json:"proxyPoolStrategy"`
	// 智能压缩相关配置
	CompressionEnabled *bool   `json:"compressionEnabled"`
	CompressionModel   *string `json:"compressionModel"`
	// 公告配置
	AnnouncementEnabled *bool   `json:"announcementEnabled"`
	AnnouncementText    *string `json:"announcementText"`
	// 强制模型配置
	ForceModelEnabled *bool   `json:"forceModelEnabled"`
	ForceModel        *string `json:"forceModel"`
	// 性能优化配置（合并了配额刷新和状态检查）
	QuotaRefreshConcurrency *int `json:"quotaRefreshConcurrency"`
	QuotaRefreshInterval    *int `json:"quotaRefreshInterval"`
}

// 支持的压缩模型列表
var SupportedCompressionModels = []string{
	"claude-haiku-4-5-20251001",
	"claude-sonnet-4-5-20250929",
	"claude-opus-4-5-20251101",
}

// 支持的强制模型列表（用于 API 请求）
var SupportedForceModels = []string{
	"claude-haiku-4-5-20251001",
	"claude-sonnet-4-5-20250929",
	"claude-opus-4-5-20251101",
}

// DefaultCompressionModel 默认压缩模型
const DefaultCompressionModel = "claude-sonnet-4-5-20250929"
