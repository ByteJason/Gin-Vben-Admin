package settingsplatform

import (
	"context"
	"errors"
	"strconv"
	"time"

	settingsapp "example.com/gin-vben-admin/server/internal/application/settings"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
	"example.com/gin-vben-admin/server/internal/platform/persistence/gormdb"
)

type GORMAuditSink struct{ db *gormdb.Store }

func NewGORMAuditSink(db *gormdb.Store) *GORMAuditSink { return &GORMAuditSink{db: db} }

type settingsAuditRecord struct {
	UserID    *uint64           `gorm:"column:user_id"`
	EventType string            `gorm:"column:event_type"`
	Category  string            `gorm:"column:category"`
	Outcome   string            `gorm:"column:outcome"`
	Metadata  map[string]string `gorm:"column:metadata;serializer:json"`
	CreatedAt time.Time         `gorm:"column:created_at"`
	TenantID  string            `gorm:"column:tenant_id"`
	OrgID     string            `gorm:"column:org_id"`
}

func (settingsAuditRecord) TableName() string { return "auth_audit_events" }

func (s *GORMAuditSink) Record(ctx context.Context, event settingsapp.AuditEvent) error {
	if s == nil || s.db == nil {
		return errors.New("settings audit sink unavailable")
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return err
	}
	var userID *uint64
	if parsed, err := strconv.ParseUint(event.ActorID, 10, 64); err == nil && parsed > 0 {
		userID = &parsed
	}
	metadata := map[string]string{"key": event.Key, "version": strconv.FormatInt(event.Version, 10)}
	record := settingsAuditRecord{UserID: userID, EventType: "settings." + event.Action, Category: "operation", Outcome: "success", Metadata: metadata, CreatedAt: time.Now().UTC()}
	record.TenantID = scope.TenantID
	record.OrgID = scope.Organization
	if err := s.db.Write(ctx).Create(&record).Error; err != nil {
		return errors.New("settings audit sink unavailable")
	}
	return nil
}

var _ settingsapp.AuditSink = (*GORMAuditSink)(nil)
