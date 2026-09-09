package tasksplatform

import (
	"context"
	"encoding/json"
	app "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/tasks"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/model"
	"gorm.io/gorm"
	"strconv"
)

type GORMAuditSink struct{ db *gormdb.Store }

func NewGORMAuditSink(db *gormdb.Store) *GORMAuditSink { return &GORMAuditSink{db: db} }
func (s *GORMAuditSink) Record(ctx context.Context, e app.AuditEvent) error {
	if s == nil || s.db == nil {
		return app.ErrRepositoryMissing
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return err
	}
	var userID *uint64
	if n, err := strconv.ParseUint(e.ActorID, 10, 64); err == nil && n > 0 {
		userID = &n
	}
	raw, err := json.Marshal(map[string]any{"resource": "tasks", "taskId": e.TaskID, "actorId": e.ActorID, "requestId": e.RequestID, "executorType": e.ExecutorType, "methodKey": e.MethodKey, "allowInternal": e.AllowInternal})
	if err != nil {
		return err
	}
	metadata := model.JSONValue(raw)
	var org *string
	if scope.Organization != "" {
		org = &scope.Organization
	}
	return gorm.G[model.AuthAuditEvent](s.db.Write(ctx)).Create(ctx, &model.AuthAuditEvent{TenantID: scope.TenantID, OrgID: org, UserID: userID, EventType: e.Action, Category: "operation", Outcome: "success", Metadata: &metadata, CreatedAt: e.CreatedAt})
}
