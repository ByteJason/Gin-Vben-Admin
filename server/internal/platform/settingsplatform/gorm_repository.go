// Package settingsplatform contains persistent adapters for versioned settings.
package settingsplatform

import (
	"context"
	"errors"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/application/settings"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GORMRepository struct{ db *gormdb.Store }

func NewGORMRepository(db *gormdb.Store) *GORMRepository { return &GORMRepository{db: db} }

type settingVersionRecord struct {
	ID        uint64    `gorm:"column:id;primaryKey"`
	Key       string    `gorm:"column:key"`
	Value     []byte    `gorm:"column:value;type:json"`
	Version   int64     `gorm:"column:version"`
	Sensitive bool      `gorm:"column:sensitive"`
	Encrypted bool      `gorm:"column:encrypted"`
	Source    string    `gorm:"column:source"`
	UpdatedBy string    `gorm:"column:updated_by"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
	TenantID  string    `gorm:"column:tenant_id"`
	OrgID     string    `gorm:"column:org_id"`
}

func (settingVersionRecord) TableName() string { return "setting_versions" }

func (r *GORMRepository) Current(ctx context.Context, key string) (settings.StoredSetting, error) {
	if r == nil || r.db == nil {
		return settings.StoredSetting{}, errors.New("settings repository is not initialized")
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return settings.StoredSetting{}, err
	}
	var record settingVersionRecord
	err = settingScope(r.db.Read(ctx), scope.TenantID, key).Order("version DESC").First(&record).Error
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
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return settings.StoredSetting{}, err
	}
	var record settingVersionRecord
	err = r.db.Write(ctx).Transaction(func(tx *gorm.DB) error {
		var current int64
		if err := settingScope(tx.Model(&settingVersionRecord{}), scope.TenantID, value.Key).Select("COALESCE(MAX(version), 0)").Scan(&current).Error; err != nil {
			return err
		}
		record = fromStored(value)
		record.Version = current + 1
		record.TenantID = scope.TenantID
		record.OrgID = scope.Organization
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
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	var records []settingVersionRecord
	if err := settingScope(r.db.Read(ctx), scope.TenantID, key).Order("version ASC").Find(&records).Error; err != nil {
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
	return settings.StoredSetting{Key: record.Key, RawValue: append([]byte(nil), record.Value...), Version: record.Version, Sensitive: record.Sensitive, Encrypted: record.Encrypted, Source: settings.Source(record.Source), UpdatedBy: record.UpdatedBy, UpdatedAt: record.UpdatedAt}
}

func fromStored(value settings.StoredSetting) settingVersionRecord {
	return settingVersionRecord{Key: value.Key, Value: append([]byte(nil), value.RawValue...), Version: value.Version, Sensitive: value.Sensitive, Encrypted: value.Encrypted, Source: string(value.Source), UpdatedBy: value.UpdatedBy, UpdatedAt: value.UpdatedAt}
}

func settingScope(db *gorm.DB, tenantID, key string) *gorm.DB {
	return db.Where("tenant_id = ?", tenantID).Where(clause.Eq{Column: clause.Column{Name: "key"}, Value: key})
}

var _ settings.Repository = (*GORMRepository)(nil)
