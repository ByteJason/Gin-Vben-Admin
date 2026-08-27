// Package model contains the persistence-side GORM models.
//
// Files are grouped by capability (shared, identity, admin, audit and
// infrastructure) while remaining one Go package so migrations and repositories
// share exactly one set of column definitions. Relationship IDs intentionally
// remain scalar fields; ORM read relations live outside the migration registry.
package model

import "time"

type DictionaryType struct {
	ID          string     `gorm:"column:id;size:64;primaryKey;comment:字典类型标识"`
	TenantID    string     `gorm:"column:tenant_id;size:128;not null;default:'';uniqueIndex:uq_dictionary_types_scope_code,priority:1;index:idx_dictionary_types_scope_status,priority:1;comment:租户标识"`
	OrgID       string     `gorm:"column:org_id;size:128;not null;default:'';uniqueIndex:uq_dictionary_types_scope_code,priority:2;index:idx_dictionary_types_scope_status,priority:2;comment:组织标识"`
	Code        string     `gorm:"column:code;size:191;not null;uniqueIndex:uq_dictionary_types_scope_code,priority:3;comment:字典编码"`
	NameZhCN    string     `gorm:"column:name_zh_cn;size:191;not null;default:'';comment:中文名称"`
	NameEnUS    string     `gorm:"column:name_en_us;size:191;not null;default:'';comment:英文名称"`
	Description string     `gorm:"column:description;size:512;not null;default:'';comment:描述"`
	Status      string     `gorm:"column:status;size:32;not null;default:active;index:idx_dictionary_types_scope_status,priority:3;check:chk_dictionary_types_status,status IN ('active','disabled');comment:状态"`
	SortOrder   int32      `gorm:"column:sort_order;not null;default:0;index:idx_dictionary_types_scope_status,priority:4;check:chk_dictionary_types_sort,sort_order >= 0;comment:排序值"`
	SystemOwned bool       `gorm:"column:system_owned;not null;default:false;comment:系统内置"`
	CreatedAt   time.Time  `gorm:"column:created_at;precision:6;not null;comment:创建时间"`
	UpdatedAt   time.Time  `gorm:"column:updated_at;precision:6;not null;comment:更新时间"`
	DeletedAt   *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
}

func (DictionaryType) TableName() string { return "dictionary_types" }

type DictionaryItem struct {
	ID          string     `gorm:"column:id;size:64;primaryKey;comment:字典项标识"`
	TenantID    string     `gorm:"column:tenant_id;size:128;not null;default:'';uniqueIndex:uq_dictionary_items_scope_value,priority:1;index:idx_dictionary_items_scope_type,priority:1;comment:租户标识"`
	OrgID       string     `gorm:"column:org_id;size:128;not null;default:'';uniqueIndex:uq_dictionary_items_scope_value,priority:2;index:idx_dictionary_items_scope_type,priority:2;comment:组织标识"`
	TypeCode    string     `gorm:"column:type_code;size:191;not null;uniqueIndex:uq_dictionary_items_scope_value,priority:3;index:idx_dictionary_items_scope_type,priority:3;comment:字典编码"`
	Value       string     `gorm:"column:item_value;size:191;not null;uniqueIndex:uq_dictionary_items_scope_value,priority:4;comment:字典值"`
	LabelZhCN   string     `gorm:"column:label_zh_cn;size:191;not null;default:'';comment:中文标签"`
	LabelEnUS   string     `gorm:"column:label_en_us;size:191;not null;default:'';comment:英文标签"`
	Description string     `gorm:"column:description;size:512;not null;default:'';comment:描述"`
	Tag         string     `gorm:"column:tag;size:64;not null;default:'';comment:标签"`
	Status      string     `gorm:"column:status;size:32;not null;default:active;index:idx_dictionary_items_scope_type,priority:4;check:chk_dictionary_items_status,status IN ('active','disabled');comment:状态"`
	SortOrder   int32      `gorm:"column:sort_order;not null;default:0;index:idx_dictionary_items_scope_type,priority:5;check:chk_dictionary_items_sort,sort_order >= 0;comment:排序值"`
	SystemOwned bool       `gorm:"column:system_owned;not null;default:false;comment:系统内置"`
	CreatedAt   time.Time  `gorm:"column:created_at;precision:6;not null;comment:创建时间"`
	UpdatedAt   time.Time  `gorm:"column:updated_at;precision:6;not null;comment:更新时间"`
	DeletedAt   *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
}

func (DictionaryItem) TableName() string { return "dictionary_items" }

type DictionaryCacheVersion struct {
	ID        string     `gorm:"column:id;size:64;primaryKey;comment:缓存版本标识"`
	TenantID  string     `gorm:"column:tenant_id;size:128;not null;default:'';uniqueIndex:uq_dictionary_cache_scope_type,priority:1;comment:租户标识"`
	OrgID     string     `gorm:"column:org_id;size:128;not null;default:'';uniqueIndex:uq_dictionary_cache_scope_type,priority:2;comment:组织标识"`
	TypeCode  string     `gorm:"column:type_code;size:191;not null;uniqueIndex:uq_dictionary_cache_scope_type,priority:3;comment:字典编码"`
	Version   uint64     `gorm:"column:version;not null;default:0;check:chk_dictionary_cache_version,version >= 0;comment:缓存版本"`
	CreatedAt time.Time  `gorm:"column:created_at;precision:6;not null;comment:创建时间"`
	UpdatedAt time.Time  `gorm:"column:updated_at;precision:6;not null;comment:更新时间"`
	DeletedAt *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
}

func (DictionaryCacheVersion) TableName() string { return "dictionary_cache_versions" }
