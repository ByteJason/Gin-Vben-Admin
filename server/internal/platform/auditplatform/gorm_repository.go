// Package auditplatform contains persistence adapters for the shared audit
// read/write ports. Querying is read-only and never returns raw credentials.
package auditplatform

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	auditapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/audit"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/model"
	"gorm.io/gorm"
)

type GORMRepository struct{ db *gormdb.Store }

func NewGORMRepository(db *gormdb.Store) *GORMRepository { return &GORMRepository{db: db} }

// auditRecord is the shared persistence model. Keeping the repository on the
// canonical model prevents schema-only row definitions from drifting away
// from the migration source of truth.
type auditRecord = model.AuthAuditEvent

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
	query := gorm.G[auditRecord](r.db.Read(ctx)).Where("tenant_id = ?", scope.TenantID)
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
		query = query.Where("event_type = ?", persistedEventType(filter))
	}
	if filter.Category != "" {
		switch filter.Category {
		case auditapp.CategoryLogin:
			query = query.Where("event_type LIKE ?", "auth.%")
		case auditapp.CategorySystem:
			query = query.Where("event_type LIKE ?", "system.%")
		case auditapp.CategoryOperation:
			query = query.Where("event_type NOT LIKE ? AND event_type NOT LIKE ?", "auth.%", "system.%")
		}
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
	total, err = query.Count(ctx, "*")
	if err != nil {
		return nil, 0, errors.New("audit repository unavailable")
	}
	limit := filter.Offset + filter.Limit
	if limit <= 0 {
		limit = 50
	}
	rows, err := query.Order("created_at DESC").Offset(0).Limit(limit).Find(ctx)
	if err != nil {
		return nil, 0, errors.New("audit repository unavailable")
	}
	events := make([]auditapp.Event, 0, len(rows))
	for _, row := range rows {
		actor := ""
		if row.UserID != nil {
			actor = strconv.FormatUint(*row.UserID, 10)
		}
		details := make(map[string]any)
		if row.Metadata != nil {
			var metadata map[string]any
			if len(*row.Metadata) > 0 && json.Unmarshal(*row.Metadata, &metadata) == nil {
				for key, value := range metadata {
					details[key] = value
				}
			}
		}
		if row.SessionID != "" {
			details["sessionId"] = row.SessionID
		}
		if row.IPAddress != "" {
			details["ipAddress"] = row.IPAddress
		}
		if row.UserAgent != "" {
			details["userAgent"] = row.UserAgent
		}
		action, resource := row.EventType, ""
		for index := 0; index < len(row.EventType); index++ {
			if row.EventType[index] == '.' {
				resource, action = row.EventType[:index], row.EventType[index+1:]
				break
			}
		}
		category := auditapp.Classify(resource, action)
		if row.Category != "" {
			category = auditapp.Category(row.Category)
		}
		events = append(events, auditapp.Event{ID: strconv.FormatUint(row.ID, 10), ActorID: actor, Action: action, Resource: resource, Category: category, Outcome: row.Outcome, RequestID: row.RequestID, Details: details, CreatedAt: row.CreatedAt})
	}
	return events, int(total), nil
}

func (r *GORMRepository) CountBefore(ctx context.Context, cutoff time.Time) (int, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("audit repository unavailable")
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return 0, err
	}
	query := gorm.G[auditRecord](r.db.Read(ctx)).Where("tenant_id = ?", scope.TenantID).Where("created_at < ?", cutoff)
	if scope.Organization != "" {
		query = query.Where("org_id = ?", scope.Organization)
	}
	var total int64
	var countErr error
	total, countErr = query.Count(ctx, "*")
	if countErr != nil {
		return 0, errors.New("audit repository unavailable")
	}
	return int(total), nil
}

func persistedEventType(filter auditapp.Filter) string {
	action := strings.TrimSpace(filter.Action)
	if action == "" {
		return ""
	}
	if strings.Contains(action, ".") || strings.TrimSpace(filter.Resource) == "" {
		return action
	}
	return strings.Trim(strings.TrimSpace(filter.Resource), ".") + "." + action
}

var _ auditapp.Repository = (*GORMRepository)(nil)
var _ auditapp.PageRepository = (*GORMRepository)(nil)
