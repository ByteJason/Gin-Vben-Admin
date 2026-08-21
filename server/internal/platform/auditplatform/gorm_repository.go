// Package auditplatform contains persistence adapters for the shared audit
// read/write ports. Querying is read-only and never returns raw credentials.
package auditplatform

import (
	"context"
	"errors"
	"strconv"
	"time"

	auditapp "example.com/gin-vben-admin/server/internal/application/audit"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
	"example.com/gin-vben-admin/server/internal/platform/persistence/gormdb"
)

type GORMRepository struct{ db *gormdb.Store }

func NewGORMRepository(db *gormdb.Store) *GORMRepository { return &GORMRepository{db: db} }

type auditRecord struct {
	ID        uint64            `gorm:"column:id;primaryKey"`
	UserID    *uint64           `gorm:"column:user_id"`
	EventType string            `gorm:"column:event_type"`
	Outcome   string            `gorm:"column:outcome"`
	RequestID string            `gorm:"column:request_id"`
	Metadata  map[string]string `gorm:"column:metadata;serializer:json"`
	CreatedAt time.Time         `gorm:"column:created_at"`
	TenantID  string            `gorm:"column:tenant_id"`
	OrgID     string            `gorm:"column:org_id"`
}

func (auditRecord) TableName() string { return "auth_audit_events" }

func (r *GORMRepository) Query(ctx context.Context, filter auditapp.Filter) ([]auditapp.Event, error) {
	events, _, err := r.QueryPage(ctx, filter)
	return events, err
}

func (r *GORMRepository) QueryPage(ctx context.Context, filter auditapp.Filter) ([]auditapp.Event, int, error) {
	if r == nil || r.db == nil {
		return nil, 0, errors.New("audit repository unavailable")
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, 0, err
	}
	query := r.db.Read(ctx).Model(&auditRecord{})
	query = query.Where("tenant_id = ?", scope.TenantID)
	if scope.Organization != "" {
		query = query.Where("org_id = ?", scope.Organization)
	}
	if filter.ActorID != "" {
		if id, err := strconv.ParseUint(filter.ActorID, 10, 64); err == nil {
			query = query.Where("user_id = ?", id)
		} else {
			return []auditapp.Event{}, 0, nil
		}
	}
	if filter.Action != "" {
		query = query.Where("event_type = ?", filter.Action)
	}
	if filter.Outcome != "" {
		query = query.Where("outcome = ?", filter.Outcome)
	}
	if filter.RequestID != "" {
		query = query.Where("request_id = ?", filter.RequestID)
	}
	if !filter.From.IsZero() {
		query = query.Where("created_at >= ?", filter.From)
	}
	if !filter.To.IsZero() {
		query = query.Where("created_at <= ?", filter.To)
	}
	if filter.Resource != "" {
		// Authentication/settings writers encode the resource prefix in the
		// event type (for example auth.login or settings.update).
		query = query.Where("event_type LIKE ?", filter.Resource+".%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, errors.New("audit repository unavailable")
	}
	rows := make([]auditRecord, 0)
	limit := filter.Offset + filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if err := query.Order("created_at DESC").Offset(0).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, errors.New("audit repository unavailable")
	}
	events := make([]auditapp.Event, 0, len(rows))
	for _, row := range rows {
		actor := ""
		if row.UserID != nil {
			actor = strconv.FormatUint(*row.UserID, 10)
		}
		details := make(map[string]any, len(row.Metadata))
		for key, value := range row.Metadata {
			details[key] = value
		}
		action, resource := row.EventType, ""
		for index := 0; index < len(row.EventType); index++ {
			if row.EventType[index] == '.' {
				resource, action = row.EventType[:index], row.EventType[index+1:]
				break
			}
		}
		events = append(events, auditapp.Event{ID: strconv.FormatUint(row.ID, 10), ActorID: actor, Action: action, Resource: resource, Outcome: row.Outcome, RequestID: row.RequestID, Details: details, CreatedAt: row.CreatedAt})
	}
	return events, int(total), nil
}

var _ auditapp.Repository = (*GORMRepository)(nil)
var _ auditapp.PageRepository = (*GORMRepository)(nil)
