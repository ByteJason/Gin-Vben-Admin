package authplatform

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

func TestRedisSessionStoreRequiresTenantContext(t *testing.T) {
	store := NewRedisSessionStore(nil)
	if _, err := store.Get(context.Background(), "session-1"); !errors.Is(err, tenant.ErrTenantContextMissing) {
		t.Fatalf("Get() error = %v, want tenant context missing", err)
	}
	if err := store.Revoke(context.Background(), "session-1"); !errors.Is(err, tenant.ErrTenantContextMissing) {
		t.Fatalf("Revoke() error = %v, want tenant context missing", err)
	}
}

func TestRedisSessionStoreRejectsMissingOrCrossTenantCreate(t *testing.T) {
	store := NewRedisSessionStore(nil)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a"})
	session := authdomain.Session{ID: "session-1", UserID: "1", RefreshJTI: "jti", ExpiresAt: time.Now().Add(time.Hour)}
	if err := store.Create(ctx, session); !errors.Is(err, tenant.ErrTenantRequired) {
		t.Fatalf("Create() missing tenant error = %v, want tenant required", err)
	}
	session.TenantID = "tenant-b"
	if err := store.Create(ctx, session); !errors.Is(err, tenant.ErrCrossTenant) {
		t.Fatalf("Create() cross tenant error = %v, want cross tenant", err)
	}
}
