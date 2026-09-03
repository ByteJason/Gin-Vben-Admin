// Package model contains the persistence-side GORM models.
//
// Files are grouped by capability (shared, identity, admin, audit and
// infrastructure) while remaining one Go package so migrations and repositories
// share exactly one set of column definitions. Relationship IDs intentionally
// remain scalar fields; ORM read relations live outside the migration registry.
package model

import "time"

type FileObject struct {
	ID                string     `gorm:"column:id;size:64;primaryKey;comment:文件标识"`
	TenantID          string     `gorm:"column:tenant_id;size:128;not null;uniqueIndex:uq_file_objects_tenant_key,priority:1;index:idx_file_objects_tenant_created,priority:1;comment:租户标识"`
	OrgID             *string    `gorm:"column:org_id;size:128;comment:组织标识"`
	ScopeType         string     `gorm:"column:scope_type;size:16;not null;default:tenant;index:idx_file_objects_scope_category_created,priority:1;index:idx_file_objects_scope_mime_created,priority:1;index:idx_file_objects_scope_owner_created,priority:1;comment:作用域类型"`
	CategoryID        *string    `gorm:"column:category_id;size:64;index:idx_file_objects_scope_category_created,priority:2;comment:媒体分类标识"`
	ProviderID        string     `gorm:"column:provider_id;size:64;not null;default:local;comment:存储提供商标识"`
	LifecycleStatus   string     `gorm:"column:lifecycle_status;size:16;not null;default:ready;index:idx_file_objects_status_created,priority:1;comment:生命周期状态"`
	MetadataJSON      JSONValue  `gorm:"column:metadata_json;not null;comment:媒体元数据"`
	OriginalExtension string     `gorm:"column:original_extension;size:32;not null;default:'';comment:原始扩展名"`
	DetectedMIME      *string    `gorm:"column:detected_mime;size:191;index:idx_file_objects_scope_mime_created,priority:2;comment:检测媒体类型"`
	ReconcileKey      *string    `gorm:"column:reconcile_key;size:191;uniqueIndex:uq_file_objects_reconcile_key;comment:预置对账键"`
	ObjectKey         string     `gorm:"column:object_key;size:255;not null;uniqueIndex:uq_file_objects_tenant_key,priority:2;comment:对象键"`
	Name              string     `gorm:"column:name;size:255;not null;comment:文件名称"`
	MIME              string     `gorm:"column:mime;size:191;not null;comment:媒体类型"`
	Size              int64      `gorm:"column:size;not null;comment:文件大小"`
	SHA256            *string    `gorm:"column:sha256;type:char(64);comment:内容摘要"`
	OwnerID           string     `gorm:"column:owner_id;size:128;not null;index:idx_file_objects_scope_owner_created,priority:2;comment:所有者标识"`
	ACL               string     `gorm:"column:acl;size:16;not null;default:private;comment:访问控制"`
	CreatedAt         time.Time  `gorm:"column:created_at;precision:6;not null;index:idx_file_objects_tenant_created,priority:2;index:idx_file_objects_scope_category_created,priority:3;index:idx_file_objects_scope_mime_created,priority:3;index:idx_file_objects_scope_owner_created,priority:3;index:idx_file_objects_status_created,priority:2;index:idx_file_objects_cleanup;comment:创建时间"`
	UpdatedAt         time.Time  `gorm:"column:updated_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:更新时间"`
	PendingAt         *time.Time `gorm:"column:pending_at;precision:6;comment:进入待处理时间"`
	ReadyAt           *time.Time `gorm:"column:ready_at;precision:6;comment:进入就绪时间"`
	DeletedAt         *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
}

func (FileObject) TableName() string { return "file_objects" }
