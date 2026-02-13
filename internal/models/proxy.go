package models

// Proxy 代理配置
// @author ygw
type Proxy struct {
	ID        int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	URL       string `gorm:"column:url;type:text;not null" json:"url"`   // 代理地址，支持 % 占位符
	Name      string `gorm:"column:name;size:100" json:"name"`           // 代理名称（可选）
	Enabled   bool   `gorm:"column:enabled;default:true" json:"enabled"` // 是否启用
	Weight    int    `gorm:"column:weight;default:1" json:"weight"`      // 权重（用于加权轮换）
	CreatedAt string `gorm:"column:created_at;size:50" json:"created_at"`
	UpdatedAt string `gorm:"column:updated_at;size:50" json:"updated_at"`
}

// TableName 指定表名
func (Proxy) TableName() string {
	return "proxies"
}

// ProxyCreate 创建代理请求
// @author ygw
type ProxyCreate struct {
	URL     string `json:"url" binding:"required"`
	Name    string `json:"name"`
	Enabled *bool  `json:"enabled"`
	Weight  *int   `json:"weight"`
}

// ProxyUpdate 更新代理请求
// @author ygw
type ProxyUpdate struct {
	URL     *string `json:"url"`
	Name    *string `json:"name"`
	Enabled *bool   `json:"enabled"`
	Weight  *int    `json:"weight"`
}
