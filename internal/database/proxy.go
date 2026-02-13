package database

import (
	"context"
	"claude-api/internal/models"
)

// GetProxies 获取所有代理
// @return []*models.Proxy 代理列表
// @author ygw
func (db *DB) GetProxies(ctx context.Context) ([]*models.Proxy, error) {
	var proxies []*models.Proxy
	err := db.gorm.WithContext(ctx).Order("id ASC").Find(&proxies).Error
	return proxies, err
}

// GetEnabledProxies 获取启用的代理
// @return []*models.Proxy 启用的代理列表
// @author ygw
func (db *DB) GetEnabledProxies(ctx context.Context) ([]*models.Proxy, error) {
	var proxies []*models.Proxy
	err := db.gorm.WithContext(ctx).Where("enabled = ?", true).Order("id ASC").Find(&proxies).Error
	return proxies, err
}

// GetProxyByID 根据ID获取代理
// @param id 代理ID
// @return *models.Proxy 代理
// @author ygw
func (db *DB) GetProxyByID(ctx context.Context, id int64) (*models.Proxy, error) {
	var proxy models.Proxy
	err := db.gorm.WithContext(ctx).First(&proxy, id).Error
	if err != nil {
		return nil, err
	}
	return &proxy, nil
}

// CreateProxy 创建代理
// @param proxy 代理数据
// @author ygw
func (db *DB) CreateProxy(ctx context.Context, proxy *models.Proxy) error {
	proxy.CreatedAt = models.CurrentTime()
	proxy.UpdatedAt = models.CurrentTime()
	return db.gorm.WithContext(ctx).Create(proxy).Error
}

// UpdateProxy 更新代理
// @param id 代理ID
// @param updates 更新数据
// @author ygw
func (db *DB) UpdateProxy(ctx context.Context, id int64, updates map[string]interface{}) error {
	updates["updated_at"] = models.CurrentTime()
	return db.gorm.WithContext(ctx).Model(&models.Proxy{}).Where("id = ?", id).Updates(updates).Error
}

// DeleteProxy 删除代理
// @param id 代理ID
// @author ygw
func (db *DB) DeleteProxy(ctx context.Context, id int64) error {
	return db.gorm.WithContext(ctx).Delete(&models.Proxy{}, id).Error
}

// DeleteAllProxies 删除所有代理
// @author ygw
func (db *DB) DeleteAllProxies(ctx context.Context) error {
	return db.gorm.WithContext(ctx).Where("1 = 1").Delete(&models.Proxy{}).Error
}
