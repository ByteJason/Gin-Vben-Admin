package settingsplatform

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	settingsapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/settings"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/model"
	"gorm.io/gorm"
)

type GORMAuditSink struct{ db *gormdb.Store }

func NewGORMAuditSink(db *gormdb.Store) *GORMAuditSink { return &GORMAuditSink{db: db} }

type settingsAuditRecord = model.AuthAuditEvent

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
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return errors.New("settings audit sink unavailable")
	}
	metadataValue := model.JSONValue(encoded)
	orgID := scope.Organization
	var orgPtr *string
	if orgID != "" {
		orgPtr = &orgID
	}
	record := settingsAuditRecord{UserID: userID, EventType: "settings." + event.Action, Category: "operation", Outcome: "success", Metadata: &metadataValue, CreatedAt: time.Now().UTC(), TenantID: scope.TenantID, OrgID: orgPtr}
	if err := gorm.G[settingsAuditRecord](s.db.Write(ctx)).Create(ctx, &record); err != nil {
		return errors.New("settings audit sink unavailable")
	}
	return nil
}

var _ settingsapp.AuditSink = (*GORMAuditSink)(nil)
