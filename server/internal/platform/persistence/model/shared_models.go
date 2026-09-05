// Package model contains the persistence-side GORM models.
//
// Files are grouped by capability (shared, identity, admin, audit and
// infrastructure) while remaining one Go package so migrations and repositories
// share exactly one set of column definitions. Relationship IDs intentionally
// remain scalar fields; ORM read relations live outside the migration registry.
package model

import "time"

type AppMetadata struct {
	MetadataKey   string     `gorm:"column:metadata_key;size:191;primaryKey;comment:元数据键"`
	MetadataValue JSONValue  `gorm:"column:metadata_value;not null;comment:元数据内容"`
	Version       uint64     `gorm:"column:version;not null;check:chk_gvba_sys_app_metadata_version,version > 0;comment:版本号"`
	CreatedAt     time.Time  `gorm:"column:created_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:创建时间"`
	UpdatedAt     time.Time  `gorm:"column:updated_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:更新时间"`
	DeletedAt     *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
}

func (AppMetadata) TableName() string { return "gvba_sys_app_metadata" }

type Tenant struct {
	ID        string     `gorm:"column:id;size:64;primaryKey;comment:租户标识"`
	Name      string     `gorm:"column:name;size:191;not null;uniqueIndex:uq_gvba_sys_tenants_name;comment:租户名称"`
	Status    string     `gorm:"column:status;size:32;not null;default:active;check:chk_gvba_sys_tenants_status,status IN ('active','disabled');comment:租户状态"`
	CreatedAt time.Time  `gorm:"column:created_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:创建时间"`
	UpdatedAt time.Time  `gorm:"column:updated_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:更新时间"`
	DeletedAt *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
}

func (Tenant) TableName() string { return "gvba_sys_tenants" }

type Organization struct {
	ID        string     `gorm:"column:id;size:64;primaryKey;comment:组织标识"`
	TenantID  string     `gorm:"column:tenant_id;size:64;not null;uniqueIndex:uq_gvba_sys_organizations_tenant_name,priority:1;index:idx_gvba_sys_organizations_tenant_parent,priority:1;comment:租户标识"`
	ParentID  *string    `gorm:"column:parent_id;size:64;index:idx_gvba_sys_organizations_tenant_parent,priority:2;comment:上级组织"`
	Name      string     `gorm:"column:name;size:191;not null;uniqueIndex:uq_gvba_sys_organizations_tenant_name,priority:2;comment:组织名称"`
	Status    string     `gorm:"column:status;size:32;not null;default:active;check:chk_gvba_sys_organizations_status,status IN ('active','disabled');comment:组织状态"`
	CreatedAt time.Time  `gorm:"column:created_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:创建时间"`
	UpdatedAt time.Time  `gorm:"column:updated_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:更新时间"`
	DeletedAt *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
}

func (Organization) TableName() string { return "gvba_sys_organizations" }
