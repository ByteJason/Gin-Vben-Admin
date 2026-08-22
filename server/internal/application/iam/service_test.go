package iam

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domain "example.com/gin-vben-admin/server/internal/domain/iam"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
)

func TestServiceAuthorizesRolePolicyAndScopes(t *testing.T) {
	store := NewMemoryStore()
	if err := store.SaveUser(context.Background(), domain.User{ID: "u1", Username: "alice", Active: true, RoleIDs: []string{"role-reader"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRole(context.Background(), domain.Role{ID: "role-reader", Name: "Reader", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePolicy(context.Background(), domain.Policy{RoleID: "role-reader", Method: "GET", Path: "/orders", Effect: domain.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDataScope(context.Background(), domain.DataScope{RoleID: "role-reader", Resource: "orders", Scope: domain.ScopeOrg, IDs: []string{"org-a"}}); err != nil {
		t.Fatal(err)
	}
	service := NewService(store)
	ok, err := service.Authorize(context.Background(), domain.Subject{UserID: "u1", RoleIDs: []string{"role-reader"}}, domain.Request{Method: "GET", Path: "/orders"})
	if err != nil || !ok {
		t.Fatalf("authorize ok=%v err=%v", ok, err)
	}
	scope, err := service.ResolveDataScope(context.Background(), domain.Subject{UserID: "u1", RoleIDs: []string{"role-reader"}}, "orders")
	if err != nil || scope.Scope != domain.ScopeOrg || scope.IDs[0] != "org-a" {
		t.Fatalf("scope=%+v err=%v", scope, err)
	}
}

func TestMemoryStoreNotFoundAndCopiesValues(t *testing.T) {
	store := NewMemoryStore()
	if _, err := store.FindUser(context.Background(), "missing"); !errors.Is(err, domain.ErrResourceNotFound) {
		t.Fatalf("missing user error=%v", err)
	}
	user := domain.User{ID: "u1", Username: "alice", Active: true, RoleIDs: []string{"r1"}}
	if err := store.SaveUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	user.RoleIDs[0] = "mutated"
	got, err := store.FindUser(context.Background(), "u1")
	if err != nil || got.RoleIDs[0] != "r1" {
		t.Fatalf("stored user alias leaked: %+v err=%v", got, err)
	}
}

func TestServiceRejectsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := NewService(NewMemoryStore())
	if _, err := service.Authorize(ctx, domain.Subject{UserID: "u1"}, domain.Request{Method: "GET", Path: "/"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("authorize cancelled error=%v", err)
	}
}

func TestServiceExposesManagementCollections(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(store)
	if err := service.SaveMenu(context.Background(), domain.Menu{ID: "menu-home", Name: "Home", Path: "/home", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := service.SavePermission(context.Background(), domain.Permission{ID: "perm-home", Name: "Home", Method: "GET", Path: "/home", Active: true}); err != nil {
		t.Fatal(err)
	}
	menus, err := service.ListMenus(context.Background())
	if err != nil || len(menus) != 1 || menus[0].ID != "menu-home" {
		t.Fatalf("menus=%+v err=%v", menus, err)
	}
	permissions, err := service.ListPermissions(context.Background())
	if err != nil || len(permissions) != 1 || permissions[0].ID != "perm-home" {
		t.Fatalf("permissions=%+v err=%v", permissions, err)
	}
}

func TestServiceListUsersPageFiltersPaginatesAndKeepsTenantBoundary(t *testing.T) {
	store := NewMemoryStore()
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	users := []domain.User{
		{ID: "1", Username: "alice", DisplayName: "Alice", Email: "alice@example.test", TenantID: "tenant-a", OrgID: "org-a", Active: true, LastLoginAt: time.Unix(1, 0)},
		{ID: "2", Username: "albert", DisplayName: "Albert", Email: "albert@example.test", TenantID: "tenant-a", OrgID: "org-a", Active: true, LastLoginAt: time.Unix(2, 0)},
		{ID: "3", Username: "bob", DisplayName: "Bob", Email: "bob@example.test", TenantID: "tenant-a", OrgID: "org-b", Active: false, LastLoginAt: time.Unix(3, 0)},
		{ID: "4", Username: "alice-other", DisplayName: "Other", Email: "other@example.test", TenantID: "tenant-b", OrgID: "org-a", Active: true},
	}
	for _, user := range users {
		if err := store.SaveUser(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	page, err := NewService(store).ListUsersPage(ctx, domain.UserListQuery{
		Page: 1, PageSize: 1, Keyword: "AL", Status: "active", OrgID: "org-a", Sort: "-username",
	})
	if err != nil {
		t.Fatalf("ListUsersPage() error = %v", err)
	}
	if page.Total != 2 || page.Page != 1 || page.PageSize != 1 || len(page.Items) != 1 || page.Items[0].Username != "alice" {
		t.Fatalf("page = %+v", page)
	}
	if _, err := NewService(store).ListUsersPage(ctx, domain.UserListQuery{PageSize: 101}); !errors.Is(err, ErrInvalidUserQuery) {
		t.Fatalf("oversized page error = %v, want ErrInvalidUserQuery", err)
	}
	if _, err := NewService(store).ListUsersPage(ctx, domain.UserListQuery{OrgID: "org-b"}); !errors.Is(err, tenant.ErrOrganizationDenied) {
		t.Fatalf("cross-organization query error = %v, want organization denied", err)
	}
}

func TestUserListQueryDefaultsAndRejectsUnknownSortOrStatus(t *testing.T) {
	query, err := (domain.UserListQuery{}).Normalize()
	if err != nil || query.Page != 1 || query.PageSize != 20 || query.Sort != "id" {
		t.Fatalf("normalized defaults = %+v err=%v", query, err)
	}
	for _, invalid := range []domain.UserListQuery{{Page: -1}, {PageSize: 101}, {Status: "pending"}, {Sort: "username;drop"}} {
		if _, err := invalid.Normalize(); !errors.Is(err, domain.ErrInvalidUserQuery) {
			t.Fatalf("Normalize(%+v) error = %v, want ErrInvalidUserQuery", invalid, err)
		}
	}
}

func TestServiceListUsersPageRequiresTenantContextForLegacyRepository(t *testing.T) {
	if _, err := NewService(NewMemoryStore()).ListUsersPage(context.Background(), domain.UserListQuery{}); !errors.Is(err, tenant.ErrTenantContextMissing) {
		t.Fatalf("missing tenant error = %v, want tenant context missing", err)
	}
}

type recordingPasswordHasher struct {
	password string
}

func (h *recordingPasswordHasher) Hash(password string) (string, error) {
	h.password = password
	return "hashed:" + password, nil
}

func TestServiceCreateUserHashesPasswordAndAppliesTenantOrganizationScope(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(store)
	hasher := &recordingPasswordHasher{}
	service.SetPasswordHasher(hasher)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"})

	created, err := service.CreateUser(ctx, UserCreateInput{
		Username: " Alice ", Email: " Alice@Example.TEST ", Nickname: "Alice A", Password: "correct-password",
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if created.ID == "" || created.UsernameNormalized != "alice" || created.EmailNormalized != "alice@example.test" || created.TenantID != "tenant-a" || created.OrgID != "org-a" || created.PasswordHash != "hashed:correct-password" {
		t.Fatalf("created user = %+v", created)
	}
	if hasher.password != "correct-password" {
		t.Fatalf("hasher input = %q", hasher.password)
	}
	if strings.Contains(created.PasswordHash, "correct-password") == false {
		// The fixture hash is intentionally observable in this unit test; the
		// HTTP response contract still omits PasswordHash.
		t.Fatal("fixture hash was not returned by the recording hasher")
	}
	if _, err := service.CreateUser(ctx, UserCreateInput{Username: "ALICE", Password: "another-password"}); !errors.Is(err, ErrUserConflict) {
		t.Fatalf("duplicate username error = %v, want ErrUserConflict", err)
	}
}

func TestServiceGetAndUpdateUserEnforceOrganizationAndPreservePassword(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(store)
	hasher := &recordingPasswordHasher{}
	service.SetPasswordHasher(hasher)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	created, err := service.CreateUser(ctx, UserCreateInput{Username: "alice", Password: "correct-password"})
	if err != nil {
		t.Fatal(err)
	}
	name := "Alice Updated"
	updated, err := service.UpdateUser(ctx, created.ID, UserUpdateInput{Nickname: &name})
	if err != nil {
		t.Fatalf("UpdateUser() error = %v", err)
	}
	if updated.Nickname != name || updated.PasswordHash != "hashed:correct-password" || updated.TenantID != "tenant-a" || updated.OrgID != "org-a" {
		t.Fatalf("updated user = %+v", updated)
	}
	if _, err := service.GetUser(tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-b"}), created.ID); !errors.Is(err, tenant.ErrOrganizationDenied) {
		t.Fatalf("cross-organization GetUser error = %v", err)
	}
	org := "org-b"
	if _, err := service.UpdateUser(ctx, created.ID, UserUpdateInput{OrgID: &org}); !errors.Is(err, tenant.ErrOrganizationDenied) {
		t.Fatalf("cross-organization UpdateUser error = %v", err)
	}
}
