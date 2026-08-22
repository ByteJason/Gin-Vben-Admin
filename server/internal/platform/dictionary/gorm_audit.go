package dictionaryplatform

import (
	"context"
	"strconv"
	"time"

	dictionaryapp "example.com/gin-vben-admin/server/internal/application/dictionary"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
	"example.com/gin-vben-admin/server/internal/platform/persistence/gormdb"
)

type auditRecord struct {
	UserID    *uint64           `gorm:"column:user_id"`
	EventType string            `gorm:"column:event_type"`
	Category  string            `gorm:"column:category"`
	Outcome   string            `gorm:"column:outcome"`
	Metadata  map[string]string `gorm:"column:metadata;serializer:json"`
	CreatedAt time.Time         `gorm:"column:created_at"`
	TenantID  string            `gorm:"column:tenant_id"`
	OrgID     string            `gorm:"column:org_id"`
}

func (auditRecord) TableName() string { return "auth_audit_events" }

type GORMAuditSink struct{ db *gormdb.Store }

func NewGORMAuditSink(db *gormdb.Store) *GORMAuditSink { return &GORMAuditSink{db: db} }

func (s *GORMAuditSink) Record(ctx context.Context, event dictionaryapp.AuditEvent) error {
	if s == nil || s.db == nil {
		return dictionaryapp.ErrRepositoryMissing
	}
	scope, err := tenantFromContext(ctx)
	if err != nil {
		return err
	}
	var userID *uint64
	if parsed, parseErr := strconv.ParseUint(event.ActorID, 10, 64); parseErr == nil && parsed > 0 {
		userID = &parsed
	}
	metadata := map[string]string{"typeCode": event.TypeCode}
	if event.ItemID != "" {
		metadata["itemId"] = event.ItemID
	}
	metadata["version"] = strconv.FormatInt(event.Version, 10)
	return s.db.Write(ctx).Create(&auditRecord{UserID: userID, EventType: event.Action, Category: "operation", Outcome: "success", Metadata: metadata, CreatedAt: event.CreatedAt, TenantID: scope.tenantID, OrgID: scope.orgID}).Error
}

type scopeValue struct{ tenantID, orgID string }

func tenantFromContext(ctx context.Context) (scopeValue, error) {
	// Keep this adapter independent from HTTP; application callers install the
	// validated scope in the standard tenant context before writing.
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return scopeValue{}, err
	}
	return scopeValue{tenantID: scope.TenantID, orgID: scope.Organization}, nil
}
