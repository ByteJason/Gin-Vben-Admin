package authplatform

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
)

// GORMAuditSink stores authentication outcomes in the append-only audit table.
type GORMAuditSink struct {
	db *gormdb.Store
}

func NewGORMAuditSink(db *gormdb.Store) *GORMAuditSink {
	return &GORMAuditSink{db: db}
}

type authAuditRecord struct {
	ID        uint64            `gorm:"column:id;primaryKey"`
	UserID    *uint64           `gorm:"column:user_id"`
	SessionID string            `gorm:"column:session_id"`
	EventType string            `gorm:"column:event_type"`
	Category  string            `gorm:"column:category"`
	Outcome   string            `gorm:"column:outcome"`
	RequestID string            `gorm:"column:request_id"`
	IPAddress string            `gorm:"column:ip_address"`
	UserAgent string            `gorm:"column:user_agent"`
	Metadata  map[string]string `gorm:"column:metadata;serializer:json"`
	CreatedAt time.Time         `gorm:"column:created_at"`
	TenantID  string            `gorm:"column:tenant_id"`
	OrgID     string            `gorm:"column:org_id"`
}

func (authAuditRecord) TableName() string { return "auth_audit_events" }

func (s *GORMAuditSink) Record(ctx context.Context, event authdomain.AuditEvent) error {
	if s == nil || s.db == nil {
		return authdomain.ErrDependencyUnavailable
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(event.EventType) == "" || strings.TrimSpace(event.Outcome) == "" {
		return authdomain.ErrInvalidAuditEvent
	}
	var userID *uint64
	if strings.TrimSpace(event.UserID) != "" {
		parsed, err := parseNumericUserID(event.UserID)
		if err != nil {
			return authdomain.ErrInvalidAuditEvent
		}
		userID = &parsed
	}
	createdAt := event.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	metadata := make(map[string]string, len(event.Metadata))
	for key, value := range event.Metadata {
		if strings.TrimSpace(key) == "" || len(key) > 64 || len(value) > 512 {
			return authdomain.ErrInvalidAuditEvent
		}
		metadata[key] = value
	}
	record := authAuditRecord{
		UserID: userID, SessionID: bounded(event.SessionID, 128), EventType: bounded(event.EventType, 64), Category: auditCategory(event.EventType),
		Outcome: bounded(event.Outcome, 32), RequestID: bounded(event.RequestID, 128),
		IPAddress: bounded(event.IPAddress, 64), UserAgent: bounded(event.UserAgent, 512),
		Metadata: metadata, CreatedAt: createdAt.UTC(), TenantID: scope.TenantID, OrgID: scope.Organization,
	}
	if err := s.db.Write(ctx).Create(&record).Error; err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return authdomain.ErrDependencyUnavailable
	}
	return nil
}

func auditCategory(eventType string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(eventType)), "auth.") {
		return "login"
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(eventType)), "system.") {
		return "system"
	}
	return "operation"
}

func bounded(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max]
}

var _ interface {
	Record(context.Context, authdomain.AuditEvent) error
} = (*GORMAuditSink)(nil)
