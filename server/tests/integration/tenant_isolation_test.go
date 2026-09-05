package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/iamplatform"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/migration"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
)

func TestTenantIsolationAcrossIAMPersistence(t *testing.T) {
	if os.Getenv("DATA_PLATFORM_INTEGRATION") != "1" {
		t.Skip("set DATA_PLATFORM_INTEGRATION=1 to run tenant isolation integration")
	}
	for _, tc := range []struct {
		driver string
		dsn    string
	}{
		{driver: migration.DriverMySQL, dsn: requiredEnv(t, mysqlDSNEnv)},
		{driver: migration.DriverPostgres, dsn: requiredEnv(t, postgresDSNEnv)},
	} {
		t.Run(tc.driver, func(t *testing.T) { testTenantIsolation(t, tc.driver, tc.dsn) })
	}
}

func testTenantIsolation(t *testing.T, driver, dsn string) {
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
	tenantA := "it-tenant-a-" + suffix
	tenantB := "it-tenant-b-" + suffix
	username := "same-user-" + suffix
	for _, tenantID := range []string{tenantA, tenantB} {
		if err := store.Write(ctx).Table("gvba_sys_tenants").Create(map[string]any{"id": tenantID, "name": tenantID, "status": "active"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	var userA, userB uint64
	for index, tenantID := range []string{tenantA, tenantB} {
		result := store.Write(ctx).Table("gvba_iam_users").Create(map[string]any{
			"tenant_id": tenantID, "username": username, "username_normalized": strings.ToLower(username), "password_hash": "test-hash", "status": "active",
		})
		if result.Error != nil {
			t.Fatal(result.Error)
		}
		var userID uint64
		if err := store.Read(ctx).Table("gvba_iam_users").Where("tenant_id = ? AND username = ?", tenantID, username).Pluck("id", &userID).Error; err != nil {
			t.Fatal(err)
		}
		if index == 0 {
			userA = userID
		} else {
			userB = userID
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		store.Write(cleanupCtx).Exec("DELETE FROM gvba_iam_policies WHERE tenant_id IN (?, ?)", tenantA, tenantB)
		store.Write(cleanupCtx).Exec("DELETE FROM gvba_iam_users WHERE id IN (?, ?)", userA, userB)
		store.Write(cleanupCtx).Exec("DELETE FROM gvba_sys_tenants WHERE id IN (?, ?)", tenantA, tenantB)
	})

	persistence := iamplatform.NewGORMStore(store)
	ctxA := tenant.WithContext(ctx, tenant.Context{TenantID: tenantA})
	ctxB := tenant.WithContext(ctx, tenant.Context{TenantID: tenantB})
	if _, err := persistence.FindUser(ctxA, fmt.Sprint(userB)); !errors.Is(err, domain.ErrResourceNotFound) {
		t.Fatalf("tenant A cross-read error=%v, want resource not found", err)
	}
	if user, err := persistence.FindUser(ctxB, fmt.Sprint(userB)); err != nil || user.Username != username {
		t.Fatalf("tenant B own read user=%+v err=%v", user, err)
	}

	if err := persistence.SavePolicy(ctxA, domain.Policy{Subject: fmt.Sprint(userA), Domain: tenantB, Method: "GET", Path: "/private", Effect: domain.EffectAllow}); !errors.Is(err, tenant.ErrCrossTenant) {
		t.Fatalf("cross-tenant policy write error=%v, want cross-tenant denial", err)
	}
	if err := persistence.SavePolicy(ctxA, domain.Policy{Subject: fmt.Sprint(userA), Method: "GET", Path: "/private", Effect: domain.EffectAllow}); err != nil {
		t.Fatalf("tenant A policy write error=%v", err)
	}
	policiesA, err := persistence.ListPolicies(ctxA)
	if err != nil || len(policiesA) == 0 {
		t.Fatalf("tenant A policies=%+v err=%v", policiesA, err)
	}
	policiesB, err := persistence.ListPolicies(ctxB)
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range policiesB {
		if policy.Path == "/private" {
			t.Fatalf("tenant B observed tenant A policy: %+v", policy)
		}
	}
}
