package iamplatform

import (
	"context"
	"errors"
	"testing"

	domain "example.com/gin-vben-admin/server/internal/domain/iam"
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
