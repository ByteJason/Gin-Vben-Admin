package auth_test

import (
	"context"
	"testing"
	"time"

	"example.com/gin-vben-admin/server/internal/application/auth"
	"example.com/gin-vben-admin/server/internal/domain/authdomain"
	"example.com/gin-vben-admin/server/internal/platform/authplatform"
)

type auditSink struct {
	events []authdomain.AuditEvent
}

func (s *auditSink) Record(_ context.Context, event authdomain.AuditEvent) error {
	s.events = append(s.events, event)
	return nil
}

func TestSuccessfulLoginRecordsAuditEvent(t *testing.T) {
	hasher := authplatform.BcryptHasher{Cost: 4}
	hash, err := hasher.Hash("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	sink := &auditSink{}
	svc := auth.NewService(userRepo{user: authdomain.User{ID: "u1", Identifier: "alice", PasswordHash: hash, Active: true}}, hasher, authplatform.NewJWTService([]byte("test-secret"), time.Minute, time.Hour), authplatform.NewMemorySessionStore())
	svc.SetAuditSink(sink)

	if _, err := svc.Login(context.Background(), "alice", "correct-password"); err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("audit event count = %d, want 1", len(sink.events))
	}
	event := sink.events[0]
	if event.EventType != authdomain.AuditLogin || event.Outcome != authdomain.AuditOutcomeSuccess || event.UserID != "u1" || event.SessionID == "" {
		t.Fatalf("audit event = %+v", event)
	}
}
