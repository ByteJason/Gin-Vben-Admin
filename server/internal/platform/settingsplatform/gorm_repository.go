// Package settingsplatform contains persistent adapters for versioned settings.
package settingsplatform

import (
	"context"
	"errors"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/application/settings"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GORMRepository struct{ db *gormdb.Store }

func NewGORMRepository(db *gormdb.Store) *GORMRepository { return &GORMRepository{db: db} }

type settingVersionRecord = model.SettingVersion

func (r *GORMRepository) Current(ctx context.Context, key string) (settings.StoredSetting, error) {
	if r == nil || r.db == nil {
		return settings.StoredSetting{}, errors.New("settings repository is not initialized")
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return settings.StoredSetting{}, err
	}
	record, err := gorm.G[settingVersionRecord](r.db.Read(ctx)).Scopes(settingScope(scope.TenantID, key)).Order("version DESC").First(ctx)
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
	err = r.db.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var aggregate struct {
			Current int64 `gorm:"column:current"`
		}
		if err := gorm.G[settingVersionRecord](tx).Select("COALESCE(MAX(version), 0) AS current").Scopes(settingScope(scope.TenantID, value.Key)).Scan(ctx, &aggregate); err != nil {
			return err
		}
		record = fromStored(value)
		record.Version = aggregate.Current + 1
		record.TenantID = scope.TenantID
		if scope.Organization != "" {
			orgID := scope.Organization
			record.OrgID = &orgID
		}
		return gorm.G[settingVersionRecord](tx).Create(ctx, &record)
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
	records, err := gorm.G[settingVersionRecord](r.db.Read(ctx)).Scopes(settingScope(scope.TenantID, key)).Order("version ASC").Find(ctx)
	if err != nil {
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
	return settingVersionRecord{Key: value.Key, Value: model.JSONValue(append([]byte(nil), value.RawValue...)), Version: value.Version, Sensitive: value.Sensitive, Encrypted: value.Encrypted, Source: string(value.Source), UpdatedBy: value.UpdatedBy, UpdatedAt: value.UpdatedAt}
}

func settingScope(tenantID, key string) func(*gorm.Statement) {
	return func(statement *gorm.Statement) {
		statement.AddClause(clause.Where{Exprs: []clause.Expression{
			clause.Eq{Column: clause.Column{Name: "tenant_id"}, Value: tenantID},
			clause.Eq{Column: clause.Column{Name: "key"}, Value: key},
		}})
	}
}

var _ settings.Repository = (*GORMRepository)(nil)
