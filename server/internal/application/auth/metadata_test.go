package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/application/auth"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/authplatform"
)

type metadataJournal struct {
	session authdomain.Session
}

func (j *metadataJournal) Create(_ context.Context, session authdomain.Session) error {
	j.session = session
	return nil
}
func (j *metadataJournal) Rotate(context.Context, string, string, string, time.Time) error {
	return nil
}
func (j *metadataJournal) Revoke(context.Context, string) error { return nil }

func TestLoginCopiesRequestMetadataToSessionAndAudit(t *testing.T) {
	hasher := authplatform.BcryptHasher{Cost: 4}
	hash, err := hasher.Hash("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	journal := &metadataJournal{}
	sink := &auditSink{}
	svc := auth.NewService(userRepo{user: authdomain.User{ID: "u1", Identifier: "alice", PasswordHash: hash, Active: true}}, hasher, authplatform.NewJWTService([]byte("test-secret"), time.Minute, time.Hour), authplatform.NewMemorySessionStore())
	svc.SetSessionJournal(journal)
	svc.SetAuditSink(sink)
	ctx := auth.WithRequestMetadata(tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a"}), auth.RequestMetadata{
		RequestID: "req-1", DeviceID: "device-1", DeviceName: "Safari", JSFingerprint: "fp-1", IPAddress: "127.0.0.1", UserAgent: "UA",
	})

	if _, err := svc.Login(ctx, "alice", "correct-password"); err != nil {
		t.Fatal(err)
	}
	if journal.session.TenantID != "tenant-a" || journal.session.DeviceID != "device-1" || journal.session.DeviceName != "Safari" || journal.session.IPAddress != "127.0.0.1" || journal.session.UserAgent != "UA" {
		t.Fatalf("session metadata = %+v", journal.session)
	}
	if len(sink.events) != 1 || sink.events[0].RequestID != "req-1" || sink.events[0].IPAddress != "127.0.0.1" || sink.events[0].UserAgent != "UA" {
		t.Fatalf("audit metadata = %+v", sink.events)
	}
	if got := sink.events[0].Metadata; got["deviceId"] != "device-1" || got["deviceName"] != "Safari" || got["jsFingerprint"] != "fp-1" || got["username"] != "alice" {
		t.Fatalf("audit detail metadata = %+v", got)
	}
}
