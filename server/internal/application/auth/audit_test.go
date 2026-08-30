package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/application/auth"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/authplatform"
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

func TestFailedLoginRecordsEveryAttemptWithBoundedDeviceMetadata(t *testing.T) {
	hasher := authplatform.BcryptHasher{Cost: 4}
	hash, err := hasher.Hash("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	sink := &auditSink{}
	svc := auth.NewService(
		userRepo{user: authdomain.User{ID: "u1", Identifier: "alice", PasswordHash: hash, Active: true}},
		hasher,
		authplatform.NewJWTService([]byte("test-secret"), time.Minute, time.Hour),
		authplatform.NewMemorySessionStore(),
	)
	svc.SetAuditSink(sink)
	ctx := auth.WithRequestMetadata(context.Background(), auth.RequestMetadata{
		RequestID: "req-failed", DeviceID: "device-failed", DeviceName: "Firefox",
		JSFingerprint: "fingerprint-failed", IPAddress: "192.0.2.15", UserAgent: "Mozilla/5.0",
	})

	if _, err := svc.Login(ctx, "alice", "wrong-password"); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v", err)
	}
	if len(sink.events) != 1 {
		t.Fatalf("audit event count = %d, want 1", len(sink.events))
	}
	event := sink.events[0]
	if event.EventType != authdomain.AuditLogin || event.Outcome != authdomain.AuditOutcomeFailure || event.UserID != "u1" || event.RequestID != "req-failed" || event.IPAddress != "192.0.2.15" {
		t.Fatalf("failed login audit = %+v", event)
	}
	if event.Metadata["username"] != "alice" || event.Metadata["deviceId"] != "device-failed" || event.Metadata["deviceName"] != "Firefox" || event.Metadata["jsFingerprint"] != "fingerprint-failed" || event.Metadata["reason"] != "invalid_credentials" {
		t.Fatalf("failed login metadata = %+v", event.Metadata)
	}
}

func TestRefreshAndLogoutRecordAuditEvents(t *testing.T) {
	hasher := authplatform.BcryptHasher{Cost: 4}
	hash, err := hasher.Hash("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	sink := &auditSink{}
	tokens := authplatform.NewJWTService([]byte("test-secret"), time.Minute, time.Hour)
	svc := auth.NewService(userRepo{user: authdomain.User{ID: "u1", Identifier: "alice", PasswordHash: hash, Active: true}}, hasher, tokens, authplatform.NewMemorySessionStore())
	svc.SetAuditSink(sink)

	pair, err := svc.Login(context.Background(), "alice", "correct-password")
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := svc.Refresh(context.Background(), pair.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := tokens.Parse(rotated.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Logout(context.Background(), claims.SessionID); err != nil {
		t.Fatal(err)
	}
	if len(sink.events) != 3 || sink.events[1].EventType != authdomain.AuditRefresh || sink.events[2].EventType != authdomain.AuditLogout {
		t.Fatalf("audit lifecycle events = %+v", sink.events)
	}
}
