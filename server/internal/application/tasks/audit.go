package tasks

import (
	"context"
	"encoding/json"
	appauth "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/auth"
	"time"
)

type AuditEvent struct {
	TaskID, ActorID, RequestID, Action, ExecutorType, MethodKey string
	AllowInternal                                               bool
	CreatedAt                                                   time.Time
}
type AuditSink interface {
	Record(context.Context, AuditEvent) error
}

func (s *Service) SetAuditSink(sink AuditSink) { s.audit = sink }
func (s *Service) record(ctx context.Context, action string, d TaskDefinition) error {
	if s.audit == nil {
		return nil
	}
	meta := appauth.RequestMetadataFromContext(ctx)
	var config struct {
		AllowInternal bool `json:"allowInternal"`
	}
	_ = json.Unmarshal(d.HTTPConfig, &config)
	return s.audit.Record(ctx, AuditEvent{TaskID: d.ID, ActorID: meta.PrincipalID, RequestID: meta.RequestID, Action: action, ExecutorType: d.ExecutorType, MethodKey: d.MethodKey, AllowInternal: config.AllowInternal, CreatedAt: s.clock().UTC()})
}
