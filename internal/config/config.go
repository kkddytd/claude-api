package config

import (
	"encoding/json"
	"os"

	"gopkg.in/yaml.v3"
)

// MaxAccounts 最大账号数量（开源版本固定为100）
const MaxAccounts = 100

// DatabaseType 数据库类型
type DatabaseType string

const (
	DatabaseTypeSQLite DatabaseType = "sqlite"
	DatabaseTypeMySQL  DatabaseType = "mysql"
)

// SQLiteConfig SQLite 数据库配置
type SQLiteConfig struct {
	Path string `yaml:"path" json:"path"`
}

// MySQLConfig MySQL 数据库配置
type MySQLConfig struct {
	Host     string `yaml:"host" json:"host"`
	Port     int    `yaml:"port" json:"port"`
	User     string `yaml:"user" json:"user"`
	Password string `yaml:"password" json:"password"`
	Database string `yaml:"database" json:"database"`
	Charset  string `yaml:"charset" json:"charset"`
}

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Type   DatabaseType `yaml:"type" json:"type"`
	SQLite SQLiteConfig `yaml:"sqlite" json:"sqlite"`
	MySQL  MySQLConfig  `yaml:"mysql" json:"mysql"`
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Host string `yaml:"host" json:"host"`
	Port int    `yaml:"port" json:"port"`
} 

// Config 应用配置
type Config struct {
	// 数据库配置
	Database DatabaseConfig

	// 服务器配置
	Server ServerConfig

	// 运行时配置（从数据库加载或动态设置）
	DatabaseURL                  string
	OpenAIKeys                   []string
	TokenCountMultiplier         float64
	MaxErrorCount                int
	HTTPProxy                    string
	ProxyPoolEnabled             bool   // 是否启用代理池
	ProxyPoolStrategy            string // 代理选择策略: round_robin, random, weighted
	EnableConsole                bool
	AdminPassword                string
	Host                         string
	Port                         int
	LazyAccountPoolEnabled       bool
	LazyAccountPoolSize          int
	LazyAccountPoolRefreshOffset int
	LazyAccountPoolOrderBy       string
	LazyAccountPoolOrderDesc     bool
	AccountSelectionMode         string // 账号选择方式: sequential, random, weighted_random, round_robin
	CompressionEnabled           bool   // 是否启用上下文压缩

	// 调试和测试模式
	Debug bool
	Test  bool
}

// Load 返回默认配置
func Load() *Config {
	return &Config{
		Database: DatabaseConfig{
			Type: DatabaseTypeSQLite,
			SQLite: SQLiteConfig{
				Path: "data.sqlite3",
			},
			MySQL: MySQLConfig{
				Host:     "localhost",
				Port:     3306,
				User:     "root",
				Password: "",
				Database: "claude-api",
				Charset:  "utf8mb4",
			},
		},
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 62311,
		},
		DatabaseURL:                  "",
		OpenAIKeys:                   []string{},
		TokenCountMultiplier:         1.0,
		MaxErrorCount:                30,
		HTTPProxy:                    "",
		ProxyPoolEnabled:             false,
		ProxyPoolStrategy:            "round_robin",
		EnableConsole:                true,
		AdminPassword:                "admin",
		Host:                         "0.0.0.0",
		Port:                         62311,
		LazyAccountPoolEnabled:       false,
		LazyAccountPoolSize:          20,
		LazyAccountPoolRefreshOffset: 10,
		LazyAccountPoolOrderBy:       "created_at",
		LazyAccountPoolOrderDesc:     false,
		AccountSelectionMode:         "sequential",
		CompressionEnabled:           false,
		Debug:                        false,
		Test:                         false,
	}
}

// GetMaxAccounts 返回最大账号数量限制（开源版本固定为100）
func (c *Config) GetMaxAccounts() int {
	return MaxAccounts
}

// YAMLFileConfig YAML 配置文件结构
type YAMLFileConfig struct {
	Database DatabaseConfig `yaml:"database"`
	Server   ServerConfig   `yaml:"server"`
	Debug    bool           `yaml:"debug"`
	Test     bool           `yaml:"test"`
}

// FileConfig 配置文件结构（兼容旧 JSON 格式）
type FileConfig struct {
	Host               string `json:"host"`
	Port               int    `json:"port"`
	Debug              bool   `json:"debug"`
	Test               bool   `json:"test"`
	CompressionEnabled *bool  `json:"compression_enabled,omitempty"`
}

// TestModePassword 测试模式保护密码（仅后端使用）
const TestModePassword = "yunsheng123"

// LoadFromYAML 从 YAML 配置文件加载配置
func LoadFromYAML(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var yamlConfig YAMLFileConfig
	if err := yaml.Unmarshal(data, &yamlConfig); err != nil {
		return nil, err
	}

	cfg := Load()

	if yamlConfig.Database.Type != "" {
		cfg.Database.Type = yamlConfig.Database.Type
	}
	if yamlConfig.Database.SQLite.Path != "" {
		cfg.Database.SQLite.Path = yamlConfig.Database.SQLite.Path
	}
	if yamlConfig.Database.MySQL.Host != "" {
		cfg.Database.MySQL.Host = yamlConfig.Database.MySQL.Host
	}
	if yamlConfig.Database.MySQL.Port != 0 {
		cfg.Database.MySQL.Port = yamlConfig.Database.MySQL.Port
	}
	if yamlConfig.Database.MySQL.User != "" {
		cfg.Database.MySQL.User = yamlConfig.Database.MySQL.User
	}
	if yamlConfig.Database.MySQL.Password != "" {
		cfg.Database.MySQL.Password = yamlConfig.Database.MySQL.Password
	}
	if yamlConfig.Database.MySQL.Database != "" {
		cfg.Database.MySQL.Database = yamlConfig.Database.MySQL.Database
	}
	if yamlConfig.Database.MySQL.Charset != "" {
		cfg.Database.MySQL.Charset = yamlConfig.Database.MySQL.Charset
	}
	if yamlConfig.Server.Host != "" {
		cfg.Server.Host = yamlConfig.Server.Host
		cfg.Host = yamlConfig.Server.Host
	}
	if yamlConfig.Server.Port != 0 {
		cfg.Server.Port = yamlConfig.Server.Port
		cfg.Port = yamlConfig.Server.Port
	}
	cfg.Debug = yamlConfig.Debug
	cfg.Test = yamlConfig.Test

	return cfg, nil
}

// LoadFromFile 从当前目录的 config.json 加载配置（兼容旧格式）
func LoadFromFile() (*FileConfig, error) {
	data, err := os.ReadFile("config.json")
	if err != nil {
		return nil, err
	}
	var fc FileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return nil, err
	}
	return &fc, nil
}

// LoadConfig 智能加载配置文件（优先 YAML，兼容 JSON）
func LoadConfig() (*Config, error) {
	if _, err := os.Stat("config.yaml"); err == nil {
		return LoadFromYAML("config.yaml")
	}

	if _, err := os.Stat("config.yml"); err == nil {
		return LoadFromYAML("config.yml")
	}

	if _, err := os.Stat("config.json"); err == nil {
		fc, err := LoadFromFile()
		if err != nil {
			return nil, err
		}
		cfg := Load()
		if fc.Host != "" {
			cfg.Host = fc.Host
			cfg.Server.Host = fc.Host
		}
		if fc.Port != 0 {
			cfg.Port = fc.Port
			cfg.Server.Port = fc.Port
		}
		cfg.Debug = fc.Debug
		cfg.Test = fc.Test
		if fc.CompressionEnabled != nil {
			cfg.CompressionEnabled = *fc.CompressionEnabled
		}
		return cfg, nil
	}

	return Load(), nil
}
