package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/organization"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/migration"
	organizationplatform "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/organizationplatform"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
)

func TestTenantOrganizationTreeIsolation(t *testing.T) {
	if os.Getenv("DATA_PLATFORM_INTEGRATION") != "1" {
		t.Skip("set DATA_PLATFORM_INTEGRATION=1 to run organization isolation integration")
	}
	for _, tc := range []struct {
		driver string
		dsn    string
	}{
		{driver: migration.DriverMySQL, dsn: requiredEnv(t, mysqlDSNEnv)},
		{driver: migration.DriverPostgres, dsn: requiredEnv(t, postgresDSNEnv)},
	} {
		t.Run(tc.driver, func(t *testing.T) { testOrganizationTreeIsolation(t, tc.driver, tc.dsn) })
	}
}

func testOrganizationTreeIsolation(t *testing.T, driver, dsn string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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
	tenantA, tenantB := "org-tenant-a-"+suffix, "org-tenant-b-"+suffix
	rootID, childID := "org-root-"+suffix, "org-child-"+suffix
	for _, id := range []string{tenantA, tenantB} {
		if err := store.Write(ctx).Table("gvba_sys_tenants").Create(map[string]any{"id": id, "name": id, "status": "active"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = store.Write(cleanupCtx).Exec("DELETE FROM gvba_sys_organizations WHERE id IN (?, ?)", rootID, childID).Error
		_ = store.Write(cleanupCtx).Exec("DELETE FROM gvba_sys_tenants WHERE id IN (?, ?)", tenantA, tenantB).Error
	})

	repo := organizationplatform.NewGORMRepository(store)
	ctxA := tenant.WithContext(ctx, tenant.Context{TenantID: tenantA})
	ctxB := tenant.WithContext(ctx, tenant.Context{TenantID: tenantB})
	if err := repo.Create(ctxA, organization.Organization{ID: rootID, TenantID: tenantA, Name: "Root", Status: "active"}); err != nil {
		t.Fatalf("create root: %v", err)
	}
	if err := repo.Create(ctxA, organization.Organization{ID: childID, TenantID: tenantA, ParentID: rootID, Name: "Child", Status: "active"}); err != nil {
		t.Fatalf("create child: %v", err)
	}
	if _, err := repo.Get(ctxB, rootID); !errors.Is(err, organization.ErrOrganizationNotFound) {
		t.Fatalf("cross-tenant Get error=%v, want organization not found", err)
	}
	if roots, err := repo.List(ctxB, ""); err != nil || len(roots) != 0 {
		t.Fatalf("cross-tenant List roots=%+v err=%v, want empty", roots, err)
	}
	if child, err := repo.Get(ctxA, childID); err != nil || child.ParentID != rootID || child.TenantID != tenantA {
		t.Fatalf("own child=%+v err=%v", child, err)
	}
	if roots, err := repo.List(ctxA, ""); err != nil || len(roots) != 1 || roots[0].ID != rootID {
		t.Fatalf("own roots=%+v err=%v, want root", roots, err)
	}
	if children, err := repo.List(ctxA, rootID); err != nil || len(children) != 1 || children[0].ID != childID {
		t.Fatalf("own children=%+v err=%v, want child", children, err)
	}
}
