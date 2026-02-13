package database

import (
	"context"
	"claude-api/internal/config"
	"claude-api/internal/models"
	"testing"
	"time"

	"github.com/google/uuid"
)

// setupTestDB 创建测试数据库（使用 SQLite 内存数据库）
func setupTestDB(t *testing.T) *DB {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: config.DatabaseTypeSQLite,
			SQLite: config.SQLiteConfig{
				Path: ":memory:",
			},
		},
		MaxErrorCount: 3,
	}

	db, err := New(cfg)
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}

	return db
}

// TestAccountCRUD 测试账号的增删改查
func TestAccountCRUD(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// 创建账号
	t.Run("CreateAccount", func(t *testing.T) {
		acc := &models.Account{
			ID:           uuid.New().String(),
			ClientID:     "test-client-id",
			ClientSecret: "test-client-secret",
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

		newLabel := "updated-label"
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
			ClientID:     "to-delete",
			ClientSecret: "to-delete",
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

	// 获取账号数量
	t.Run("GetAccountCount", func(t *testing.T) {
		count, err := db.GetAccountCount(ctx)
		if err != nil {
			t.Fatalf("获取账号数量失败: %v", err)
		}
		if count < 0 {
			t.Errorf("账号数量无效: %d", count)
		}
	})
}

// TestUserCRUD 测试用户的增删改查
func TestUserCRUD(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// 创建用户
	t.Run("CreateUser", func(t *testing.T) {
		user := &models.User{
			ID:        uuid.New().String(),
			Name:      "Test User",
			APIKey:    "test-api-key-" + uuid.New().String(),
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
		apiKey := "unique-api-key-" + uuid.New().String()
		user := &models.User{
			ID:        uuid.New().String(),
			Name:      "API Key User",
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

	// 列出用户
	t.Run("ListUsers", func(t *testing.T) {
		users, err := db.ListUsers(ctx, nil)
		if err != nil {
			t.Fatalf("列出用户失败: %v", err)
		}
		if len(users) == 0 {
			t.Error("用户列表为空")
		}
	})

	// 更新用户
	t.Run("UpdateUser", func(t *testing.T) {
		users, _ := db.ListUsers(ctx, nil)
		if len(users) == 0 {
			t.Skip("没有用户可更新")
		}

		newName := "Updated User"
		updates := &models.UserUpdate{
			Name: &newName,
		}

		err := db.UpdateUser(ctx, users[0].ID, updates)
		if err != nil {
			t.Fatalf("更新用户失败: %v", err)
		}

		// 验证更新
		got, _ := db.GetUser(ctx, users[0].ID)
		if got.Name != newName {
			t.Errorf("Name 更新失败: got %s, want %s", got.Name, newName)
		}
	})

	// 删除用户
	t.Run("DeleteUser", func(t *testing.T) {
		user := &models.User{
			ID:        uuid.New().String(),
			Name:      "To Delete",
			APIKey:    "to-delete-" + uuid.New().String(),
			CreatedAt: models.CurrentTime(),
			UpdatedAt: models.CurrentTime(),
			Enabled:   true,
		}
		db.CreateUser(ctx, user)

		err := db.DeleteUser(ctx, user.ID)
		if err != nil {
			t.Fatalf("删除用户失败: %v", err)
		}
	})
}

// TestSettings 测试设置的读写
func TestSettings(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// 获取默认设置
	t.Run("GetSettings", func(t *testing.T) {
		settings, err := db.GetSettings(ctx)
		if err != nil {
			t.Fatalf("获取设置失败: %v", err)
		}
		if settings.AdminPassword != "admin" {
			t.Errorf("默认密码不正确: got %s, want admin", settings.AdminPassword)
		}
	})

	// 更新设置
	t.Run("UpdateSettings", func(t *testing.T) {
		newPassword := "new-password"
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

	// 保存和获取激活码
	t.Run("LicenseCode", func(t *testing.T) {
		code := "TEST-LICENSE-CODE"
		err := db.SaveLicenseCode(ctx, code)
		if err != nil {
			t.Fatalf("保存激活码失败: %v", err)
		}

		got, err := db.GetLicenseCode(ctx)
		if err != nil {
			t.Fatalf("获取激活码失败: %v", err)
		}
		if got != code {
			t.Errorf("激活码不匹配: got %s, want %s", got, code)
		}
	})
}

// TestRequestLogs 测试请求日志
func TestRequestLogs(t *testing.T) {
	db := setupTestDB(t)
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

	// 获取请求日志数量
	t.Run("GetRequestLogsCount", func(t *testing.T) {
		count, err := db.GetRequestLogsCount(ctx, nil)
		if err != nil {
			t.Fatalf("获取请求日志数量失败: %v", err)
		}
		if count == 0 {
			t.Error("请求日志数量为 0")
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

// TestBlockedIPs 测试 IP 封禁
func TestBlockedIPs(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	testIP := "10.0.0.1"

	// 封禁 IP
	t.Run("BlockIP", func(t *testing.T) {
		err := db.BlockIP(ctx, testIP, "测试封禁")
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

	// 获取封禁 IP 列表
	t.Run("GetBlockedIPs", func(t *testing.T) {
		ips, err := db.GetBlockedIPs(ctx)
		if err != nil {
			t.Fatalf("获取封禁 IP 列表失败: %v", err)
		}
		if len(ips) == 0 {
			t.Error("封禁 IP 列表为空")
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

// TestBackupRestore 测试备份和恢复
func TestBackupRestore(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// 创建测试数据
	acc := &models.Account{
		ID:           uuid.New().String(),
		ClientID:     "backup-test-client",
		ClientSecret: "backup-test-secret",
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

		// 清空数据库（通过创建新的内存数据库模拟）
		db2 := setupTestDB(t)
		defer db2.Close()

		// 恢复数据
		err := db2.RestoreData(ctx, backup)
		if err != nil {
			t.Fatalf("恢复数据失败: %v", err)
		}

		// 验证恢复
		accounts, _ := db2.ListAccounts(ctx, nil, "created_at", true)
		if len(accounts) == 0 {
			t.Error("恢复后账号列表为空")
		}
	})
}

// TestAccountStats 测试账号统计
func TestAccountStats(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// 创建测试账号
	acc := &models.Account{
		ID:           uuid.New().String(),
		ClientID:     "stats-test-client",
		ClientSecret: "stats-test-secret",
		CreatedAt:    models.CurrentTime(),
		UpdatedAt:    models.CurrentTime(),
		Enabled:      true,
	}
	db.CreateAccount(ctx, acc)

	// 更新成功统计
	t.Run("UpdateStats_Success", func(t *testing.T) {
		err := db.UpdateStats(ctx, acc.ID, true)
		if err != nil {
			t.Fatalf("更新成功统计失败: %v", err)
		}

		got, _ := db.GetAccount(ctx, acc.ID)
		if got.SuccessCount != 1 {
			t.Errorf("成功计数不正确: got %d, want 1", got.SuccessCount)
		}
	})

	// 更新失败统计
	t.Run("UpdateStats_Failure", func(t *testing.T) {
		err := db.UpdateStats(ctx, acc.ID, false)
		if err != nil {
			t.Fatalf("更新失败统计失败: %v", err)
		}

		got, _ := db.GetAccount(ctx, acc.ID)
		if got.ErrorCount != 1 {
			t.Errorf("错误计数不正确: got %d, want 1", got.ErrorCount)
		}
	})

	// 重置所有账号统计
	t.Run("ResetAllAccountStats", func(t *testing.T) {
		err := db.ResetAllAccountStats(ctx)
		if err != nil {
			t.Fatalf("重置账号统计失败: %v", err)
		}

		got, _ := db.GetAccount(ctx, acc.ID)
		if got.SuccessCount != 0 || got.ErrorCount != 0 {
			t.Errorf("统计未重置: success=%d, error=%d", got.SuccessCount, got.ErrorCount)
		}
	})
}

// TestUserTokenUsage 测试用户 Token 使用量
func TestUserTokenUsage(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// 创建测试用户
	user := &models.User{
		ID:           uuid.New().String(),
		Name:         "Token Usage Test",
		APIKey:       "token-usage-" + uuid.New().String(),
		CreatedAt:    models.CurrentTime(),
		UpdatedAt:    models.CurrentTime(),
		Enabled:      true,
		DailyQuota:   10000,
		MonthlyQuota: 100000,
	}
	db.CreateUser(ctx, user)

	// 更新 Token 使用量
	t.Run("UpdateTokenUsage", func(t *testing.T) {
		err := db.UpdateTokenUsage(ctx, user.ID, 100, 200)
		if err != nil {
			t.Fatalf("更新 Token 使用量失败: %v", err)
		}

		// 再次更新
		err = db.UpdateTokenUsage(ctx, user.ID, 50, 100)
		if err != nil {
			t.Fatalf("第二次更新 Token 使用量失败: %v", err)
		}
	})

	// 检查配额
	t.Run("CheckUserQuota", func(t *testing.T) {
		allowed, reason, err := db.CheckUserQuota(ctx, user.ID)
		if err != nil {
			t.Fatalf("检查用户配额失败: %v", err)
		}
		if !allowed {
			t.Errorf("用户应该有配额: %s", reason)
		}
	})

	// 获取用户统计
	t.Run("GetUserStats", func(t *testing.T) {
		stats, err := db.GetUserStats(ctx, user.ID, 7)
		if err != nil {
			t.Fatalf("获取用户统计失败: %v", err)
		}
		if stats.UserID != user.ID {
			t.Errorf("用户 ID 不匹配: got %s, want %s", stats.UserID, user.ID)
		}
	})
}

