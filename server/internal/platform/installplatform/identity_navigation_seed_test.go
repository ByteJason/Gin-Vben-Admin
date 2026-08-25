package installplatform

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	iamapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/iam"
	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	iamdomain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/DATA-DOG/go-sqlmock"
	gormmysql "gorm.io/driver/mysql"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestInitialNavigationSeedIsIdempotentAndContainsFourProductionGroups(t *testing.T) {
	store := &memoryNavigationSeedStore{
		menus:       make(map[string]initialMenuSeed),
		permissions: make(map[string]initialPermissionSeed),
	}
	first, err := seedInitialNavigation(store)
	if err != nil {
		t.Fatalf("first seed: %v", err)
	}
	second, err := seedInitialNavigation(store)
	if err != nil {
		t.Fatalf("second seed: %v", err)
	}
	_, permissionSeeds := initialNavigationSeeds()
	if len(first.MenuIDs) != 18 || len(first.PermissionIDs) != len(permissionSeeds) || len(second.MenuIDs) != 0 || len(second.PermissionIDs) != 0 {
		t.Fatalf("seed receipts first=%+v second=%+v", first, second)
	}

	if len(store.menus) != 18 || len(store.permissions) != len(permissionSeeds) {
		t.Fatalf("seed counts menus=%d permissions=%d", len(store.menus), len(store.permissions))
	}
	wantRoots := []string{"menu-identity", "menu-operations", "menu-overview", "menu-system-config"}
	var roots []string
	childCounts := make(map[string]int)
	for id, menu := range store.menus {
		if menu.ParentID == "" {
			roots = append(roots, id)
		} else {
			childCounts[menu.ParentID]++
		}
		if strings.Contains(menu.Path, "profile") || strings.Contains(menu.Path, "workspace") || strings.Contains(menu.Path, "policies") || strings.Contains(menu.Path, "data-scopes") || strings.Contains(menu.Path, "demo") {
			t.Fatalf("non-production route was seeded: %+v", menu)
		}
		if menu.Permission != "" {
			if _, ok := store.permissions[menu.Permission]; !ok {
				t.Fatalf("menu %s references missing permission %q", id, menu.Permission)
			}
		}
	}
	sort.Strings(roots)
	if !reflect.DeepEqual(roots, wantRoots) {
		t.Fatalf("roots=%v want=%v", roots, wantRoots)
	}
	if childCounts["menu-overview"] != 1 || childCounts["menu-identity"] != 4 || childCounts["menu-system-config"] != 5 || childCounts["menu-operations"] != 4 {
		t.Fatalf("child counts=%v", childCounts)
	}
	for id, wantName := range map[string]string{
		"menu-system-settings":    "系统设置",
		"menu-system-mail":        "邮件服务",
		"menu-operations-monitor": "资源监控",
	} {
		if got := store.menus[id].Name; got != wantName {
			t.Fatalf("menu %s name=%q want=%q", id, got, wantName)
		}
	}
	if got := store.permissions["dashboard:overview:read"].Path; got != "/api/admin/v1/dashboard/summary" {
		t.Fatalf("dashboard overview permission path=%q", got)
	}

	methodPaths := make(map[string]string, len(store.permissions))
	for code, permission := range store.permissions {
		if !permission.Active || strings.TrimSpace(code) == "" || strings.TrimSpace(permission.Method) == "" || strings.TrimSpace(permission.Path) == "" {
			t.Fatalf("invalid permission seed: %+v", permission)
		}
		key := permission.Method + " " + permission.Path
		if previous, exists := methodPaths[key]; exists {
			t.Fatalf("duplicate method/path %s for %s and %s", key, previous, code)
		}
		methodPaths[key] = code
	}
}

func TestFilterKnownInitialNavigationSeedIDsRejectsUnownedMetadata(t *testing.T) {
	menus, permissions := filterKnownInitialNavigationSeedIDs(
		[]string{"menu-overview", "attacker-menu", "menu-overview"},
		[]string{"iam:users:read", "attacker:delete", "iam:users:read"},
	)
	if !reflect.DeepEqual(menus, []string{"menu-overview"}) || !reflect.DeepEqual(permissions, []string{"iam:users:read"}) {
		t.Fatalf("filtered menus=%v permissions=%v", menus, permissions)
	}
}

func TestInitialNavigationSeedRejectsConflictingExistingResource(t *testing.T) {
	menus, permissions := initialNavigationSeeds()
	store := &memoryNavigationSeedStore{
		menus:       map[string]initialMenuSeed{menus[0].ID: menus[0]},
		permissions: map[string]initialPermissionSeed{permissions[0].ID: permissions[0]},
	}
	conflictingMenu := store.menus[menus[0].ID]
	conflictingMenu.Name = "冲突菜单"
	store.menus[menus[0].ID] = conflictingMenu

	if _, err := seedInitialNavigation(store); !errors.Is(err, errNavigationSeedConflict) {
		t.Fatalf("seedInitialNavigation() error=%v, want navigation seed conflict", err)
	}
	if got := store.menus[menus[0].ID].Name; got != "冲突菜单" {
		t.Fatalf("conflicting existing menu was overwritten: %q", got)
	}
}

func TestNavigationSeedConflictDiagnosticContainsOnlyBoundedResourceIdentity(t *testing.T) {
	err := newNavigationSeedConflict("menu", "menu-system-settings")
	if !errors.Is(err, errNavigationSeedConflict) {
		t.Fatalf("error=%v, want navigation seed conflict sentinel", err)
	}
	var provider interface {
		InstallationFailureDiagnostic() installer.FailureDiagnostic
	}
	if !errors.As(err, &provider) {
		t.Fatalf("error=%T %v, want diagnostic provider", err, err)
	}
	got := provider.InstallationFailureDiagnostic()
	if got.Reason != "navigation_seed_conflict" || got.Operation != "apply" ||
		got.ResourceKind != "menu" || got.ResourceID != "menu-system-settings" {
		t.Fatalf("diagnostic=%#v", got)
	}

	unsafe := newNavigationSeedConflict("table", "menu-system-settings\npassword=secret")
	unsafeDiagnostic := unsafe.(interface {
		InstallationFailureDiagnostic() installer.FailureDiagnostic
	}).InstallationFailureDiagnostic()
	if unsafeDiagnostic.ResourceKind != "" || unsafeDiagnostic.ResourceID != "" {
		t.Fatalf("unsafe diagnostic was not redacted: %#v", unsafeDiagnostic)
	}
}

func TestNavigationSeedInsertSQLIsPortableAndHasNoDanglingConflictClause(t *testing.T) {
	menus, permissions := initialNavigationSeeds()
	for _, tt := range []struct {
		name      string
		dialector gorm.Dialector
	}{
		{
			name: "mysql",
			dialector: gormmysql.New(gormmysql.Config{
				DSN: "seed:seed@tcp(127.0.0.1:1)/seed", SkipInitializeWithVersion: true,
			}),
		},
		{
			name: "postgres",
			dialector: gormpostgres.New(gormpostgres.Config{
				DSN: "host=127.0.0.1 port=1 user=seed password=seed dbname=seed sslmode=disable", PreferSimpleProtocol: true,
			}),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			database, err := gorm.Open(tt.dialector, &gorm.Config{
				DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			store := gormNavigationSeedStore{tx: database}
			for _, result := range []*gorm.DB{store.createMenu(menus[0]), store.createPermission(permissions[0])} {
				if result.Error != nil {
					t.Fatalf("create seed SQL: %v", result.Error)
				}
				sql := strings.ToUpper(strings.TrimSpace(result.Statement.SQL.String()))
				if !strings.HasPrefix(sql, "INSERT INTO") {
					t.Fatalf("SQL is not an INSERT: %q", sql)
				}
				if strings.Contains(sql, "ON DUPLICATE KEY UPDATE") || strings.Contains(sql, "ON CONFLICT") {
					t.Fatalf("seed ownership must not rely on a conflict clause: %q", sql)
				}
			}
		})
	}
}

func TestNavigationSeedRowMatchRequiresExactNullableRepresentation(t *testing.T) {
	menus, permissions := initialNavigationSeeds()
	menu := menus[0]
	row := navigationMenuSeedRow{
		ID: menu.ID, TenantID: initialTenantID, ParentID: optionalSeedString(menu.ParentID), Name: menu.Name, Path: menu.Path, MenuType: menu.Type,
		Component: optionalSeedString(menu.Component), Redirect: optionalSeedString(menu.Redirect), Icon: optionalSeedString(menu.Icon), Permission: optionalSeedString(menu.Permission),
		SortOrder: menu.Sort, Visible: menu.Visible, Status: statusForSeed(menu.Active), KeepAlive: menu.KeepAlive, External: menu.External,
	}
	if !row.matches(menu) {
		t.Fatal("canonical menu row should match")
	}
	empty := ""
	row.Component = &empty
	if row.matches(menu) {
		t.Fatal("empty string must not be treated as canonical NULL")
	}

	permission := permissions[0]
	permissionRow := navigationPermissionSeedRow{
		ID: permission.ID, TenantID: initialTenantID, Name: permission.Name, Method: permission.Method,
		Path: permission.Path, Status: statusForSeed(permission.Active),
	}
	if !permissionRow.matches(permission) {
		t.Fatal("canonical permission row should match")
	}
	space := " "
	permissionRow.OrgID = &space
	if permissionRow.matches(permission) {
		t.Fatal("blank organization must not be treated as canonical NULL")
	}
}

func TestGORMNavigationSeedMenuAcceptsOnlyOneCanonicalExistingRow(t *testing.T) {
	menus, _ := initialNavigationSeeds()
	seed := menus[0]
	canonical := navigationMenuRowForSeed(seed)
	globalIDConflict := canonical
	globalIDConflict.TenantID = "another-tenant"
	tenantPathConflict := canonical
	tenantPathConflict.ID = "legacy-menu-id"
	globalIDAndPathConflict := canonical
	globalIDAndPathConflict.Path = "/legacy-path"

	tests := []struct {
		name         string
		rows         []navigationMenuSeedRow
		wantConflict bool
	}{
		{name: "canonical row is idempotent", rows: []navigationMenuSeedRow{canonical}},
		{name: "global id belongs to another tenant", rows: []navigationMenuSeedRow{globalIDConflict}, wantConflict: true},
		{name: "tenant path belongs to another id", rows: []navigationMenuSeedRow{tenantPathConflict}, wantConflict: true},
		{name: "global id and tenant path resolve to different rows", rows: []navigationMenuSeedRow{globalIDAndPathConflict, tenantPathConflict}, wantConflict: true},
	}
	for _, dialect := range navigationSeedSQLDialects() {
		for _, tt := range tests {
			t.Run(dialect.name+"/"+tt.name, func(t *testing.T) {
				store, mock := newNavigationSeedSQLMock(t, dialect)
				mock.ExpectQuery(dialect.menuLookup).
					WithArgs(seed.ID, initialTenantID, seed.Path).
					WillReturnRows(navigationMenuSQLRows(tt.rows...))

				created, err := store.EnsureMenu(seed)
				if tt.wantConflict {
					assertNavigationSeedConflict(t, err, "menu", seed.ID)
				} else if err != nil {
					t.Fatalf("EnsureMenu() error=%v", err)
				}
				if created {
					t.Fatal("EnsureMenu() created a row despite a pre-existing identity/unique-key match")
				}
			})
		}
	}
}

func TestGORMNavigationSeedPermissionAcceptsOnlyOneCanonicalExistingRow(t *testing.T) {
	_, permissions := initialNavigationSeeds()
	seed := permissions[0]
	canonical := navigationPermissionRowForSeed(seed)
	globalIDConflict := canonical
	globalIDConflict.TenantID = "another-tenant"
	globalIDConflict.Method = "POST"
	methodPathConflict := canonical
	methodPathConflict.ID = "legacy:permission:id"
	methodPathConflict.TenantID = "another-tenant"

	tests := []struct {
		name         string
		rows         []navigationPermissionSeedRow
		wantConflict bool
	}{
		{name: "canonical row is idempotent", rows: []navigationPermissionSeedRow{canonical}},
		{name: "global id belongs to another tenant", rows: []navigationPermissionSeedRow{globalIDConflict}, wantConflict: true},
		{name: "global method path belongs to another id", rows: []navigationPermissionSeedRow{methodPathConflict}, wantConflict: true},
		{name: "global id and method path resolve to different rows", rows: []navigationPermissionSeedRow{globalIDConflict, methodPathConflict}, wantConflict: true},
	}
	for _, dialect := range navigationSeedSQLDialects() {
		for _, tt := range tests {
			t.Run(dialect.name+"/"+tt.name, func(t *testing.T) {
				store, mock := newNavigationSeedSQLMock(t, dialect)
				mock.ExpectQuery(dialect.permissionLookup).
					WithArgs(seed.ID, seed.Method, seed.Path).
					WillReturnRows(navigationPermissionSQLRows(tt.rows...))

				created, err := store.EnsurePermission(seed)
				if tt.wantConflict {
					assertNavigationSeedConflict(t, err, "permission", seed.ID)
				} else if err != nil {
					t.Fatalf("EnsurePermission() error=%v", err)
				}
				if created {
					t.Fatal("EnsurePermission() created a row despite a pre-existing identity/unique-key match")
				}
			})
		}
	}
}

type navigationSeedSQLDialect struct {
	name             string
	dialector        func(*sql.DB) gorm.Dialector
	menuLookup       string
	permissionLookup string
}

func navigationSeedSQLDialects() []navigationSeedSQLDialect {
	return []navigationSeedSQLDialect{
		{
			name: "mysql",
			dialector: func(database *sql.DB) gorm.Dialector {
				return gormmysql.New(gormmysql.Config{Conn: database, SkipInitializeWithVersion: true})
			},
			menuLookup:       "SELECT * FROM `menus` WHERE id = ? OR (tenant_id = ? AND path = ?)",
			permissionLookup: "SELECT * FROM `permissions` WHERE id = ? OR (method = ? AND path = ?)",
		},
		{
			name: "postgres",
			dialector: func(database *sql.DB) gorm.Dialector {
				return gormpostgres.New(gormpostgres.Config{Conn: database, PreferSimpleProtocol: true})
			},
			menuLookup:       `SELECT * FROM "menus" WHERE id = $1 OR (tenant_id = $2 AND path = $3)`,
			permissionLookup: `SELECT * FROM "permissions" WHERE id = $1 OR (method = $2 AND path = $3)`,
		},
	}
}

func newNavigationSeedSQLMock(t *testing.T, dialect navigationSeedSQLDialect) (gormNavigationSeedStore, sqlmock.Sqlmock) {
	t.Helper()
	database, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatal(err)
	}
	gormDatabase, err := gorm.Open(dialect.dialector(database), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		_ = database.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet SQL expectation: %v", err)
		}
		mock.ExpectClose()
		if err := database.Close(); err != nil {
			t.Errorf("close SQL mock: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("unmet SQL close expectation: %v", err)
		}
	})
	return gormNavigationSeedStore{tx: gormDatabase}, mock
}

func navigationMenuRowForSeed(seed initialMenuSeed) navigationMenuSeedRow {
	return navigationMenuSeedRow{
		ID: seed.ID, TenantID: initialTenantID, ParentID: optionalSeedString(seed.ParentID), Name: seed.Name, Path: seed.Path,
		MenuType: seed.Type, Component: optionalSeedString(seed.Component), Redirect: optionalSeedString(seed.Redirect),
		Icon: optionalSeedString(seed.Icon), Permission: optionalSeedString(seed.Permission), SortOrder: seed.Sort,
		Visible: seed.Visible, Status: statusForSeed(seed.Active), KeepAlive: seed.KeepAlive, External: seed.External,
	}
}

func navigationPermissionRowForSeed(seed initialPermissionSeed) navigationPermissionSeedRow {
	return navigationPermissionSeedRow{
		ID: seed.ID, TenantID: initialTenantID, Name: seed.Name, Method: seed.Method,
		Path: seed.Path, Status: statusForSeed(seed.Active),
	}
}

func navigationMenuSQLRows(rows ...navigationMenuSeedRow) *sqlmock.Rows {
	result := sqlmock.NewRows([]string{
		"id", "tenant_id", "org_id", "parent_id", "name", "path", "menu_type", "component", "redirect", "icon", "permission",
		"sort_order", "visible", "status", "keep_alive", "external",
	})
	for _, row := range rows {
		result.AddRow([]driver.Value{
			row.ID, row.TenantID, nullableSeedSQLValue(row.OrgID), nullableSeedSQLValue(row.ParentID), row.Name, row.Path,
			row.MenuType, nullableSeedSQLValue(row.Component), nullableSeedSQLValue(row.Redirect), nullableSeedSQLValue(row.Icon),
			nullableSeedSQLValue(row.Permission), int64(row.SortOrder), row.Visible, row.Status, row.KeepAlive, row.External,
		}...)
	}
	return result
}

func navigationPermissionSQLRows(rows ...navigationPermissionSeedRow) *sqlmock.Rows {
	result := sqlmock.NewRows([]string{"id", "tenant_id", "org_id", "name", "method", "path", "status"})
	for _, row := range rows {
		result.AddRow(
			row.ID, row.TenantID, nullableSeedSQLValue(row.OrgID), row.Name, row.Method, row.Path, row.Status,
		)
	}
	return result
}

func nullableSeedSQLValue(value *string) driver.Value {
	if value == nil {
		return nil
	}
	return *value
}

func assertNavigationSeedConflict(t *testing.T, err error, resourceKind, resourceID string) {
	t.Helper()
	if !errors.Is(err, errNavigationSeedConflict) {
		t.Fatalf("error=%v, want navigation seed conflict", err)
	}
	var provider installer.FailureDiagnosticProvider
	if !errors.As(err, &provider) {
		t.Fatalf("error=%T %v, want failure diagnostic provider", err, err)
	}
	diagnostic := provider.InstallationFailureDiagnostic()
	if diagnostic.ResourceKind != resourceKind || diagnostic.ResourceID != resourceID {
		t.Fatalf("diagnostic=%#v, want resource %s/%s", diagnostic, resourceKind, resourceID)
	}
}

func TestInitialSuperAdministratorWildcardResolvesEveryActiveSeedPermission(t *testing.T) {
	_, seeds := initialNavigationSeeds()
	permissions := make([]iamdomain.Permission, 0, len(seeds))
	for _, seed := range seeds {
		permissions = append(permissions, iamdomain.Permission{
			ID: seed.ID, Name: seed.Name, Method: seed.Method, Path: seed.Path, Active: seed.Active,
		})
	}
	policies := iamdomain.NewMemoryPolicyStore()
	if err := policies.AddPolicy(iamdomain.Policy{
		RoleID: installationRoleID, Method: "*", Path: "*", Effect: iamdomain.EffectAllow,
	}); err != nil {
		t.Fatal(err)
	}
	service := iamapp.NewServiceWithRepositories(nil, nil, nil, seedPermissionRepository{permissions: permissions}, policies, nil)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: initialTenantID})
	codes, err := service.ListAccessCodes(ctx, iamdomain.Subject{UserID: "1", RoleIDs: []string{installationRoleID}})
	if err != nil {
		t.Fatalf("ListAccessCodes() error = %v", err)
	}
	want := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		if seed.Active {
			want = append(want, seed.ID)
		}
	}
	sort.Strings(want)
	if !reflect.DeepEqual(codes, want) {
		t.Fatalf("codes=%v want=%v", codes, want)
	}
}

type memoryNavigationSeedStore struct {
	menus       map[string]initialMenuSeed
	permissions map[string]initialPermissionSeed
}

type seedPermissionRepository struct{ permissions []iamdomain.Permission }

func (r seedPermissionRepository) ListPermissions(context.Context) ([]iamdomain.Permission, error) {
	return append([]iamdomain.Permission(nil), r.permissions...), nil
}

func (s *memoryNavigationSeedStore) EnsureMenu(menu initialMenuSeed) (bool, error) {
	if existing, exists := s.menus[menu.ID]; !exists {
		s.menus[menu.ID] = menu
		return true, nil
	} else if existing != menu {
		return false, errNavigationSeedConflict
	}
	return false, nil
}

func (s *memoryNavigationSeedStore) EnsurePermission(permission initialPermissionSeed) (bool, error) {
	if existing, exists := s.permissions[permission.ID]; !exists {
		s.permissions[permission.ID] = permission
		return true, nil
	} else if existing != permission {
		return false, errNavigationSeedConflict
	}
	return false, nil
}
