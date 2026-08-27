// Package model contains the persistence-side GORM models.
//
// Files are grouped by capability (shared, identity, admin, audit and
// infrastructure) while remaining one Go package so migrations and repositories
// share exactly one set of column definitions. Relationship IDs intentionally
// remain scalar fields; ORM read relations live outside the migration registry.
package model

import "time"

type SettingVersion struct {
	ID        uint64     `gorm:"column:id;primaryKey;autoIncrement;comment:设置记录标识"`
	TenantID  string     `gorm:"column:tenant_id;size:64;not null;default:default;index:idx_setting_versions_tenant,priority:1;uniqueIndex:uq_setting_versions_tenant_key_version,priority:1;index:idx_setting_versions_tenant_key_updated,priority:1;comment:租户标识"`
	OrgID     *string    `gorm:"column:org_id;size:64;index:idx_setting_versions_tenant,priority:2;comment:组织标识"`
	Key       string     `gorm:"column:key;size:191;not null;uniqueIndex:uq_setting_versions_tenant_key_version,priority:2;index:idx_setting_versions_key_updated,priority:1;index:idx_setting_versions_tenant_key_updated,priority:2;comment:设置键"`
	Value     JSONValue  `gorm:"column:value;not null;comment:设置值"`
	Version   int64      `gorm:"column:version;not null;uniqueIndex:uq_setting_versions_tenant_key_version,priority:3;comment:版本号"`
	Sensitive bool       `gorm:"column:sensitive;not null;default:false;comment:是否敏感"`
	Encrypted bool       `gorm:"column:encrypted;not null;default:false;comment:是否加密"`
	Source    string     `gorm:"column:source;size:16;not null;default:database;comment:设置来源"`
	UpdatedBy string     `gorm:"column:updated_by;size:191;not null;comment:更新人"`
	CreatedAt time.Time  `gorm:"column:created_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:创建时间"`
	UpdatedAt time.Time  `gorm:"column:updated_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);index:idx_setting_versions_key_updated,priority:2;index:idx_setting_versions_tenant_key_updated,priority:3;comment:更新时间"`
	DeletedAt *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
}

func (SettingVersion) TableName() string { return "setting_versions" }
