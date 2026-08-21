// Package settingsplatform contains persistent adapters for versioned settings.
package settingsplatform

import (
	"context"
	"errors"
	"time"

	"example.com/gin-vben-admin/server/internal/application/settings"
	"example.com/gin-vben-admin/server/internal/platform/persistence/gormdb"
	"gorm.io/gorm"
)

type GORMRepository struct{ db *gormdb.Store }

func NewGORMRepository(db *gormdb.Store) *GORMRepository { return &GORMRepository{db: db} }

type settingVersionRecord struct {
	ID        uint64    `gorm:"column:id;primaryKey"`
	Key       string    `gorm:"column:key"`
	Value     []byte    `gorm:"column:value;type:json"`
	Version   int64     `gorm:"column:version"`
	Sensitive bool      `gorm:"column:sensitive"`
	UpdatedBy string    `gorm:"column:updated_by"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (settingVersionRecord) TableName() string { return "setting_versions" }

func (r *GORMRepository) Current(ctx context.Context, key string) (settings.StoredSetting, error) {
	if r == nil || r.db == nil {
		return settings.StoredSetting{}, errors.New("settings repository is not initialized")
	}
	var record settingVersionRecord
	err := r.db.Read(ctx).Where("key = ?", key).Order("version DESC").First(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return settings.StoredSetting{}, settings.ErrSettingNotFound
	}
	if err != nil {
		return settings.StoredSetting{}, err
	}
	return toStored(record), nil
}

func (r *GORMRepository) Append(ctx context.Context, value settings.StoredSetting) (settings.StoredSetting, error) {
	if r == nil || r.db == nil {
		return settings.StoredSetting{}, errors.New("settings repository is not initialized")
	}
	var record settingVersionRecord
	err := r.db.Write(ctx).Transaction(func(tx *gorm.DB) error {
		var current int64
		if err := tx.Model(&settingVersionRecord{}).Where("key = ?", value.Key).Select("COALESCE(MAX(version), 0)").Scan(&current).Error; err != nil {
			return err
		}
		record = fromStored(value)
		record.Version = current + 1
		return tx.Create(&record).Error
	})
	if err != nil {
		return settings.StoredSetting{}, err
	}
	return toStored(record), nil
}

func (r *GORMRepository) History(ctx context.Context, key string) ([]settings.StoredSetting, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("settings repository is not initialized")
	}
	var records []settingVersionRecord
	if err := r.db.Read(ctx).Where("key = ?", key).Order("version ASC").Find(&records).Error; err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, settings.ErrSettingNotFound
	}
	out := make([]settings.StoredSetting, 0, len(records))
	for _, record := range records {
		out = append(out, toStored(record))
	}
	return out, nil
}

func toStored(record settingVersionRecord) settings.StoredSetting {
	return settings.StoredSetting{Key: record.Key, RawValue: append([]byte(nil), record.Value...), Version: record.Version, Sensitive: record.Sensitive, UpdatedBy: record.UpdatedBy, UpdatedAt: record.UpdatedAt}
}

func fromStored(value settings.StoredSetting) settingVersionRecord {
	return settingVersionRecord{Key: value.Key, Value: append([]byte(nil), value.RawValue...), Version: value.Version, Sensitive: value.Sensitive, UpdatedBy: value.UpdatedBy, UpdatedAt: value.UpdatedAt}
}

var _ settings.Repository = (*GORMRepository)(nil)
