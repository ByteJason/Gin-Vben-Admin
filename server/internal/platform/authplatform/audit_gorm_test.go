package authplatform

import (
	"context"
	"testing"

	appauth "example.com/gin-vben-admin/server/internal/application/auth"
	"example.com/gin-vben-admin/server/internal/domain/authdomain"
)

func TestGORMAuditSinkRecordsThroughTheApplicationPort(t *testing.T) {
	var _ appauth.AuditSink = NewGORMAuditSink(nil)
	sink := NewGORMAuditSink(nil)
	if err := sink.Record(context.Background(), authdomain.AuditEvent{EventType: authdomain.AuditLogin, Outcome: authdomain.AuditOutcomeSuccess}); err == nil {
		t.Fatal("Record() with an uninitialized sink returned nil error")
	}
}
