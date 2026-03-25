package database

import (
	"claude-api/internal/models"
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const licenseCodeSettingKey = "license_code"

// SaveLicenseCode 保存激活码到 settings 表。
func (db *DB) SaveLicenseCode(ctx context.Context, code string) error {
	setting := models.Setting{
		Key:   licenseCodeSettingKey,
		Value: code,
	}

	return db.gorm.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "setting_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"setting_value"}),
	}).Create(&setting).Error
}

// GetLicenseCode 从 settings 表读取激活码。
func (db *DB) GetLicenseCode(ctx context.Context) (string, error) {
	var setting models.Setting
	err := db.gorm.WithContext(ctx).
		Where("setting_key = ?", licenseCodeSettingKey).
		Take(&setting).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", err
	}

	return setting.Value, nil
}
