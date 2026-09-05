package integration

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	iamapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/iam"
	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/iamplatform"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/migration"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
)

func TestRBACPersistenceSingleNode(t *testing.T) {
	if os.Getenv("DATA_PLATFORM_INTEGRATION") != "1" {
		t.Skip("set DATA_PLATFORM_INTEGRATION=1 to run RBAC persistence integration")
	}
	for _, tc := range []struct {
		driver string
		dsn    string
	}{
		{driver: migration.DriverMySQL, dsn: requiredEnv(t, mysqlDSNEnv)},
		{driver: migration.DriverPostgres, dsn: requiredEnv(t, postgresDSNEnv)},
	} {
		t.Run(tc.driver, func(t *testing.T) { testRBACPersistence(t, tc.driver, tc.dsn) })
	}
}

func testRBACPersistence(t *testing.T, driver, dsn string) {
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
	persistence := iamplatform.NewGORMStore(store)
	scopeCtx := tenant.WithContext(ctx, tenant.Context{TenantID: "default"})

	suffix := fmt.Sprintf("%s_%d", driver, time.Now().UnixNano())
	username := "it_rbac_" + suffix
	roleID := "it-role-" + suffix
	menuID := "it-menu-" + suffix
	permissionID := "it-perm-" + suffix
	cleanup := func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		store.Write(cleanupCtx).Exec("DELETE FROM gvba_iam_menus WHERE id = ?", menuID)
		store.Write(cleanupCtx).Exec("DELETE FROM gvba_iam_permissions WHERE id = ?", permissionID)
		store.Write(cleanupCtx).Exec("DELETE FROM gvba_iam_roles WHERE id = ?", roleID)
		store.Write(cleanupCtx).Exec("DELETE FROM gvba_iam_users WHERE username = ?", username)
	}
	t.Cleanup(cleanup)

	if err := store.Write(ctx).Exec("INSERT INTO gvba_iam_users (username, username_normalized, password_hash, status) VALUES (?, ?, ?, ?)", username, strings.ToLower(username), "test-hash", "active").Error; err != nil {
		t.Fatal(err)
	}
	var userID uint64
	if err := store.Read(ctx).Table("gvba_iam_users").Where("username = ?", username).Pluck("id", &userID).Error; err != nil {
		t.Fatal(err)
	}
	user := domain.User{ID: fmt.Sprint(userID), Username: username, Active: true, RoleIDs: []string{roleID}}
	if err := persistence.SaveRole(scopeCtx, domain.Role{ID: roleID, Name: "Integration Reader", Active: true, DataScope: domain.ScopeOrg}); err != nil {
		t.Fatal(err)
	}
	if err := persistence.SaveUser(scopeCtx, user); err != nil {
		t.Fatal(err)
	}
	if err := persistence.SavePolicy(scopeCtx, domain.Policy{RoleID: roleID, Method: "GET", Path: "/api/admin/v1/iam/users", Effect: domain.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	if err := persistence.SaveDataScope(scopeCtx, domain.DataScope{RoleID: roleID, Resource: "gvba_iam_users", Scope: domain.ScopeOrg, IDs: []string{"org-a"}}); err != nil {
		t.Fatal(err)
	}
	if err := persistence.SaveMenu(scopeCtx, domain.Menu{ID: menuID, Name: "Users", Path: "/users", Visible: true, Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := persistence.SavePermission(scopeCtx, domain.Permission{ID: permissionID, Name: "List users", Method: "GET", Path: "/api/admin/v1/iam/users", Active: true}); err != nil {
		t.Fatal(err)
	}

	loaded, err := persistence.FindUser(scopeCtx, fmt.Sprint(userID))
	if err != nil || len(loaded.RoleIDs) != 1 || loaded.RoleIDs[0] != roleID {
		t.Fatalf("loaded user=%+v err=%v", loaded, err)
	}
	service := iamapp.NewService(nil)
	service.Users = persistence
	service.Roles = persistence
	service.Menus = persistence
	service.Permissions = persistence
	service.Policies = persistence
	service.DataScopes = persistence
	service.Authorizer = domain.NewAuthorizer(persistence)
	service.Scopes = domain.NewMemoryDataScopeResolver(persistence)
	ok, err := service.Authorize(scopeCtx, domain.Subject{UserID: fmt.Sprint(userID), RoleIDs: []string{roleID}}, domain.Request{Method: "GET", Path: "/api/admin/v1/iam/users"})
	if err != nil || !ok {
		t.Fatalf("persistent authorization ok=%v err=%v", ok, err)
	}
	dataScope, err := service.ResolveDataScope(scopeCtx, domain.Subject{UserID: fmt.Sprint(userID), RoleIDs: []string{roleID}}, "gvba_iam_users")
	if err != nil || dataScope.Scope != domain.ScopeOrg || len(dataScope.IDs) != 1 {
		t.Fatalf("persistent scope=%+v err=%v", dataScope, err)
	}
}
