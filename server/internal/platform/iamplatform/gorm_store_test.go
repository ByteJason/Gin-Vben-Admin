package iamplatform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	gormmysql "gorm.io/driver/mysql"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestGORMStoreNilDependencyReturnsStableErrors(t *testing.T) {
	store := NewGORMStore(nil)
	if _, err := store.FindUser(context.Background(), "1"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("FindUser error=%v", err)
	}
	if err := store.SaveRole(context.Background(), domain.Role{ID: "r1", Name: "Reader"}); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("SaveRole error=%v", err)
	}
}

func TestGORMStoreRejectsNonNumericUserIDs(t *testing.T) {
	store := NewGORMStore(nil)
	if _, err := store.FindUser(context.Background(), "user-1"); !errors.Is(err, ErrInvalidNumericID) {
		t.Fatalf("FindUser error=%v", err)
	}
}

func TestIAMPersistenceRequiresTenantContext(t *testing.T) {
	_, err := tenantID(context.Background())
	if !errors.Is(err, tenant.ErrTenantContextMissing) {
		t.Fatalf("tenantID() error=%v, want tenant context missing", err)
	}
}

func TestGORMStoreUserPageRejectsInvalidQueryBeforeDatabaseAccess(t *testing.T) {
	store := NewGORMStore(nil)
	if _, err := store.ListUsersPage(tenant.WithContext(context.Background(), tenant.Context{TenantID: "default"}), domain.UserListQuery{PageSize: 101}); !errors.Is(err, domain.ErrInvalidUserQuery) {
		t.Fatalf("ListUsersPage() error = %v, want ErrInvalidUserQuery", err)
	}
}

func TestRoleListQueryKeepsGlobalAndCurrentOrganizationOnly(t *testing.T) {
	database, err := gormdb.Open(gormdb.Options{
		Driver: "postgres", Mode: gormdb.ModeSingle,
		DSN: "host=127.0.0.1 port=1 user=fixture dbname=fixture sslmode=disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	scope := tenant.Context{TenantID: "tenant-a", Organization: "org-a"}
	ctx := tenant.WithContext(context.Background(), scope)
	query := scopedRoleListQuery(database.Read(ctx).Session(&gorm.Session{DryRun: true}).Table("gvba_iam_roles"), scope)
	statement := query.Find(&[]roleRow{}).Statement
	sql := statement.SQL.String()
	if !strings.Contains(sql, "tenant_id =") || !strings.Contains(sql, "org_id =") || !strings.Contains(sql, "org_id IS NULL") {
		t.Fatalf("scoped role SQL=%s vars=%v", sql, statement.Vars)
	}
	if len(statement.Vars) != 2 || statement.Vars[0] != "tenant-a" || statement.Vars[1] != "org-a" {
		t.Fatalf("scoped role vars=%v", statement.Vars)
	}
}

func TestActiveRoleAuthorizationQueryFiltersStatusTenantAndOrganizationAcrossDialects(t *testing.T) {
	for _, dialect := range []struct {
		name string
		open gorm.Dialector
	}{
		{name: "mysql", open: gormmysql.New(gormmysql.Config{DSN: "iam:iam@tcp(127.0.0.1:1)/iam", SkipInitializeWithVersion: true})},
		{name: "postgres", open: gormpostgres.New(gormpostgres.Config{DSN: "host=127.0.0.1 port=1 user=iam password=iam dbname=iam sslmode=disable", PreferSimpleProtocol: true})},
	} {
		t.Run(dialect.name, func(t *testing.T) {
			database, err := gorm.Open(dialect.open, &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
			if err != nil {
				t.Fatal(err)
			}
			for _, scope := range []tenant.Context{
				{TenantID: "tenant-a"},
				{TenantID: "tenant-a", Organization: "org-a"},
			} {
				result := activeRoleIDsQuery(database, scope, 7).Order("ur.role_id ASC").Find(&[]activeRoleIDRow{})
				if result.Error != nil {
					t.Fatal(result.Error)
				}
				sql := strings.ToUpper(result.Statement.SQL.String())
				for _, fragment := range []string{"JOIN GVBA_IAM_ROLES AS R", "R.TENANT_ID = UR.TENANT_ID", "R.STATUS =", "UR.TENANT_ID =", "UR.USER_ID ="} {
					if !strings.Contains(sql, fragment) {
						t.Fatalf("scope=%+v SQL missing %q: %s", scope, fragment, sql)
					}
				}
				if scope.Organization == "" {
					if !strings.Contains(sql, "R.ORG_ID IS NULL AND UR.ORG_ID IS NULL") {
						t.Fatalf("tenant-wide SQL leaked organization roles: %s", sql)
					}
				} else if !strings.Contains(sql, "R.ORG_ID IS NULL OR R.ORG_ID =") || !strings.Contains(sql, "UR.ORG_ID IS NULL OR UR.ORG_ID =") {
					t.Fatalf("organization SQL missing global/current-org bounds: %s", sql)
				}
			}
		})
	}
}

func TestAuthorizationDataScopeQueryPinsPrimary(t *testing.T) {
	database, err := gormdb.Open(gormdb.Options{
		Driver: "postgres", Mode: gormdb.ModeSingle,
		DSN: "host=127.0.0.1 port=1 user=fixture dbname=fixture sslmode=disable",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	scope := tenant.Context{TenantID: "tenant-a", Organization: "org-a"}
	ctx := tenant.WithContext(context.Background(), scope)
	query := NewGORMStore(database).authorizationDataScopesQuery(ctx, scope)
	if query == nil {
		t.Fatal("authorization data-scope query is nil")
	}
	if _, pinned := query.Statement.Settings.Load("gorm:db_resolver:write"); !pinned {
		t.Fatal("authorization data-scope query is not pinned to the primary")
	}
	if _, staleRead := query.Statement.Settings.Load("gorm:db_resolver:read"); staleRead {
		t.Fatal("authorization data-scope query retained the replica marker")
	}
}

func TestUserRowMapsProfileFieldsAndNormalizedUpdate(t *testing.T) {
	lastLogin := time.Unix(100, 0).UTC()
	passwordChanged := time.Unix(90, 0).UTC()
	row := userRow{
		ID: 7, Username: "Alice", UsernameNormalized: stringPtr("alice"),
		Email: stringPtr("Alice@Example.TEST"), EmailNormalized: stringPtr("alice@example.test"),
		Nickname: stringPtr("Alice A"), Avatar: stringPtr("avatar-key"), Phone: stringPtr("+8613800138000"),
		LastLoginIP: stringPtr("192.0.2.9"), LastLoginAt: &lastLogin, PasswordChangedAt: &passwordChanged,
		Status: "active",
	}
	got := row.toDomain([]string{"role-reader"})
	if got.ID != "7" || got.UsernameNormalized != "alice" || got.Email != "Alice@Example.TEST" || got.EmailNormalized != "alice@example.test" || got.Nickname != "Alice A" || got.Avatar != "avatar-key" || got.Phone != "+8613800138000" || got.LastLoginIP != "192.0.2.9" || !got.LastLoginAt.Equal(lastLogin) || !got.PasswordChangedAt.Equal(passwordChanged) || len(got.RoleIDs) != 1 {
		t.Fatalf("mapped user = %+v", got)
	}
	values, err := profileUpdateValues(domain.User{Username: " Alice ", Email: " Alice@Example.TEST ", Nickname: "Alice A", Avatar: "avatar-key", Phone: "+8613800138000", Active: true})
	if err != nil {
		t.Fatalf("profileUpdateValues() error = %v", err)
	}
	if values["username_normalized"] != "alice" || values["email_normalized"] != "alice@example.test" || values["email"] != "Alice@Example.TEST" {
		t.Fatalf("normalized update values = %#v", values)
	}
	if _, err := profileUpdateValues(domain.User{Username: "alice", Phone: "13800138000"}); !errors.Is(err, authdomain.ErrInvalidPhone) {
		t.Fatalf("profileUpdateValues(invalid phone) error = %v, want ErrInvalidPhone", err)
	}
}

var _ interface {
	FindUser(context.Context, string) (domain.User, error)
	SaveUser(context.Context, domain.User) error
	ListUsers(context.Context) ([]domain.User, error)
	FindRole(context.Context, string) (domain.Role, error)
	SaveRole(context.Context, domain.Role) error
	ListRoles(context.Context) ([]domain.Role, error)
	SaveMenu(context.Context, domain.Menu) error
	ListMenus(context.Context) ([]domain.Menu, error)
	SavePermission(context.Context, domain.Permission) error
	ListPermissions(context.Context) ([]domain.Permission, error)
	SavePolicy(context.Context, domain.Policy) error
	ListPolicies(context.Context) ([]domain.Policy, error)
	SaveDataScope(context.Context, domain.DataScope) error
	ListDataScopes(context.Context) ([]domain.DataScope, error)
} = (*GORMStore)(nil)

var _ interface {
	AssignRoleUsers(context.Context, string, []string) (domain.Role, error)
	AssignRoleDataScopes(context.Context, string, []domain.RoleDataScopeBinding) (domain.Role, error)
} = (*GORMStore)(nil)

func TestGORMStoreDataScopeReplacementValidatesBeforeUnavailableDependency(t *testing.T) {
	store := NewGORMStore(nil)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "default"})
	if _, err := store.AssignRoleDataScopes(ctx, "role", []domain.RoleDataScopeBinding{{Resource: "", Scope: domain.ScopeOwn}}); !errors.Is(err, domain.ErrInvalidDataScope) {
		t.Fatalf("invalid data scope error = %v", err)
	}
	if _, err := store.AssignRoleDataScopes(ctx, "role", []domain.RoleDataScopeBinding{{Resource: "orders", Scope: domain.ScopeOwn}}); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("unavailable data scope error = %v", err)
	}
}

func TestGORMStoreUserWriteValidationRunsBeforeUnavailableDependency(t *testing.T) {
	store := NewGORMStore(nil)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "default"})
	if _, err := store.CreateUser(ctx, domain.User{Username: "alice"}); !errors.Is(err, domain.ErrInvalidUser) {
		t.Fatalf("CreateUser() error = %v, want ErrInvalidUser", err)
	}
	if _, err := store.UpdateUser(ctx, domain.User{ID: "not-numeric", Username: "alice"}); !errors.Is(err, ErrInvalidNumericID) {
		t.Fatalf("UpdateUser() error = %v, want ErrInvalidNumericID", err)
	}
}

func TestGORMStoreSoftDeleteValidatesIDAndDependency(t *testing.T) {
	store := NewGORMStore(nil)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "default"})
	if _, err := store.SoftDeleteUser(ctx, "not-numeric"); !errors.Is(err, ErrInvalidNumericID) {
		t.Fatalf("SoftDeleteUser(non-numeric) error = %v, want ErrInvalidNumericID", err)
	}
	if _, err := store.SoftDeleteUser(ctx, "7"); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("SoftDeleteUser(unavailable) error = %v, want ErrStoreUnavailable", err)
	}
}

func TestGORMStoreBatchStatusValidatesBeforeUnavailableDependency(t *testing.T) {
	store := NewGORMStore(nil)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "default"})
	if _, err := store.UpdateUserStatuses(ctx, []domain.UserStatusChange{{ID: "not-numeric", Active: false}}); !errors.Is(err, domain.ErrInvalidUser) {
		t.Fatalf("UpdateUserStatuses(non-numeric) error = %v, want ErrInvalidUser", err)
	}
	if _, err := store.UpdateUserStatuses(ctx, []domain.UserStatusChange{{ID: "7", Active: false}}); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("UpdateUserStatuses(unavailable) error = %v, want ErrStoreUnavailable", err)
	}
}

func TestGORMStoreResetPasswordValidatesBeforeUnavailableDependency(t *testing.T) {
	store := NewGORMStore(nil)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "default"})
	if _, err := store.UpdateUserPassword(ctx, "not-numeric", "encoded", time.Now().UTC()); !errors.Is(err, ErrInvalidNumericID) {
		t.Fatalf("UpdateUserPassword(non-numeric) error = %v, want ErrInvalidNumericID", err)
	}
	if _, err := store.UpdateUserPassword(ctx, "7", "", time.Now().UTC()); !errors.Is(err, domain.ErrInvalidUser) {
		t.Fatalf("UpdateUserPassword(empty hash) error = %v, want ErrInvalidUser", err)
	}
	if _, err := store.UpdateUserPassword(ctx, "7", "encoded", time.Time{}); !errors.Is(err, domain.ErrInvalidUser) {
		t.Fatalf("UpdateUserPassword(zero timestamp) error = %v, want ErrInvalidUser", err)
	}
	if _, err := store.UpdateUserPassword(ctx, "7", "encoded", time.Now().UTC()); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("UpdateUserPassword(unavailable) error = %v, want ErrStoreUnavailable", err)
	}
}

func TestGORMStoreRoleAssignmentValidatesBeforeUnavailableDependency(t *testing.T) {
	store := NewGORMStore(nil)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "default"})
	if _, err := store.AssignRoleUsers(ctx, "role-editor", []string{"not-numeric"}); !errors.Is(err, domain.ErrInvalidUser) {
		t.Fatalf("AssignRoleUsers(non-numeric user) error = %v, want ErrInvalidUser", err)
	}
	if _, err := store.AssignRoleUsers(ctx, "", []string{}); !errors.Is(err, domain.ErrInvalidUser) {
		t.Fatalf("AssignRoleUsers(empty role) error = %v, want ErrInvalidUser", err)
	}
	if _, err := store.AssignRoleUsers(ctx, "role-editor", []string{"7"}); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("AssignRoleUsers(unavailable) error = %v, want ErrStoreUnavailable", err)
	}
}
