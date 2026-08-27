package dictionaryplatform

import (
	"context"
	"encoding/json"
	"strconv"

	dictionaryapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/dictionary"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/model"
	"gorm.io/gorm"
)

type auditRecord = model.AuthAuditEvent

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
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return dictionaryapp.ErrRepositoryMissing
	}
	metadataValue := model.JSONValue(encoded)
	var orgPtr *string
	if scope.orgID != "" {
		orgPtr = &scope.orgID
	}
	return gorm.G[auditRecord](s.db.Write(ctx)).Create(ctx, &auditRecord{UserID: userID, EventType: event.Action, Category: "operation", Outcome: "success", Metadata: &metadataValue, CreatedAt: event.CreatedAt, TenantID: scope.tenantID, OrgID: orgPtr})
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
