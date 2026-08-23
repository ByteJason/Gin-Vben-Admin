package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	auditapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/audit"
	settingsapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/settings"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	auditplatform "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/auditplatform"
	authplatform "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/authplatform"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/migration"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	settingsplatform "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/settingsplatform"
)

func TestTenantIsolationAcrossSettingsAndAuditPersistence(t *testing.T) {
	if os.Getenv("DATA_PLATFORM_INTEGRATION") != "1" {
		t.Skip("set DATA_PLATFORM_INTEGRATION=1 to run settings/audit tenant isolation integration")
	}
	for _, tc := range []struct {
		driver string
		dsn    string
	}{
		{driver: migration.DriverMySQL, dsn: requiredEnv(t, mysqlDSNEnv)},
		{driver: migration.DriverPostgres, dsn: requiredEnv(t, postgresDSNEnv)},
	} {
		t.Run(tc.driver, func(t *testing.T) { testSettingsAuditTenantIsolation(t, tc.driver, tc.dsn) })
	}
}

func testSettingsAuditTenantIsolation(t *testing.T, driver, dsn string) {
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
	tenantA, tenantB := "settings-a-"+suffix, "settings-b-"+suffix
	key := "site.name." + suffix
	requestA, requestB, authRequestA := "audit-a-"+suffix, "audit-b-"+suffix, "auth-a-"+suffix
	for _, id := range []string{tenantA, tenantB} {
		if err := store.Write(ctx).Table("tenants").Create(map[string]any{"id": id, "name": id, "status": "active"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = store.Write(cleanupCtx).Exec("DELETE FROM setting_versions WHERE tenant_id IN (?, ?)", tenantA, tenantB).Error
		_ = store.Write(cleanupCtx).Exec("DELETE FROM auth_audit_events WHERE tenant_id IN (?, ?)", tenantA, tenantB).Error
		_ = store.Write(cleanupCtx).Exec("DELETE FROM tenants WHERE id IN (?, ?)", tenantA, tenantB).Error
	})

	ctxA := tenant.WithContext(ctx, tenant.Context{TenantID: tenantA})
	ctxB := tenant.WithContext(ctx, tenant.Context{TenantID: tenantB})
	repo := settingsplatform.NewGORMRepository(store)
	if _, err := repo.Current(context.Background(), key); !errors.Is(err, tenant.ErrTenantContextMissing) {
		t.Fatalf("settings Current without tenant error=%v, want tenant context missing", err)
	}
	if _, err := repo.Append(ctxA, settingsapp.StoredSetting{Key: key, RawValue: []byte(`"A"`), UpdatedBy: "admin"}); err != nil {
		t.Fatalf("settings append A: %v", err)
	}
	if _, err := repo.Append(ctxB, settingsapp.StoredSetting{Key: key, RawValue: []byte(`"B"`), UpdatedBy: "admin"}); err != nil {
		t.Fatalf("settings append B: %v", err)
	}
	if got, err := repo.Current(ctxA, key); err != nil || string(got.RawValue) != `"A"` {
		t.Fatalf("settings A current=%+v err=%v", got, err)
	}
	if got, err := repo.Current(ctxB, key); err != nil || string(got.RawValue) != `"B"` {
		t.Fatalf("settings B current=%+v err=%v", got, err)
	}

	settingsAudit := settingsplatform.NewGORMAuditSink(store)
	if err := settingsAudit.Record(ctxA, settingsapp.AuditEvent{ActorID: "", Action: "update", Key: key, Version: 1}); err != nil {
		t.Fatalf("settings audit A: %v", err)
	}
	if err := settingsAudit.Record(ctxB, settingsapp.AuditEvent{ActorID: "", Action: "update", Key: key, Version: 1}); err != nil {
		t.Fatalf("settings audit B: %v", err)
	}
	authAudit := authplatform.NewGORMAuditSink(store)
	if err := authAudit.Record(ctxA, authdomain.AuditEvent{EventType: authdomain.AuditLogin, Outcome: authdomain.AuditOutcomeSuccess, RequestID: authRequestA}); err != nil {
		t.Fatalf("auth audit A: %v", err)
	}
	if err := authAudit.Record(context.Background(), authdomain.AuditEvent{EventType: authdomain.AuditLogin, Outcome: authdomain.AuditOutcomeSuccess}); !errors.Is(err, tenant.ErrTenantContextMissing) {
		t.Fatalf("auth audit without tenant error=%v, want tenant context missing", err)
	}

	auditRepo := auditplatform.NewGORMRepository(store)
	if _, _, err := auditRepo.QueryPage(context.Background(), auditapp.Filter{RequestID: requestA}); !errors.Is(err, tenant.ErrTenantContextMissing) {
		t.Fatalf("audit QueryPage without tenant error=%v, want tenant context missing", err)
	}
	// Add request IDs through the generic table to make the read-side assertion
	// independent of actor/user foreign keys and keep cleanup tenant-scoped.
	for _, item := range []struct {
		ctx context.Context
		id  string
	}{
		{ctx: ctxA, id: requestA}, {ctx: ctxB, id: requestB},
	} {
		scope, _ := tenant.RequireContext(item.ctx)
		if err := store.Write(item.ctx).Table("auth_audit_events").Create(map[string]any{
			"tenant_id": scope.TenantID, "event_type": "settings.update", "outcome": "success", "request_id": item.id,
			"metadata": `{}`, "created_at": time.Now().UTC(),
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		ctx  context.Context
		id   string
		want int
	}{
		{ctx: ctxA, id: requestA, want: 1}, {ctx: ctxB, id: requestB, want: 1},
	} {
		page, total, err := auditRepo.QueryPage(item.ctx, auditapp.Filter{RequestID: item.id, Limit: 10})
		if err != nil || total != item.want || len(page) != item.want {
			t.Fatalf("audit %s page=%+v total=%d err=%v", item.id, page, total, err)
		}
	}
	if page, total, err := auditRepo.QueryPage(ctxA, auditapp.Filter{RequestID: requestB, Limit: 10}); err != nil || total != 0 || len(page) != 0 {
		t.Fatalf("cross-tenant audit page=%+v total=%d err=%v, want empty", page, total, err)
	}
}
