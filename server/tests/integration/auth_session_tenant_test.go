package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/authplatform"
	rediscache "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/cache/redis"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/migration"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
)

func TestTenantIsolationAcrossDurableAuthSessions(t *testing.T) {
	if os.Getenv("DATA_PLATFORM_INTEGRATION") != "1" {
		t.Skip("set DATA_PLATFORM_INTEGRATION=1 to run auth session tenant isolation integration")
	}
	for _, tc := range []struct {
		driver string
		dsn    string
	}{
		{driver: migration.DriverMySQL, dsn: requiredEnv(t, mysqlDSNEnv)},
		{driver: migration.DriverPostgres, dsn: requiredEnv(t, postgresDSNEnv)},
	} {
		t.Run(tc.driver, func(t *testing.T) { testAuthSessionTenantIsolation(t, tc.driver, tc.dsn) })
	}
}

func TestTenantIsolationAcrossRedisAuthSessions(t *testing.T) {
	if os.Getenv("DATA_PLATFORM_INTEGRATION") != "1" {
		t.Skip("set DATA_PLATFORM_INTEGRATION=1 to run Redis auth session tenant isolation integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cache, err := rediscache.New(rediscache.Config{
		Mode: rediscache.ModeSingle, Addr: requiredEnv(t, redisAddrEnv),
		Namespace: fmt.Sprintf("app:v1:test-session-tenant-%d", time.Now().UnixNano()),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cache.Close() })
	store := authplatform.NewRedisSessionStore(cache)
	session := authdomain.Session{
		ID: "redis-session-" + fmt.Sprint(time.Now().UnixNano()), TenantID: "tenant-a", UserID: "1",
		RefreshJTI: "jti-a", ExpiresAt: time.Now().Add(time.Hour),
	}
	ctxA := tenant.WithContext(ctx, tenant.Context{TenantID: "tenant-a"})
	ctxB := tenant.WithContext(ctx, tenant.Context{TenantID: "tenant-b"})
	if err := store.Create(ctxA, session); err != nil {
		t.Fatalf("create own tenant session: %v", err)
	}
	key, err := cache.Key("auth-session", session.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cache.Delete(context.Background(), key) })
	if _, err := store.Get(ctxB, session.ID); !errors.Is(err, authdomain.ErrSessionNotFound) {
		t.Fatalf("cross-tenant Get error=%v, want session not found", err)
	}
	if _, err := store.Get(ctxA, session.ID); err != nil {
		t.Fatalf("own-tenant Get error=%v", err)
	}
	if err := store.Rotate(ctxB, session.ID, "jti-a", "jti-b", time.Now().Add(time.Hour)); !errors.Is(err, authdomain.ErrSessionNotFound) {
		t.Fatalf("cross-tenant Rotate error=%v, want session not found", err)
	}
	if err := store.Revoke(ctxB, session.ID); !errors.Is(err, authdomain.ErrSessionNotFound) {
		t.Fatalf("cross-tenant Revoke error=%v, want session not found", err)
	}
	if err := store.Revoke(ctxA, session.ID); err != nil {
		t.Fatalf("own-tenant Revoke error=%v", err)
	}
}

func testAuthSessionTenantIsolation(t *testing.T, driver, dsn string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	runner, err := migration.New(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	if err := runner.Up(); err != nil {
		t.Fatal(err)
	}
	store, err := gormdb.Open(gormdb.Options{Driver: driver, Mode: gormdb.ModeSingle, DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	suffix := fmt.Sprintf("%s_%d", driver, time.Now().UnixNano())
	tenantA := "session-a-" + suffix
	tenantB := "session-b-" + suffix
	for _, id := range []string{tenantA, tenantB} {
		if err := store.Write(ctx).Table("gvba_sys_tenants").Create(map[string]any{"id": id, "name": id, "status": "active"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	var userID uint64
	username := "session-user-" + suffix
	if err := store.Write(ctx).Table("gvba_iam_users").Create(map[string]any{"tenant_id": tenantA, "username": username, "username_normalized": strings.ToLower(username), "password_hash": "hash", "status": "active"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.Read(ctx).Table("gvba_iam_users").Where("tenant_id = ?", tenantA).Pluck("id", &userID).Error; err != nil {
		t.Fatal(err)
	}
	sessionID := "session-" + suffix
	if err := store.Write(ctx).Table("gvba_auth_sessions").Create(map[string]any{
		"id": sessionID, "tenant_id": tenantA, "user_id": userID,
		"refresh_token_hash": authdomain.HashRefreshJTI("jti-a"), "family_id": sessionID,
		"status": "active", "expires_at": time.Now().Add(time.Hour), "last_seen_at": time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		store.Write(cleanupCtx).Exec("DELETE FROM gvba_auth_sessions WHERE id = ?", sessionID)
		store.Write(cleanupCtx).Exec("DELETE FROM gvba_iam_users WHERE id = ?", userID)
		store.Write(cleanupCtx).Exec("DELETE FROM gvba_sys_tenants WHERE id IN (?, ?)", tenantA, tenantB)
	})

	sessions := authplatform.NewGORMSessionStore(store)
	ctxA := tenant.WithContext(ctx, tenant.Context{TenantID: tenantA})
	ctxB := tenant.WithContext(ctx, tenant.Context{TenantID: tenantB})
	if _, err := sessions.Get(ctxB, sessionID); !errors.Is(err, authdomain.ErrSessionNotFound) {
		t.Fatalf("cross-tenant Get error=%v, want session not found", err)
	}
	if _, err := sessions.Get(ctxA, sessionID); err != nil {
		t.Fatalf("own-tenant Get error=%v", err)
	}
	if list, err := sessions.ListByUser(ctxB, fmt.Sprint(userID)); err != nil || len(list) != 0 {
		t.Fatalf("cross-tenant ListByUser sessions=%+v err=%v, want empty", list, err)
	}
	if list, err := sessions.ListByUser(ctxA, fmt.Sprint(userID)); err != nil || len(list) != 1 {
		t.Fatalf("own-tenant ListByUser sessions=%+v err=%v, want one", list, err)
	}
	if err := sessions.Rotate(ctxB, sessionID, "jti-a", "jti-b", time.Now().Add(time.Hour)); !errors.Is(err, authdomain.ErrSessionNotFound) {
		t.Fatalf("cross-tenant Rotate error=%v, want session not found", err)
	}
	if err := sessions.Revoke(ctxB, sessionID); !errors.Is(err, authdomain.ErrSessionNotFound) {
		t.Fatalf("cross-tenant Revoke error=%v, want session not found", err)
	}
	if err := sessions.Revoke(ctxA, sessionID); err != nil {
		t.Fatalf("own-tenant Revoke error=%v", err)
	}
}
