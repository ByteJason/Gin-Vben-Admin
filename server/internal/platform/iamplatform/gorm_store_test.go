package iamplatform

import (
	"context"
	"errors"
	"testing"
	"time"

	"example.com/gin-vben-admin/server/internal/domain/authdomain"
	domain "example.com/gin-vben-admin/server/internal/domain/iam"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
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
