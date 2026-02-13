package database

import (
	"context"
	"fmt"
	"claude-api/internal/config"
	"claude-api/internal/logger"
	"claude-api/internal/models"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// DB 封装 GORM 数据库连接
type DB struct {
	gorm *gorm.DB
	cfg  *config.Config
}

// New 创建新的数据库实例（支持 SQLite 和 MySQL）
func New(cfg *config.Config) (*DB, error) {
	var dialector gorm.Dialector

	switch cfg.Database.Type {
	case config.DatabaseTypeMySQL:
		// MySQL 连接
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
			cfg.Database.MySQL.User,
			cfg.Database.MySQL.Password,
			cfg.Database.MySQL.Host,
			cfg.Database.MySQL.Port,
			cfg.Database.MySQL.Database,
			cfg.Database.MySQL.Charset,
		)
		fmt.Printf("[DB] 使用 MySQL 数据库: %s@%s:%d/%s\n",
			cfg.Database.MySQL.User,
			cfg.Database.MySQL.Host,
			cfg.Database.MySQL.Port,
			cfg.Database.MySQL.Database,
		)
		dialector = mysql.Open(dsn)

	default: // sqlite
		// SQLite 连接
		dbPath := cfg.Database.SQLite.Path
		if dbPath == "" {
			dbPath = "data.sqlite3"
		}
		// 添加 SQLite 优化参数
		dsn := fmt.Sprintf("%s?_busy_timeout=30000&_txlock=immediate", dbPath)
		fmt.Printf("[DB] 使用 SQLite 数据库: %s\n", dbPath)
		dialector = sqlite.Open(dsn)
	}

	// GORM 配置
	gormConfig := &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	}
	if cfg.Debug {
		gormConfig.Logger = gormlogger.Default.LogMode(gormlogger.Info)
	}

	// 打开数据库连接
	gormDB, err := gorm.Open(dialector, gormConfig)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	// 获取底层 sql.DB 设置连接池
	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, fmt.Errorf("获取数据库连接失败: %w", err)
	}

	// 设置连接池参数
	if cfg.Database.Type == config.DatabaseTypeMySQL {
		sqlDB.SetMaxOpenConns(25)
		sqlDB.SetMaxIdleConns(10)
	} else {
		// SQLite 只支持一个写入连接
		sqlDB.SetMaxOpenConns(10)
		sqlDB.SetMaxIdleConns(5)

		// SQLite 性能优化
		// 1. 启用 WAL 模式（允许读写并发）
		if err := gormDB.Exec("PRAGMA journal_mode=WAL").Error; err != nil {
			fmt.Printf("[DB] 警告: 启用 WAL 模式失败: %v\n", err)
		} else {
			fmt.Println("[DB] 已启用 WAL 模式")
		}

		// 2. 设置同步模式为 NORMAL（平衡性能和安全性）
		if err := gormDB.Exec("PRAGMA synchronous=NORMAL").Error; err != nil {
			fmt.Printf("[DB] 警告: 设置同步模式失败: %v\n", err)
		}

		// 3. 增加缓存大小到 64MB
		if err := gormDB.Exec("PRAGMA cache_size=-64000").Error; err != nil {
			fmt.Printf("[DB] 警告: 设置缓存大小失败: %v\n", err)
		}

		// 4. 临时表使用内存
		if err := gormDB.Exec("PRAGMA temp_store=MEMORY").Error; err != nil {
			fmt.Printf("[DB] 警告: 设置临时存储失败: %v\n", err)
		}

		fmt.Println("[DB] SQLite 性能优化已应用")
	}

	db := &DB{gorm: gormDB, cfg: cfg}

	// 自动迁移数据库结构
	if err := db.autoMigrate(); err != nil {
		return nil, fmt.Errorf("自动迁移数据库结构失败: %w", err)
	}

	// 初始化默认设置
	if err := db.initDefaultSettings(); err != nil {
		return nil, fmt.Errorf("初始化默认设置失败: %w", err)
	}

	return db, nil
}

// autoMigrate 自动迁移数据库结构
func (db *DB) autoMigrate() error {
	logger.Info("开始自动迁移数据库结构...")

	// 对于 SQLite，禁用外键约束以避免迁移问题
	if db.IsSQLite() {
		db.gorm.Exec("PRAGMA foreign_keys = OFF")
		defer db.gorm.Exec("PRAGMA foreign_keys = ON")
	}

	// 先处理 settings 表的列名迁移（key -> setting_key, value -> setting_value）
	if err := db.migrateSettingsTable(); err != nil {
		logger.Warn("迁移 settings 表列名时出现警告: %v", err)
	}

	// 处理 accounts 表的 clientSecret 列类型迁移（VARCHAR -> TEXT）
	if err := db.migrateAccountsClientSecretColumn(); err != nil {
		logger.Warn("迁移 accounts.clientSecret 列类型时出现警告: %v", err)
	}

	// 处理 accounts 表的 status 字段迁移
	if err := db.migrateAccountStatusColumn(); err != nil {
		logger.Warn("迁移 accounts.status 字段时出现警告: %v", err)
	}

	// 禁用 GORM 的自动约束迁移，只创建/更新表结构
	// 这样可以避免因为 NOT NULL 约束导致数据丢失
	migrator := db.gorm.Migrator()

	// 定义需要迁移的表
	type tableInfo struct {
		model interface{}
		name  string
	}

	tables := []tableInfo{
		{&models.Setting{}, "settings"},
		{&models.Account{}, "accounts"},
		{&models.User{}, "users"},
		{&models.UserTokenUsage{}, "user_token_usage"},
		{&models.RequestLog{}, "request_logs"},
		{&models.BlockedIP{}, "blocked_ips"},
		{&models.IPConfig{}, "ip_configs"},
		{&models.ImportedAccount{}, "imported_accounts"},
		{&models.Proxy{}, "proxies"},
	}

	for _, t := range tables {
		// 检查表是否存在
		if !migrator.HasTable(t.model) {
			// 表不存在，创建新表
			if err := migrator.CreateTable(t.model); err != nil {
				logger.Warn("创建表 %s 时出现警告: %v", t.name, err)
			} else {
				logger.Info("创建表: %s", t.name)
			}
		} else {
			// 表存在，只添加缺失的列（不修改现有列的约束）
			if err := db.addMissingColumns(t.model, t.name); err != nil {
				logger.Warn("更新表 %s 结构时出现警告: %v", t.name, err)
			}
		}
	}

	logger.Info("数据库结构迁移完成")
	return nil
}

// migrateSettingsTable 迁移 settings 表的列名（从 key/value 到 setting_key/setting_value）
func (db *DB) migrateSettingsTable() error {
	// 检查 settings 表是否存在
	var tableExists bool
	if db.IsSQLite() {
		var count int64
		db.gorm.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='settings'").Scan(&count)
		tableExists = count > 0
	} else {
		var count int64
		db.gorm.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'settings'").Scan(&count)
		tableExists = count > 0
	}

	if !tableExists {
		return nil // 表不存在，不需要迁移
	}

	// 检查是否有旧的 key 列
	var hasOldKeyColumn bool
	if db.IsSQLite() {
		var count int64
		db.gorm.Raw("SELECT COUNT(*) FROM pragma_table_info('settings') WHERE name='key'").Scan(&count)
		hasOldKeyColumn = count > 0
	} else {
		var count int64
		db.gorm.Raw("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'settings' AND column_name = 'key'").Scan(&count)
		hasOldKeyColumn = count > 0
	}

	if !hasOldKeyColumn {
		return nil // 已经是新格式，不需要迁移
	}

	logger.Info("检测到旧版 settings 表结构，开始迁移列名...")

	// SQLite 不支持直接重命名列，需要重建表
	if db.IsSQLite() {
		// 1. 创建临时表
		if err := db.gorm.Exec(`CREATE TABLE IF NOT EXISTS settings_new (
			setting_key TEXT PRIMARY KEY,
			setting_value TEXT
		)`).Error; err != nil {
			return err
		}

		// 2. 复制数据
		if err := db.gorm.Exec(`INSERT OR IGNORE INTO settings_new (setting_key, setting_value) 
			SELECT key, value FROM settings`).Error; err != nil {
			return err
		}

		// 3. 删除旧表
		if err := db.gorm.Exec(`DROP TABLE settings`).Error; err != nil {
			return err
		}

		// 4. 重命名新表
		if err := db.gorm.Exec(`ALTER TABLE settings_new RENAME TO settings`).Error; err != nil {
			return err
		}
	} else {
		// MySQL 支持直接重命名列
		if err := db.gorm.Exec("ALTER TABLE settings CHANGE COLUMN `key` setting_key VARCHAR(100)").Error; err != nil {
			return err
		}
		if err := db.gorm.Exec("ALTER TABLE settings CHANGE COLUMN `value` setting_value TEXT").Error; err != nil {
			return err
		}
	}

	logger.Info("settings 表列名迁移完成")
	return nil
}

// migrateAccountsClientSecretColumn 迁移 accounts 表的 clientSecret 列类型（VARCHAR -> TEXT）
// 因为 clientSecret 存储的是 JWT 令牌，长度可能超过 255 字符
func (db *DB) migrateAccountsClientSecretColumn() error {
	// 检查 accounts 表是否存在
	var tableExists bool
	if db.IsSQLite() {
		var count int64
		db.gorm.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='accounts'").Scan(&count)
		tableExists = count > 0
	} else {
		var count int64
		db.gorm.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'accounts'").Scan(&count)
		tableExists = count > 0
	}

	if !tableExists {
		return nil // 表不存在，不需要迁移
	}

	// 只有 MySQL 需要修改列类型，SQLite 的 TEXT 类型没有长度限制
	if !db.IsSQLite() {
		// 检查 clientSecret 列的当前类型
		var dataType string
		db.gorm.Raw(`
			SELECT DATA_TYPE 
			FROM information_schema.columns 
			WHERE table_schema = DATABASE() 
			AND table_name = 'accounts' 
			AND column_name = 'clientSecret'
		`).Scan(&dataType)

		// 如果是 varchar 类型，需要修改为 text
		if dataType == "varchar" {
			logger.Info("检测到 accounts.clientSecret 列类型为 VARCHAR，正在修改为 TEXT...")
			if err := db.gorm.Exec("ALTER TABLE accounts MODIFY COLUMN clientSecret TEXT").Error; err != nil {
				return err
			}
			logger.Info("accounts.clientSecret 列类型迁移完成")
		}
	}

	return nil
}

// addMissingColumns 只添加缺失的列，不修改现有列
func (db *DB) addMissingColumns(model interface{}, tableName string) error {
	migrator := db.gorm.Migrator()

	// 获取模型的所有列
	stmt := &gorm.Statement{DB: db.gorm}
	if err := stmt.Parse(model); err != nil {
		return err
	}

	for _, field := range stmt.Schema.Fields {
		if field.DBName == "" {
			continue
		}
		// 检查列是否存在
		if !migrator.HasColumn(model, field.DBName) {
			// 列不存在，添加
			if err := migrator.AddColumn(model, field.DBName); err != nil {
				logger.Warn("添加列 %s.%s 时出现警告: %v", tableName, field.DBName, err)
			} else {
				logger.Info("添加列: %s.%s", tableName, field.DBName)
			}
		}
	}

	return nil
}

// initDefaultSettings 初始化默认设置
func (db *DB) initDefaultSettings() error {
	// 检查是否已有 admin_password 设置
	var count int64
	if err := db.gorm.Model(&models.Setting{}).Where("setting_key = ?", "admin_password").Count(&count).Error; err != nil {
		return err
	}

	// 如果没有设置，插入默认密码 "admin"
	if count == 0 {
		setting := &models.Setting{Key: "admin_password", Value: "admin"}
		if err := db.gorm.Create(setting).Error; err != nil {
			return err
		}
		fmt.Println("[DB] 已初始化默认管理密码: admin")
	}

	return nil
}

// Close 关闭数据库连接
func (db *DB) Close() error {
	sqlDB, err := db.gorm.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// GetGormDB 获取底层 GORM 实例（用于测试或高级操作）
func (db *DB) GetGormDB() *gorm.DB {
	return db.gorm
}

// IsSQLite 判断是否为 SQLite 数据库
func (db *DB) IsSQLite() bool {
	return db.cfg.Database.Type != config.DatabaseTypeMySQL
}

// IsMySQL 判断是否为 MySQL 数据库
func (db *DB) IsMySQL() bool {
	return db.cfg.Database.Type == config.DatabaseTypeMySQL
}

// GetConfig 返回配置指针
func (db *DB) GetConfig() *config.Config {
	return db.cfg
}

// RetryOnLock 为 SQLite 提供写入重试机制
// 当遇到 database is locked 错误时，自动重试
// @author ygw
func (db *DB) RetryOnLock(ctx context.Context, maxRetries int, fn func() error) error {
	// MySQL 不需要重试机制
	if db.IsMySQL() {
		return fn()
	}

	var lastErr error
	for i := 0; i < maxRetries; i++ {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		// 检查是否为 SQLite 锁定错误
		if !strings.Contains(lastErr.Error(), "database is locked") &&
			!strings.Contains(lastErr.Error(), "SQLITE_BUSY") {
			return lastErr
		}

		// 指数退避重试
		backoff := time.Duration(10*(i+1)) * time.Millisecond
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
			// 继续重试
		}
	}
	return lastErr
}

// migrateAccountStatusColumn 迁移账号状态字段
// 将现有 enabled 字段的值同步到新的 status 字段
// @author ygw
func (db *DB) migrateAccountStatusColumn() error {
	// 检查 accounts 表是否存在
	var tableExists bool
	if db.IsSQLite() {
		var count int64
		db.gorm.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='accounts'").Scan(&count)
		tableExists = count > 0
	} else {
		var count int64
		db.gorm.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'accounts'").Scan(&count)
		tableExists = count > 0
	}

	if !tableExists {
		return nil // 表不存在，不需要迁移
	}

	// 检查 status 列是否存在
	var hasStatusColumn bool
	if db.IsSQLite() {
		var count int64
		db.gorm.Raw("SELECT COUNT(*) FROM pragma_table_info('accounts') WHERE name='status'").Scan(&count)
		hasStatusColumn = count > 0
	} else {
		var count int64
		db.gorm.Raw("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'accounts' AND column_name = 'status'").Scan(&count)
		hasStatusColumn = count > 0
	}

	if !hasStatusColumn {
		// status 列不存在，会由 addMissingColumns 自动添加
		// 这里只需要在添加后进行数据迁移
		return nil
	}

	// 检查是否有需要迁移的数据（status 为空或为默认值但 enabled=false 的记录）
	var needMigration int64
	db.gorm.Raw("SELECT COUNT(*) FROM accounts WHERE (status IS NULL OR status = '' OR status = 'normal') AND enabled = 0").Scan(&needMigration)

	if needMigration > 0 {
		logger.Info("检测到 %d 条账号记录需要迁移 status 字段...", needMigration)

		// 将 enabled=false 的账号状态设置为 disabled
		if err := db.gorm.Exec("UPDATE accounts SET status = 'disabled' WHERE (status IS NULL OR status = '' OR status = 'normal') AND enabled = 0").Error; err != nil {
			return err
		}

		logger.Info("账号 status 字段数据迁移完成")
	}

	// 确保所有 status 为空的记录设置为 normal
	db.gorm.Exec("UPDATE accounts SET status = 'normal' WHERE status IS NULL OR status = ''")

	return nil
}
