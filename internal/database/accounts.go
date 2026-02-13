package database

import (
	"context"
	"encoding/json"
	"fmt"
	"claude-api/internal/logger"
	"claude-api/internal/models"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CreateAccount 创建新账号
func (db *DB) CreateAccount(ctx context.Context, acc *models.Account) error {
	logger.Debug("数据库: 创建账号 - ID: %s, 标签: %v", acc.ID, acc.Label)

	if err := db.gorm.WithContext(ctx).Create(acc).Error; err != nil {
		logger.Debug("数据库: 创建账号失败 - ID: %s, 错误: %v", acc.ID, err)
		return err
	}

	logger.Debug("数据库: 账号创建成功 - ID: %s", acc.ID)
	return nil
}

// GetAccount 根据 ID 获取账号
func (db *DB) GetAccount(ctx context.Context, id string) (*models.Account, error) {
	logger.Debug("数据库: 查询账号 - ID: %s", id)

	var acc models.Account
	err := db.gorm.WithContext(ctx).Where("id = ?", id).First(&acc).Error
	if err == gorm.ErrRecordNotFound {
		logger.Debug("数据库: 账号不存在 - ID: %s", id)
		return nil, nil
	}
	if err != nil {
		logger.Debug("数据库: 查询账号失败 - ID: %s, 错误: %v", id, err)
		return nil, err
	}

	logger.Debug("数据库: 账号查询成功 - ID: %s", id)
	return &acc, nil
}

// ListAccounts 列出账号（可选过滤条件）
func (db *DB) ListAccounts(ctx context.Context, enabled *bool, orderBy string, orderDesc bool) ([]*models.Account, error) {
	logger.Debug("数据库: 列出账号 - 启用状态: %v, 排序: %s %v", enabled, orderBy, orderDesc)

	query := db.gorm.WithContext(ctx).Model(&models.Account{})

	if enabled != nil {
		query = query.Where("enabled = ?", *enabled)
	}

	// 验证 orderBy 防止 SQL 注入
	validOrderBy := map[string]bool{"created_at": true, "id": true, "success_count": true}
	if !validOrderBy[orderBy] {
		orderBy = "created_at"
	}

	order := orderBy
	if orderDesc {
		order += " DESC"
	} else {
		order += " ASC"
	}
	query = query.Order(order)

	var accounts []*models.Account
	if err := query.Find(&accounts).Error; err != nil {
		logger.Debug("数据库: 列出账号查询失败 - 错误: %v", err)
		return nil, err
	}

	logger.Debug("数据库: 列出账号成功 - 数量: %d", len(accounts))
	return accounts, nil
}

// PaginationResult 分页查询结果
// @author ygw
type PaginationResult struct {
	Total    int64 // 总记录数
	Page     int   // 当前页码
	PageSize int   // 每页数量
	Pages    int   // 总页数
}

// ListAccountsWithPagination 分页列出账号
// @param ctx 上下文
// @param enabled 启用状态过滤（nil 表示不过滤）
// @param orderBy 排序字段
// @param orderDesc 是否降序
// @param page 页码（从1开始）
// @param pageSize 每页数量
// @return 账号列表、分页信息、错误
// @author ygw
func (db *DB) ListAccountsWithPagination(ctx context.Context, enabled *bool, orderBy string, orderDesc bool, page, pageSize int) ([]*models.Account, *PaginationResult, error) {
	logger.Debug("数据库: 分页列出账号 - 启用状态: %v, 排序: %s %v, 页码: %d, 每页: %d", enabled, orderBy, orderDesc, page, pageSize)

	// 参数校验
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 100
	}

	query := db.gorm.WithContext(ctx).Model(&models.Account{})

	if enabled != nil {
		query = query.Where("enabled = ?", *enabled)
	}

	// 先查询总数
	var total int64
	if err := query.Count(&total).Error; err != nil {
		logger.Debug("数据库: 分页列出账号统计失败 - 错误: %v", err)
		return nil, nil, err
	}

	// 计算总页数
	pages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		pages++
	}

	// 验证 orderBy 防止 SQL 注入
	// @author ygw - 扩展支持更多排序字段
	validOrderBy := map[string]bool{
		"created_at":    true,
		"id":            true,
		"success_count": true,
		"error_count":   true,
		"status":        true,
		"enabled":       true,
		"usage_current": true, // 配额使用量
	}
	if !validOrderBy[orderBy] {
		orderBy = "created_at"
	}

	order := orderBy
	if orderDesc {
		order += " DESC"
	} else {
		order += " ASC"
	}

	// 计算偏移量
	offset := (page - 1) * pageSize

	var accounts []*models.Account
	if err := query.Order(order).Offset(offset).Limit(pageSize).Find(&accounts).Error; err != nil {
		logger.Debug("数据库: 分页列出账号查询失败 - 错误: %v", err)
		return nil, nil, err
	}

	pagination := &PaginationResult{
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		Pages:    pages,
	}

	logger.Debug("数据库: 分页列出账号成功 - 数量: %d, 总数: %d, 页码: %d/%d", len(accounts), total, page, pages)
	return accounts, pagination, nil
}

// GetAccountCount 获取账号数量（轻量级查询）
func (db *DB) GetAccountCount(ctx context.Context) (int, error) {
	var count int64
	if err := db.gorm.WithContext(ctx).Model(&models.Account{}).Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

// UpdateAccount 更新账号
func (db *DB) UpdateAccount(ctx context.Context, id string, updates *models.AccountUpdate) error {
	logger.Debug("数据库: 更新账号 - ID: %s", id)

	updateMap := make(map[string]interface{})

	if updates.Label != nil {
		updateMap["label"] = updates.Label
	}
	if updates.ClientID != nil {
		updateMap["clientId"] = updates.ClientID
	}
	if updates.ClientSecret != nil {
		updateMap["clientSecret"] = updates.ClientSecret
	}
	if updates.RefreshToken != nil {
		updateMap["refreshToken"] = updates.RefreshToken
	}
	if updates.AccessToken != nil {
		updateMap["accessToken"] = updates.AccessToken
	}
	if updates.Other != nil {
		otherJSON, _ := json.Marshal(updates.Other)
		updateMap["other"] = otherJSON
	}
	if updates.Enabled != nil {
		updateMap["enabled"] = *updates.Enabled
	}
	if updates.Email != nil {
		updateMap["email"] = updates.Email
	}
	if updates.AuthMethod != nil {
		updateMap["auth_method"] = updates.AuthMethod
	}
	if updates.Region != nil {
		updateMap["region"] = updates.Region
	}
	if updates.QUserID != nil {
		updateMap["q_user_id"] = updates.QUserID
	}
	if updates.MachineID != nil {
		updateMap["machine_id"] = updates.MachineID
	}

	if len(updateMap) == 0 {
		logger.Debug("数据库: 更新账号无需更新 - ID: %s", id)
		return nil
	}

	updateMap["updated_at"] = models.CurrentTime()

	if err := db.gorm.WithContext(ctx).Model(&models.Account{}).Where("id = ?", id).Updates(updateMap).Error; err != nil {
		logger.Debug("数据库: 更新账号失败 - ID: %s, 错误: %v", id, err)
		return err
	}

	logger.Debug("数据库: 账号更新成功 - ID: %s, 更新字段数: %d", id, len(updateMap))
	return nil
}

// DeleteAccount 删除账号
func (db *DB) DeleteAccount(ctx context.Context, id string) error {
	logger.Debug("数据库: 删除账号 - ID: %s", id)

	result := db.gorm.WithContext(ctx).Where("id = ?", id).Delete(&models.Account{})
	if result.Error != nil {
		logger.Debug("数据库: 删除账号失败 - ID: %s, 错误: %v", id, result.Error)
		return result.Error
	}

	logger.Debug("数据库: 账号删除完成 - ID: %s, 影响行数: %d", id, result.RowsAffected)
	return nil
}

// DeleteAllAccounts 删除所有账号（安全保护机制触发时使用）
// @author ygw
func (db *DB) DeleteAllAccounts(ctx context.Context) (int64, error) {
	logger.Debug("数据库: 删除所有账号")

	result := db.gorm.WithContext(ctx).Where("1 = 1").Delete(&models.Account{})
	if result.Error != nil {
		logger.Debug("数据库: 删除所有账号失败 - 错误: %v", result.Error)
		return 0, result.Error
	}

	logger.Debug("数据库: 所有账号删除完成 - 影响行数: %d", result.RowsAffected)
	return result.RowsAffected, nil
}

// DeleteSuspendedAccounts 删除所有封控状态的账号
// @author ygw
func (db *DB) DeleteSuspendedAccounts(ctx context.Context) (int64, error) {
	logger.Debug("数据库: 删除所有封控账号")

	result := db.gorm.WithContext(ctx).Where("status = ?", models.AccountStatusSuspended).Delete(&models.Account{})
	if result.Error != nil {
		logger.Debug("数据库: 删除封控账号失败 - 错误: %v", result.Error)
		return 0, result.Error
	}

	logger.Debug("数据库: 封控账号删除完成 - 影响行数: %d", result.RowsAffected)
	return result.RowsAffected, nil
}

// QuotaStats 配额统计结果
// @author ygw
type QuotaStats struct {
	TotalUsage float64 // 总使用量
	TotalLimit float64 // 总配额
	Count      int64   // 有配额数据的账号数
}

// GetTotalQuotaStats 获取所有账号的配额统计
// @author ygw
func (db *DB) GetTotalQuotaStats(ctx context.Context) (*QuotaStats, error) {
	logger.Debug("数据库: 获取总配额统计")

	var result struct {
		TotalUsage float64
		TotalLimit float64
		Count      int64
	}

	err := db.gorm.WithContext(ctx).Model(&models.Account{}).
		Select("COALESCE(SUM(usage_current), 0) as total_usage, COALESCE(SUM(usage_limit), 0) as total_limit, COUNT(CASE WHEN usage_limit > 0 THEN 1 END) as count").
		Scan(&result).Error

	if err != nil {
		logger.Debug("数据库: 获取总配额统计失败 - 错误: %v", err)
		return nil, err
	}

	logger.Debug("数据库: 总配额统计 - 使用: %.2f, 限制: %.2f, 账号数: %d", result.TotalUsage, result.TotalLimit, result.Count)
	return &QuotaStats{
		TotalUsage: result.TotalUsage,
		TotalLimit: result.TotalLimit,
		Count:      result.Count,
	}, nil
}

// AccountStats 账号统计结果
// @author ygw
type AccountStats struct {
	TotalCount   int64 // 总账号数
	EnabledCount int64 // 启用账号数
	SuccessTotal int64 // 总成功请求数
	ErrorTotal   int64 // 总失败请求数
}

// GetAccountStats 获取所有账号的统计数据
// @author ygw
func (db *DB) GetAccountStats(ctx context.Context) (*AccountStats, error) {
	logger.Debug("数据库: 获取账号统计")

	var result AccountStats

	// 查询总数和启用数
	err := db.gorm.WithContext(ctx).Model(&models.Account{}).
		Select("COUNT(*) as total_count, COUNT(CASE WHEN enabled = 1 THEN 1 END) as enabled_count").
		Scan(&result).Error
	if err != nil {
		logger.Debug("数据库: 获取账号数量统计失败 - 错误: %v", err)
		return nil, err
	}

	// 查询成功/失败总数
	var sumResult struct {
		SuccessTotal int64
		ErrorTotal   int64
	}
	err = db.gorm.WithContext(ctx).Model(&models.Account{}).
		Select("COALESCE(SUM(success_count), 0) as success_total, COALESCE(SUM(error_count), 0) as error_total").
		Scan(&sumResult).Error
	if err != nil {
		logger.Debug("数据库: 获取账号请求统计失败 - 错误: %v", err)
		return nil, err
	}

	result.SuccessTotal = sumResult.SuccessTotal
	result.ErrorTotal = sumResult.ErrorTotal

	logger.Debug("数据库: 账号统计 - 总数: %d, 启用: %d, 成功: %d, 失败: %d",
		result.TotalCount, result.EnabledCount, result.SuccessTotal, result.ErrorTotal)
	return &result, nil
}

// ResetAllAccountStats 清除所有账号的成功/失败计数
func (db *DB) ResetAllAccountStats(ctx context.Context) error {
	logger.Debug("数据库: 清除所有账号统计计数")

	// 使用 Where("1 = 1") 来更新所有记录
	err := db.gorm.WithContext(ctx).Model(&models.Account{}).Where("1 = 1").Updates(map[string]interface{}{
		"success_count": 0,
		"error_count":   0,
		"updated_at":    models.CurrentTime(),
	}).Error

	if err != nil {
		logger.Debug("数据库: 清除账号统计失败: %v", err)
		return err
	}

	logger.Debug("数据库: 所有账号统计计数已清除")
	return nil
}

// UpdateTokens 更新访问令牌和刷新令牌
func (db *DB) UpdateTokens(ctx context.Context, id string, accessToken, refreshToken, status string) error {
	logger.Debug("数据库: 更新账号令牌 - ID: %s, 状态: %s", id, status)

	now := models.CurrentTime()
	updateMap := map[string]interface{}{
		"accessToken":         accessToken,
		"refreshToken":        refreshToken,
		"last_refresh_time":   now,
		"last_refresh_status": status,
		"updated_at":          now,
	}

	// 如果刷新成功，将账号状态恢复为 normal（修复：之前 expired 的账号刷新后无法使用）
	if status == "success" && accessToken != "" {
		updateMap["status"] = models.AccountStatusNormal
		updateMap["enabled"] = true
		logger.Info("账号 %s 令牌刷新成功，状态恢复为 normal", id)
	}

	err := db.gorm.WithContext(ctx).Model(&models.Account{}).Where("id = ?", id).Updates(updateMap).Error

	if err != nil {
		logger.Debug("数据库: 更新令牌失败 - ID: %s, 错误: %v", id, err)
		return err
	}

	logger.Debug("数据库: 令牌更新成功 - ID: %s", id)
	return nil
}

// UpdateQUserID 更新账号的 Q 用户 ID，标签为空时用 userId 填充
func (db *DB) UpdateQUserID(ctx context.Context, id string, qUserID string, label string) error {
	logger.Debug("数据库: 更新 Q 用户 ID - 账号ID: %s, QUserID: %s, Label: %s", id, qUserID, label)

	// 先获取当前账号
	var acc models.Account
	if err := db.gorm.WithContext(ctx).Where("id = ?", id).First(&acc).Error; err != nil {
		return err
	}

	updateMap := map[string]interface{}{
		"q_user_id":  qUserID,
		"updated_at": models.CurrentTime(),
	}

	// 如果当前标签为空，用 qUserID 填充
	if acc.Label == nil || *acc.Label == "" {
		updateMap["label"] = label
	}

	return db.gorm.WithContext(ctx).Model(&models.Account{}).Where("id = ?", id).Updates(updateMap).Error
}

// GetAccountByQUserID 根据 Q 用户 ID 查找账号
func (db *DB) GetAccountByQUserID(ctx context.Context, qUserID string) (*models.Account, error) {
	var acc models.Account
	err := db.gorm.WithContext(ctx).Where("q_user_id = ?", qUserID).First(&acc).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &acc, nil
}

// FindDuplicateAccounts 查找重复的账号（相同 q_user_id）
// @author ygw
func (db *DB) FindDuplicateAccounts(ctx context.Context) (map[string][]string, error) {
	type DuplicateResult struct {
		QUserID    string
		AccountIDs string
	}

	var results []DuplicateResult

	// 使用原生 SQL 查询重复账号（GROUP_CONCAT 在 SQLite 和 MySQL 都支持）
	query := `SELECT q_user_id, GROUP_CONCAT(id) as account_ids
		FROM accounts
		WHERE q_user_id IS NOT NULL AND q_user_id != ''
		GROUP BY q_user_id
		HAVING COUNT(*) > 1`

	if err := db.gorm.WithContext(ctx).Raw(query).Scan(&results).Error; err != nil {
		return nil, err
	}

	duplicates := make(map[string][]string)
	for _, r := range results {
		if r.QUserID != "" && r.AccountIDs != "" {
			duplicates[r.QUserID] = splitString(r.AccountIDs, ",")
		}
	}

	return duplicates, nil
}

// UpdateStats 更新成功/错误计数
// @author ygw
func (db *DB) UpdateStats(ctx context.Context, id string, success bool) error {
	if success {
		logger.Debug("数据库: 更新账号统计 - ID: %s, 成功计数+1, 错误计数归零", id)
		return db.gorm.WithContext(ctx).Model(&models.Account{}).Where("id = ?", id).Updates(map[string]interface{}{
			"success_count": gorm.Expr("success_count + 1"),
			"error_count":   0,
			"updated_at":    models.CurrentTime(),
		}).Error
	}

	// 失败：使用单次原子操作，递增 error_count 并在达到阈值时自动禁用
	maxErrorCount := db.cfg.MaxErrorCount
	logger.Debug("数据库: 更新账号错误计数 - ID: %s, 阈值: %d", id, maxErrorCount)

	result := db.gorm.WithContext(ctx).Model(&models.Account{}).Where("id = ?", id).Updates(map[string]interface{}{
		"error_count": gorm.Expr("error_count + 1"),
		"enabled":     gorm.Expr("CASE WHEN error_count + 1 >= ? THEN 0 ELSE enabled END", maxErrorCount),
		"updated_at":  models.CurrentTime(),
	})

	if result.Error != nil {
		logger.Debug("数据库: 更新账号错误计数失败 - ID: %s, 错误: %v", id, result.Error)
		return result.Error
	}

	// 检查是否触发了自动禁用（通过查询确认）
	if result.RowsAffected > 0 {
		var acc models.Account
		if err := db.gorm.WithContext(ctx).Select("error_count", "enabled").Where("id = ?", id).First(&acc).Error; err == nil {
			if !acc.Enabled && acc.ErrorCount >= maxErrorCount {
				logger.Info("数据库: 账号错误次数达到上限 - ID: %s, 错误次数: %d, 已自动禁用", id, acc.ErrorCount)
			}
		}
	}

	return nil
}

// ListIncompleteAccounts 列出信息不全的账号（缺少 clientId 或 clientSecret）
func (db *DB) ListIncompleteAccounts(ctx context.Context) ([]*models.Account, error) {
	logger.Debug("数据库: 列出信息不全的账号")

	var accounts []*models.Account
	err := db.gorm.WithContext(ctx).
		Where("clientId IS NULL OR clientId = '' OR clientSecret IS NULL OR clientSecret = ''").
		Find(&accounts).Error

	if err != nil {
		logger.Debug("数据库: 列出信息不全账号查询失败 - 错误: %v", err)
		return nil, err
	}

	logger.Debug("数据库: 列出信息不全账号成功 - 数量: %d", len(accounts))
	return accounts, nil
}

// DeleteIncompleteAccounts 删除所有信息不全的账号
func (db *DB) DeleteIncompleteAccounts(ctx context.Context) (int64, error) {
	logger.Debug("数据库: 删除所有信息不全的账号")

	result := db.gorm.WithContext(ctx).
		Where("clientId IS NULL OR clientId = '' OR clientSecret IS NULL OR clientSecret = ''").
		Delete(&models.Account{})

	if result.Error != nil {
		logger.Debug("数据库: 删除信息不全账号失败 - 错误: %v", result.Error)
		return 0, result.Error
	}

	logger.Debug("数据库: 信息不全账号删除完成 - 影响行数: %d", result.RowsAffected)
	return result.RowsAffected, nil
}

// CreateImportedAccount 创建导入账号备份记录
func (db *DB) CreateImportedAccount(ctx context.Context, acc *models.ImportedAccount) error {
	logger.Debug("数据库: 创建导入账号备份 - ID: %s, Email: %v", acc.ID, acc.Email)

	if acc.ID == "" {
		acc.ID = uuid.New().String()
	}

	if err := db.gorm.WithContext(ctx).Create(acc).Error; err != nil {
		logger.Error("数据库: 创建导入账号备份失败 - ID: %s, 错误: %v", acc.ID, err)
		return err
	}

	logger.Debug("数据库: 导入账号备份创建成功 - ID: %s", acc.ID)
	return nil
}

// ListImportedAccounts 列出所有导入的账号备份
func (db *DB) ListImportedAccounts(ctx context.Context) ([]*models.ImportedAccount, error) {
	logger.Debug("数据库: 列出导入账号备份")

	var accounts []*models.ImportedAccount
	if err := db.gorm.WithContext(ctx).Order("imported_at DESC").Find(&accounts).Error; err != nil {
		return nil, err
	}

	logger.Debug("数据库: 列出导入账号备份成功 - 数量: %d", len(accounts))
	return accounts, nil
}

// GetImportedAccountByQUserID 根据 Q 用户 ID 获取导入的账号备份
func (db *DB) GetImportedAccountByQUserID(ctx context.Context, qUserID string) (*models.ImportedAccount, error) {
	logger.Debug("数据库: 查询导入账号备份 - QUserID: %s", qUserID)

	var acc models.ImportedAccount
	err := db.gorm.WithContext(ctx).Where("q_user_id = ?", qUserID).First(&acc).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &acc, nil
}

// splitString 分割字符串
func splitString(s, sep string) []string {
	if s == "" {
		return nil
	}
	result := []string{}
	for _, part := range splitStringBySep(s, sep) {
		result = append(result, trimSpace(part))
	}
	return result
}

func splitStringBySep(s, sep string) []string {
	var result []string
	start := 0
	for i := 0; i < len(s); i++ {
		if string(s[i]) == sep {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// 兼容旧代码的类型别名
type ImportedAccount = models.ImportedAccount

// boolToInt 将布尔值转换为整数（兼容旧代码）
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// boolToString 将布尔值转换为字符串
func boolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// boolPtrToInt 将布尔指针转换为整数
func boolPtrToInt(b *bool) interface{} {
	if b == nil {
		return nil
	}
	if *b {
		return 1
	}
	return 0
}

// getString 从 map 中获取字符串
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// getStringPtr 从 map 中获取字符串指针
func getStringPtr(m map[string]interface{}, key string) *string {
	if v, ok := m[key].(string); ok && v != "" {
		return &v
	}
	return nil
}

// getBool 从 map 中获取布尔值
func getBool(m map[string]interface{}, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

// getInt 从 map 中获取整数
func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}

// getStringFallback 从 map 中获取字符串（支持多个 key）
func getStringFallback(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v := getString(m, key); v != "" {
			return v
		}
	}
	return ""
}

// getStringPtrFallback 从 map 中获取字符串指针（支持多个 key）
func getStringPtrFallback(m map[string]interface{}, keys ...string) *string {
	for _, key := range keys {
		if v := getStringPtr(m, key); v != nil {
			return v
		}
	}
	return nil
}

// getIntFallback 从 map 中获取整数（支持多个 key）
func getIntFallback(m map[string]interface{}, keys ...string) int {
	for _, key := range keys {
		if v := getInt(m, key); v != 0 {
			return v
		}
	}
	return 0
}

// getFloatFallback 从 map 中获取浮点数（支持多个 key）
func getFloatFallback(m map[string]interface{}, keys ...string) float64 {
	for _, key := range keys {
		if v, ok := m[key].(float64); ok {
			return v
		}
	}
	return 0
}

// currentTime 返回当前时间字符串
func currentTime() string {
	return models.CurrentTime()
}

// tableExists 检查表是否存在
func (db *DB) tableExists(ctx context.Context, tableName string) (bool, error) {
	if db.IsMySQL() {
		var count int64
		err := db.gorm.WithContext(ctx).Raw(
			"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?",
			tableName,
		).Scan(&count).Error
		return count > 0, err
	}

	// SQLite
	var count int64
	err := db.gorm.WithContext(ctx).Raw(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?",
		tableName,
	).Scan(&count).Error
	return count > 0, err
}

// columnExists 检查列是否存在
func (db *DB) columnExists(ctx context.Context, tableName, columnName string) (bool, error) {
	if db.IsMySQL() {
		var count int64
		err := db.gorm.WithContext(ctx).Raw(
			"SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?",
			tableName, columnName,
		).Scan(&count).Error
		return count > 0, err
	}

	// SQLite
	var count int64
	err := db.gorm.WithContext(ctx).Raw(
		fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('%s') WHERE name='%s'", tableName, columnName),
	).Scan(&count).Error
	return count > 0, err
}

// UpdateAccountStatus 更新账号状态
// @author ygw
func (db *DB) UpdateAccountStatus(ctx context.Context, id string, status string, reason string) error {
	logger.Debug("数据库: 更新账号状态 - ID: %s, 状态: %s, 原因: %s", id, status, reason)

	updateMap := map[string]interface{}{
		"status":     status,
		"updated_at": models.CurrentTime(),
	}

	if reason != "" {
		updateMap["status_reason"] = reason
	}

	// 如果是用尽状态，记录用尽时间
	if status == models.AccountStatusExhausted {
		updateMap["exhausted_at"] = models.CurrentTime()
	}

	// 如果恢复正常，清除用尽时间和原因
	if status == models.AccountStatusNormal {
		updateMap["exhausted_at"] = nil
		updateMap["status_reason"] = nil
		updateMap["error_count"] = 0
	}

	// 同步更新 enabled 字段（保持兼容性）
	updateMap["enabled"] = (status == models.AccountStatusNormal)

	return db.gorm.WithContext(ctx).Model(&models.Account{}).Where("id = ?", id).Updates(updateMap).Error
}

// ListAccountsByStatus 按状态列出账号
// @author ygw
func (db *DB) ListAccountsByStatus(ctx context.Context, status string, orderBy string, orderDesc bool) ([]*models.Account, error) {
	logger.Debug("数据库: 按状态列出账号 - 状态: %s, 排序: %s %v", status, orderBy, orderDesc)

	query := db.gorm.WithContext(ctx).Model(&models.Account{}).Where("status = ?", status)

	// 验证 orderBy 防止 SQL 注入
	validOrderBy := map[string]bool{"created_at": true, "id": true, "success_count": true}
	if !validOrderBy[orderBy] {
		orderBy = "created_at"
	}

	order := orderBy
	if orderDesc {
		order += " DESC"
	} else {
		order += " ASC"
	}
	query = query.Order(order)

	var accounts []*models.Account
	if err := query.Find(&accounts).Error; err != nil {
		logger.Debug("数据库: 按状态列出账号查询失败 - 错误: %v", err)
		return nil, err
	}

	logger.Debug("数据库: 按状态列出账号成功 - 数量: %d", len(accounts))
	return accounts, nil
}

// RecoverExhaustedAccounts 恢复用尽超过30天的账号
// @author ygw
func (db *DB) RecoverExhaustedAccounts(ctx context.Context) (int64, error) {
	// 计算30天前的时间
	thirtyDaysAgo := time.Now().AddDate(0, -1, 0).Format(models.TimeFormat)

	logger.Debug("数据库: 恢复用尽账号 - 截止时间: %s", thirtyDaysAgo)

	result := db.gorm.WithContext(ctx).Model(&models.Account{}).
		Where("status = ? AND exhausted_at IS NOT NULL AND exhausted_at < ?",
			models.AccountStatusExhausted, thirtyDaysAgo).
		Updates(map[string]interface{}{
			"status":        models.AccountStatusNormal,
			"enabled":       true,
			"exhausted_at":  nil,
			"status_reason": nil,
			"error_count":   0,
			"updated_at":    models.CurrentTime(),
		})

	if result.Error != nil {
		logger.Error("数据库: 恢复用尽账号失败 - 错误: %v", result.Error)
		return 0, result.Error
	}

	if result.RowsAffected > 0 {
		logger.Info("数据库: 已恢复 %d 个用尽账号（超过30天自动恢复）", result.RowsAffected)
	}

	return result.RowsAffected, nil
}

// GetExhaustedAccountsCount 获取用尽状态的账号数量
// @author ygw
func (db *DB) GetExhaustedAccountsCount(ctx context.Context) (int64, error) {
	var count int64
	err := db.gorm.WithContext(ctx).Model(&models.Account{}).
		Where("status = ?", models.AccountStatusExhausted).
		Count(&count).Error
	return count, err
}

// UpdateAccountQuota 更新账号配额信息
// 使用重试机制处理 SQLite 高并发写入时的锁竞争
// @author ygw
func (db *DB) UpdateAccountQuota(ctx context.Context, id string, usageCurrent, usageLimit float64, subscriptionType string, tokenExpiry *int64) error {
	logger.Debug("数据库: 更新账号配额 - ID: %s, 使用量: %.2f/%.2f, 订阅类型: %s, 有效时间: %v", id, usageCurrent, usageLimit, subscriptionType, tokenExpiry)

	updateMap := map[string]interface{}{
		"usage_current":      usageCurrent,
		"usage_limit":        usageLimit,
		"quota_refreshed_at": models.CurrentTime(),
		"updated_at":         models.CurrentTime(),
	}

	if subscriptionType != "" {
		updateMap["subscription_type"] = subscriptionType
	}

	if tokenExpiry != nil {
		updateMap["token_expiry"] = *tokenExpiry
	}

	// 使用重试机制处理 SQLite 锁竞争
	return db.RetryOnLock(ctx, 5, func() error {
		return db.gorm.WithContext(ctx).Model(&models.Account{}).Where("id = ?", id).Updates(updateMap).Error
	})
}

// GetAccountsForQuotaRefresh 获取需要刷新配额的账号（状态为 normal 且有 access token）
// @author ygw
func (db *DB) GetAccountsForQuotaRefresh(ctx context.Context) ([]*models.Account, error) {
	var accounts []*models.Account
	err := db.gorm.WithContext(ctx).
		Where("status = ? AND accessToken IS NOT NULL AND accessToken != ''", models.AccountStatusNormal).
		Find(&accounts).Error
	return accounts, err
}


// EnableAllAccounts 批量启用所有账号（排除已封控的账号）
// @author ygw
func (db *DB) EnableAllAccounts(ctx context.Context) (int64, error) {
	logger.Debug("数据库: 批量启用所有账号（排除封控账号）")

	result := db.gorm.WithContext(ctx).Model(&models.Account{}).
		Where("status != ?", models.AccountStatusSuspended).
		Updates(map[string]interface{}{
			"enabled":    true,
			"status":     models.AccountStatusNormal,
			"updated_at": models.CurrentTime(),
		})

	if result.Error != nil {
		logger.Debug("数据库: 批量启用账号失败 - 错误: %v", result.Error)
		return 0, result.Error
	}

	logger.Debug("数据库: 批量启用账号完成 - 影响行数: %d", result.RowsAffected)
	return result.RowsAffected, nil
}

// DisableAllAccounts 批量禁用所有账号
// @author ygw
func (db *DB) DisableAllAccounts(ctx context.Context) (int64, error) {
	logger.Debug("数据库: 批量禁用所有账号")

	result := db.gorm.WithContext(ctx).Model(&models.Account{}).
		Where("1 = 1").
		Updates(map[string]interface{}{
			"enabled":    false,
			"status":     models.AccountStatusDisabled,
			"updated_at": models.CurrentTime(),
		})

	if result.Error != nil {
		logger.Debug("数据库: 批量禁用账号失败 - 错误: %v", result.Error)
		return 0, result.Error
	}

	logger.Debug("数据库: 批量禁用账号完成 - 影响行数: %d", result.RowsAffected)
	return result.RowsAffected, nil
}

// GetAccountStatsByStatus 获取各状态账号数量统计
// @author ygw
func (db *DB) GetAccountStatsByStatus(ctx context.Context) (map[string]int64, error) {
	logger.Debug("数据库: 获取各状态账号数量统计")

	type StatusCount struct {
		Status string
		Count  int64
	}

	var results []StatusCount
	err := db.gorm.WithContext(ctx).Model(&models.Account{}).
		Select("status, COUNT(*) as count").
		Group("status").
		Scan(&results).Error

	if err != nil {
		logger.Debug("数据库: 获取状态统计失败 - 错误: %v", err)
		return nil, err
	}

	stats := make(map[string]int64)
	for _, r := range results {
		stats[r.Status] = r.Count
	}

	logger.Debug("数据库: 状态统计 - %v", stats)
	return stats, nil
}

// ListAccountsWithPaginationAndStatus 分页列出账号（支持状态筛选）
// @author ygw
func (db *DB) ListAccountsWithPaginationAndStatus(ctx context.Context, status string, orderBy string, orderDesc bool, page, pageSize int) ([]*models.Account, *PaginationResult, error) {
	logger.Debug("数据库: 分页列出账号 - 状态: %s, 排序: %s %v, 页码: %d, 每页: %d", status, orderBy, orderDesc, page, pageSize)

	// 参数校验
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 100
	}

	query := db.gorm.WithContext(ctx).Model(&models.Account{})

	// 状态筛选
	if status != "" && status != "all" {
		query = query.Where("status = ?", status)
	}

	// 先查询总数
	var total int64
	if err := query.Count(&total).Error; err != nil {
		logger.Debug("数据库: 分页列出账号统计失败 - 错误: %v", err)
		return nil, nil, err
	}

	// 计算总页数
	pages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		pages++
	}

	// 验证 orderBy 防止 SQL 注入
	validOrderBy := map[string]bool{
		"created_at":    true,
		"id":            true,
		"success_count": true,
		"error_count":   true,
		"status":        true,
		"enabled":       true,
		"usage_current": true,
	}
	if !validOrderBy[orderBy] {
		orderBy = "created_at"
	}

	order := orderBy
	if orderDesc {
		order += " DESC"
	} else {
		order += " ASC"
	}

	// 计算偏移量
	offset := (page - 1) * pageSize

	var accounts []*models.Account
	if err := query.Order(order).Offset(offset).Limit(pageSize).Find(&accounts).Error; err != nil {
		logger.Debug("数据库: 分页列出账号查询失败 - 错误: %v", err)
		return nil, nil, err
	}

	pagination := &PaginationResult{
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		Pages:    pages,
	}

	logger.Debug("数据库: 分页列出账号成功 - 数量: %d, 总数: %d, 页码: %d/%d", len(accounts), total, page, pages)
	return accounts, pagination, nil
}

// ExportAllAccounts 导出所有账号（不分页）
// @author ygw
func (db *DB) ExportAllAccounts(ctx context.Context) ([]*models.Account, error) {
	logger.Debug("数据库: 导出所有账号")

	var accounts []*models.Account
	if err := db.gorm.WithContext(ctx).Order("created_at DESC").Find(&accounts).Error; err != nil {
		logger.Debug("数据库: 导出账号失败 - 错误: %v", err)
		return nil, err
	}

	logger.Debug("数据库: 导出账号成功 - 数量: %d", len(accounts))
	return accounts, nil
}
