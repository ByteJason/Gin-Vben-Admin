package authplatform

import (
	"context"
	"errors"
	"testing"
	"time"

	appauth "example.com/gin-vben-admin/server/internal/application/auth"
	"example.com/gin-vben-admin/server/internal/domain/authdomain"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
)

func TestGORMSessionStoreImplementsSessionStore(t *testing.T) {
	var _ authdomain.SessionStore = NewGORMSessionStore(nil)
	store := NewGORMSessionStore(nil)
	if _, err := store.Get(context.Background(), "missing"); err == nil {
		t.Fatal("Get() with an uninitialized store returned nil error")
	}
	_ = time.Second // keep the seam test's time dependency explicit
}

func TestGORMSessionStoreExposesUserScopedDeviceOperations(t *testing.T) {
	var _ appauth.SessionQuery = NewGORMSessionStore(nil)
	store := NewGORMSessionStore(nil)
	if _, err := store.ListByUser(context.Background(), "1"); err == nil {
		t.Fatal("ListByUser() with an uninitialized store returned nil error")
	}
	if err := store.RevokeOwned(context.Background(), "1", "session-1"); err == nil {
		t.Fatal("RevokeOwned() with an uninitialized store returned nil error")
	}
	_ = authdomain.Session{DeviceID: "device-1", DeviceName: "Browser", IPAddress: "127.0.0.1", UserAgent: "UA"}
}

func TestGORMSessionStoreRequiresTenantContext(t *testing.T) {
	store := NewGORMSessionStore(nil)
	if _, err := store.Get(context.Background(), "session-1"); !errors.Is(err, tenant.ErrTenantContextMissing) {
		t.Fatalf("Get() error = %v, want tenant context missing", err)
	}
	if err := store.Revoke(context.Background(), "session-1"); !errors.Is(err, tenant.ErrTenantContextMissing) {
		t.Fatalf("Revoke() error = %v, want tenant context missing", err)
	}
}

func TestGORMSessionStoreRequiresSessionTenantOnCreate(t *testing.T) {
	store := NewGORMSessionStore(nil)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a"})
	err := store.Create(ctx, authdomain.Session{ID: "session-1", UserID: "1", RefreshJTI: "jti", ExpiresAt: time.Now().Add(time.Hour)})
	if !errors.Is(err, tenant.ErrTenantRequired) {
		t.Fatalf("Create() error = %v, want tenant required", err)
	}
}

func TestGORMSessionStoreRejectsCrossTenantCreate(t *testing.T) {
	store := NewGORMSessionStore(nil)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a"})
	err := store.Create(ctx, authdomain.Session{ID: "session-1", TenantID: "tenant-b", UserID: "1", RefreshJTI: "jti", ExpiresAt: time.Now().Add(time.Hour)})
	if !errors.Is(err, tenant.ErrCrossTenant) {
		t.Fatalf("Create() error = %v, want cross-tenant denial", err)
	}
}

func TestAuthSessionRecordRequiresTenantOnDecode(t *testing.T) {
	_, err := (authSessionRecord{ID: "session-1", UserID: 1}).toDomain()
	if !errors.Is(err, tenant.ErrTenantRequired) {
		t.Fatalf("toDomain() error = %v, want tenant required", err)
	}
}
