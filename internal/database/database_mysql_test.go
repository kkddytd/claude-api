//go:build integration

package database

import (
	"context"
	"os"
	"claude-api/internal/config"
	"claude-api/internal/models"
	"testing"
	"time"

	"github.com/google/uuid"
)

// 运行 MySQL 集成测试需要设置以下环境变量:
// TEST_MYSQL_HOST=localhost
// TEST_MYSQL_PORT=3306
// TEST_MYSQL_USER=root
// TEST_MYSQL_PASSWORD=password
// TEST_MYSQL_DATABASE=claude-api_test
//
// 运行命令: go test -v -tags=integration ./internal/database/...

// setupMySQLTestDB 创建 MySQL 测试数据库
func setupMySQLTestDB(t *testing.T) *DB {
	host := os.Getenv("TEST_MYSQL_HOST")
	if host == "" {
		host = "localhost"
	}

	portStr := os.Getenv("TEST_MYSQL_PORT")
	port := 3306
	if portStr != "" {
		// 简单解析端口
		for _, c := range portStr {
			port = port*10 + int(c-'0')
		}
		port = port / 10000 // 修正解析
	}
	port = 3306 // 默认端口

	user := os.Getenv("TEST_MYSQL_USER")
	if user == "" {
		user = "root"
	}

	password := os.Getenv("TEST_MYSQL_PASSWORD")
	database := os.Getenv("TEST_MYSQL_DATABASE")
	if database == "" {
		database = "claude-api_test"
	}

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: config.DatabaseTypeMySQL,
			MySQL: config.MySQLConfig{
				Host:     host,
				Port:     port,
				User:     user,
				Password: password,
				Database: database,
				Charset:  "utf8mb4",
			},
		},
		MaxErrorCount: 3,
	}

	db, err := New(cfg)
	if err != nil {
		t.Skipf("跳过 MySQL 测试: 无法连接数据库: %v", err)
	}

	// 清理测试数据
	cleanupMySQLTestData(db)

	return db
}

// cleanupMySQLTestData 清理测试数据
func cleanupMySQLTestData(db *DB) {
	ctx := context.Background()
	db.gorm.WithContext(ctx).Where("1 = 1").Delete(&models.RequestLog{})
	db.gorm.WithContext(ctx).Where("1 = 1").Delete(&models.UserTokenUsage{})
	db.gorm.WithContext(ctx).Where("1 = 1").Delete(&models.User{})
	db.gorm.WithContext(ctx).Where("1 = 1").Delete(&models.Account{})
	db.gorm.WithContext(ctx).Where("1 = 1").Delete(&models.BlockedIP{})
	db.gorm.WithContext(ctx).Where("1 = 1").Delete(&models.ImportedAccount{})
	// 保留 settings 表中的默认设置
}

// TestMySQLAccountCRUD 测试 MySQL 账号的增删改查
func TestMySQLAccountCRUD(t *testing.T) {
	db := setupMySQLTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// 创建账号
	t.Run("CreateAccount", func(t *testing.T) {
		acc := &models.Account{
			ID:           uuid.New().String(),
			ClientID:     "mysql-test-client-id",
			ClientSecret: "mysql-test-client-secret",
			CreatedAt:    models.CurrentTime(),
			UpdatedAt:    models.CurrentTime(),
			Enabled:      true,
		}

		err := db.CreateAccount(ctx, acc)
		if err != nil {
			t.Fatalf("创建账号失败: %v", err)
		}

		// 验证创建
		got, err := db.GetAccount(ctx, acc.ID)
		if err != nil {
			t.Fatalf("获取账号失败: %v", err)
		}
		if got == nil {
			t.Fatal("账号不存在")
		}
		if got.ClientID != acc.ClientID {
			t.Errorf("ClientID 不匹配: got %s, want %s", got.ClientID, acc.ClientID)
		}
	})

	// 列出账号
	t.Run("ListAccounts", func(t *testing.T) {
		accounts, err := db.ListAccounts(ctx, nil, "created_at", true)
		if err != nil {
			t.Fatalf("列出账号失败: %v", err)
		}
		if len(accounts) == 0 {
			t.Error("账号列表为空")
		}
	})

	// 更新账号
	t.Run("UpdateAccount", func(t *testing.T) {
		accounts, _ := db.ListAccounts(ctx, nil, "created_at", true)
		if len(accounts) == 0 {
			t.Skip("没有账号可更新")
		}

		newLabel := "mysql-updated-label"
		updates := &models.AccountUpdate{
			Label: &newLabel,
		}

		err := db.UpdateAccount(ctx, accounts[0].ID, updates)
		if err != nil {
			t.Fatalf("更新账号失败: %v", err)
		}

		// 验证更新
		got, _ := db.GetAccount(ctx, accounts[0].ID)
		if got.Label == nil || *got.Label != newLabel {
			t.Errorf("Label 更新失败: got %v, want %s", got.Label, newLabel)
		}
	})

	// 删除账号
	t.Run("DeleteAccount", func(t *testing.T) {
		acc := &models.Account{
			ID:           uuid.New().String(),
			ClientID:     "mysql-to-delete",
			ClientSecret: "mysql-to-delete",
			CreatedAt:    models.CurrentTime(),
			UpdatedAt:    models.CurrentTime(),
			Enabled:      true,
		}
		db.CreateAccount(ctx, acc)

		err := db.DeleteAccount(ctx, acc.ID)
		if err != nil {
			t.Fatalf("删除账号失败: %v", err)
		}

		// 验证删除
		got, _ := db.GetAccount(ctx, acc.ID)
		if got != nil {
			t.Error("账号未被删除")
		}
	})
}

// TestMySQLUserCRUD 测试 MySQL 用户的增删改查
func TestMySQLUserCRUD(t *testing.T) {
	db := setupMySQLTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// 创建用户
	t.Run("CreateUser", func(t *testing.T) {
		user := &models.User{
			ID:        uuid.New().String(),
			Name:      "MySQL Test User",
			APIKey:    "mysql-test-api-key-" + uuid.New().String(),
			CreatedAt: models.CurrentTime(),
			UpdatedAt: models.CurrentTime(),
			Enabled:   true,
		}

		err := db.CreateUser(ctx, user)
		if err != nil {
			t.Fatalf("创建用户失败: %v", err)
		}

		// 验证创建
		got, err := db.GetUser(ctx, user.ID)
		if err != nil {
			t.Fatalf("获取用户失败: %v", err)
		}
		if got.Name != user.Name {
			t.Errorf("Name 不匹配: got %s, want %s", got.Name, user.Name)
		}
	})

	// 通过 API Key 获取用户
	t.Run("GetUserByAPIKey", func(t *testing.T) {
		apiKey := "mysql-unique-api-key-" + uuid.New().String()
		user := &models.User{
			ID:        uuid.New().String(),
			Name:      "MySQL API Key User",
			APIKey:    apiKey,
			CreatedAt: models.CurrentTime(),
			UpdatedAt: models.CurrentTime(),
			Enabled:   true,
		}
		db.CreateUser(ctx, user)

		got, err := db.GetUserByAPIKey(ctx, apiKey)
		if err != nil {
			t.Fatalf("通过 API Key 获取用户失败: %v", err)
		}
		if got == nil {
			t.Fatal("用户不存在")
		}
		if got.ID != user.ID {
			t.Errorf("用户 ID 不匹配: got %s, want %s", got.ID, user.ID)
		}
	})
}

// TestMySQLRequestLogs 测试 MySQL 请求日志
func TestMySQLRequestLogs(t *testing.T) {
	db := setupMySQLTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// 创建请求日志
	t.Run("CreateRequestLog", func(t *testing.T) {
		log := &models.RequestLog{
			ID:         uuid.New().String(),
			Timestamp:  models.CurrentTime(),
			ClientIP:   "127.0.0.1",
			Method:     "POST",
			Path:       "/v1/chat/completions",
			StatusCode: 200,
			IsSuccess:  true,
			DurationMs: 100,
		}

		err := db.CreateRequestLog(ctx, log)
		if err != nil {
			t.Fatalf("创建请求日志失败: %v", err)
		}
	})

	// 批量创建请求日志
	t.Run("BatchCreateRequestLogs", func(t *testing.T) {
		logs := []*models.RequestLog{
			{
				ID:         uuid.New().String(),
				Timestamp:  models.CurrentTime(),
				ClientIP:   "192.168.1.1",
				Method:     "POST",
				Path:       "/v1/chat/completions",
				StatusCode: 200,
				IsSuccess:  true,
				DurationMs: 150,
			},
			{
				ID:         uuid.New().String(),
				Timestamp:  models.CurrentTime(),
				ClientIP:   "192.168.1.2",
				Method:     "POST",
				Path:       "/v1/messages",
				StatusCode: 500,
				IsSuccess:  false,
				DurationMs: 50,
			},
		}

		err := db.BatchCreateRequestLogs(ctx, logs)
		if err != nil {
			t.Fatalf("批量创建请求日志失败: %v", err)
		}
	})

	// 查询请求日志
	t.Run("GetRequestLogs", func(t *testing.T) {
		logs, err := db.GetRequestLogs(ctx, nil, 10, 0)
		if err != nil {
			t.Fatalf("查询请求日志失败: %v", err)
		}
		if len(logs) == 0 {
			t.Error("请求日志为空")
		}
	})

	// 获取请求统计
	t.Run("GetRequestStats", func(t *testing.T) {
		startTime := time.Now().AddDate(0, 0, -1).Format(models.TimeFormat)
		endTime := time.Now().AddDate(0, 0, 1).Format(models.TimeFormat)

		stats, err := db.GetRequestStats(ctx, startTime, endTime)
		if err != nil {
			t.Fatalf("获取请求统计失败: %v", err)
		}
		if stats.TotalRequests == 0 {
			t.Error("请求统计总数为 0")
		}
	})
}

// TestMySQLBlockedIPs 测试 MySQL IP 封禁
func TestMySQLBlockedIPs(t *testing.T) {
	db := setupMySQLTestDB(t)
	defer db.Close()

	ctx := context.Background()

	testIP := "10.0.0.100"

	// 封禁 IP
	t.Run("BlockIP", func(t *testing.T) {
		err := db.BlockIP(ctx, testIP, "MySQL 测试封禁")
		if err != nil {
			t.Fatalf("封禁 IP 失败: %v", err)
		}
	})

	// 检查 IP 是否被封禁
	t.Run("IsIPBlocked", func(t *testing.T) {
		blocked, err := db.IsIPBlocked(ctx, testIP)
		if err != nil {
			t.Fatalf("检查 IP 封禁状态失败: %v", err)
		}
		if !blocked {
			t.Error("IP 应该被封禁")
		}
	})

	// 解封 IP
	t.Run("UnblockIP", func(t *testing.T) {
		err := db.UnblockIP(ctx, testIP)
		if err != nil {
			t.Fatalf("解封 IP 失败: %v", err)
		}

		// 验证解封
		blocked, _ := db.IsIPBlocked(ctx, testIP)
		if blocked {
			t.Error("IP 应该已被解封")
		}
	})
}

// TestMySQLSettings 测试 MySQL 设置
func TestMySQLSettings(t *testing.T) {
	db := setupMySQLTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// 获取设置
	t.Run("GetSettings", func(t *testing.T) {
		settings, err := db.GetSettings(ctx)
		if err != nil {
			t.Fatalf("获取设置失败: %v", err)
		}
		if settings == nil {
			t.Fatal("设置为空")
		}
	})

	// 更新设置
	t.Run("UpdateSettings", func(t *testing.T) {
		newPassword := "mysql-new-password"
		updates := &models.SettingsUpdate{
			AdminPassword: &newPassword,
		}

		err := db.UpdateSettings(ctx, updates)
		if err != nil {
			t.Fatalf("更新设置失败: %v", err)
		}

		// 验证更新
		settings, _ := db.GetSettings(ctx)
		if settings.AdminPassword != newPassword {
			t.Errorf("密码更新失败: got %s, want %s", settings.AdminPassword, newPassword)
		}
	})
}

// TestMySQLBackupRestore 测试 MySQL 备份和恢复
func TestMySQLBackupRestore(t *testing.T) {
	db := setupMySQLTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// 创建测试数据
	acc := &models.Account{
		ID:           uuid.New().String(),
		ClientID:     "mysql-backup-test-client",
		ClientSecret: "mysql-backup-test-secret",
		CreatedAt:    models.CurrentTime(),
		UpdatedAt:    models.CurrentTime(),
		Enabled:      true,
	}
	db.CreateAccount(ctx, acc)

	// 备份
	t.Run("BackupData", func(t *testing.T) {
		backup, err := db.BackupData(ctx)
		if err != nil {
			t.Fatalf("备份数据失败: %v", err)
		}
		if backup["accounts"] == nil {
			t.Error("备份中没有账号数据")
		}
		if backup["settings"] == nil {
			t.Error("备份中没有设置数据")
		}
	})

	// 恢复
	t.Run("RestoreData", func(t *testing.T) {
		// 先备份
		backup, _ := db.BackupData(ctx)

		// 清空数据
		cleanupMySQLTestData(db)

		// 恢复数据
		err := db.RestoreData(ctx, backup)
		if err != nil {
			t.Fatalf("恢复数据失败: %v", err)
		}

		// 验证恢复
		accounts, _ := db.ListAccounts(ctx, nil, "created_at", true)
		if len(accounts) == 0 {
			t.Error("恢复后账号列表为空")
		}
	})
}

