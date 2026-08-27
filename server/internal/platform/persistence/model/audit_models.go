// Package model contains the persistence-side GORM models.
//
// Files are grouped by capability (shared, identity, admin, audit and
// infrastructure) while remaining one Go package so migrations and repositories
// share exactly one set of column definitions. Relationship IDs intentionally
// remain scalar fields; ORM read relations live outside the migration registry.
package model

import "time"

type AuthAuditEvent struct {
	ID        uint64     `gorm:"column:id;primaryKey;autoIncrement;comment:审计标识"`
	TenantID  string     `gorm:"column:tenant_id;size:64;not null;default:default;index:idx_auth_audit_tenant,priority:1;comment:租户标识"`
	OrgID     *string    `gorm:"column:org_id;size:64;index:idx_auth_audit_tenant,priority:2;comment:组织标识"`
	UserID    *uint64    `gorm:"column:user_id;index:idx_auth_audit_user_created,priority:1;comment:用户标识"`
	SessionID string     `gorm:"column:session_id;size:64;not null;default:'';comment:会话标识"`
	EventType string     `gorm:"column:event_type;size:64;not null;index:idx_auth_audit_type_created,priority:1;comment:事件类型"`
	Category  string     `gorm:"column:category;size:16;not null;default:operation;index:idx_auth_audit_category_created,priority:1;comment:事件分类"`
	Outcome   string     `gorm:"column:outcome;size:32;not null;comment:事件结果"`
	RequestID string     `gorm:"column:request_id;size:128;not null;default:'';comment:请求标识"`
	IPAddress string     `gorm:"column:ip_address;size:64;not null;default:'';comment:请求地址"`
	UserAgent string     `gorm:"column:user_agent;size:512;not null;default:'';comment:客户端标识"`
	Metadata  *JSONValue `gorm:"column:metadata;comment:附加信息"`
	CreatedAt time.Time  `gorm:"column:created_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);index:idx_auth_audit_user_created,priority:2;index:idx_auth_audit_type_created,priority:2;index:idx_auth_audit_category_created,priority:2;comment:创建时间"`
	UpdatedAt time.Time  `gorm:"column:updated_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:更新时间"`
	DeletedAt *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
}

func (AuthAuditEvent) TableName() string { return "auth_audit_events" }
