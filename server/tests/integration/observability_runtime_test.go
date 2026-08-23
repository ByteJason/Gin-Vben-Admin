package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/application/settings"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/bootstrap"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/config"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/migration"
)

// TestPersistedObservabilityLoadsOnRestart proves that a setting written by
// the management service is consumed by the next API process, rather than
// merely being stored for display. It uses a unique tenant and only deletes
// rows created by this test.
func TestPersistedObservabilityLoadsOnRestart(t *testing.T) {
	if os.Getenv("DATA_PLATFORM_INTEGRATION") != "1" {
		t.Skip("set DATA_PLATFORM_INTEGRATION=1 to run observability runtime integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, tc := range []struct {
		driver string
		dsn    string
	}{
		{driver: migration.DriverMySQL, dsn: requiredEnv(t, mysqlDSNEnv)},
		{driver: migration.DriverPostgres, dsn: requiredEnv(t, postgresDSNEnv)},
	} {
		t.Run(tc.driver, func(t *testing.T) {
			testPersistedObservabilityLoadsOnRestart(t, ctx, tc.driver, tc.dsn)
		})
	}
}

func testPersistedObservabilityLoadsOnRestart(t *testing.T, ctx context.Context, driver, dsn string) {
	t.Helper()
	runner, err := migration.New(driver, dsn)
	if err != nil {
		t.Fatalf("migration.New() error = %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	if err := runner.Up(); err != nil {
		t.Fatalf("migration.Up() error = %v", err)
	}

	tenantID := fmt.Sprintf("it-observability-%s-%d", driver, time.Now().UnixNano())
	base := config.Default()
	base.Database.Enabled = true
	base.Database.Driver = driver
	base.Database.Mode = "single"
	base.Database.DSN = dsn
	base.Tenant.DefaultID = tenantID
	base.Install.StateDir = t.TempDir()
	base.Server.Addr = "127.0.0.1:0"

	first, err := bootstrap.New(base)
	if err != nil {
		t.Fatalf("bootstrap.New(first) error = %v", err)
	}
	scopeCtx := tenant.WithContext(ctx, tenant.Context{TenantID: tenantID})
	if err := first.Database().Write(scopeCtx).Exec("INSERT INTO tenants (id, name, status) VALUES (?, ?, ?)", tenantID, tenantID, "active").Error; err != nil {
		_ = first.Close()
		t.Fatalf("insert test tenant error = %v", err)
	}
	actor := settings.Actor{ID: "it-observability-actor"}
	if _, err := first.Settings().Update(scopeCtx, actor, settings.UpdateInput{Key: "observability.metrics.enabled", Value: []byte(`true`), ExpectedVersion: 0}); err != nil {
		_ = first.Close()
		t.Fatalf("persist metrics enabled error = %v", err)
	}
	if _, err := first.Settings().Update(scopeCtx, actor, settings.UpdateInput{Key: "observability.metrics.endpoint", Value: []byte(`"http://127.0.0.1:8080/metrics"`), ExpectedVersion: 0}); err != nil {
		_ = first.Close()
		t.Fatalf("persist metrics endpoint error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first app error = %v", err)
	}

	second, err := bootstrap.New(base)
	if err != nil {
		t.Fatalf("bootstrap.New(second) error = %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = second.Database().Write(cleanupCtx).Exec("DELETE FROM auth_audit_events WHERE tenant_id = ?", tenantID).Error
		_ = second.Database().Write(cleanupCtx).Exec("DELETE FROM setting_versions WHERE tenant_id = ?", tenantID).Error
		_ = second.Database().Write(cleanupCtx).Exec("DELETE FROM tenants WHERE id = ?", tenantID).Error
		_ = second.Close()
	})
	if err := second.ReloadPersistedObservability(ctx); err != nil {
		t.Fatalf("ReloadPersistedObservability() error = %v", err)
	}
	if got := second.Observability().CollectorCount(); got != 1 {
		t.Fatalf("persisted metrics collector count = %d, want 1", got)
	}
	response := httptest.NewRecorder()
	second.HTTPServer().Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
}
