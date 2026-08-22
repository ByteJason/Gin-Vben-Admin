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

type resetRecordingHasher struct {
	password string
}

func (h *resetRecordingHasher) Hash(password string) (string, error) {
	h.password = password
	return "encoded-reset-password", nil
}

func TestServiceResetUserPasswordHashesOnlyCredentialAndPreservesUserState(t *testing.T) {
	store := NewMemoryStore()
	oldChangedAt := time.Unix(100, 0).UTC()
	original := domain.User{
		ID: "target", Username: "alice", DisplayName: "Alice", Nickname: "A", Avatar: "avatar",
		Email: "alice@example.test", Phone: "+8613800138000", PasswordHash: "old-hash",
		LastLoginIP: "192.0.2.10", LastLoginAt: time.Unix(90, 0).UTC(), PasswordChangedAt: oldChangedAt,
		TenantID: "tenant-a", OrgID: "org-a", Active: false, RoleIDs: []string{"r-reader", "r-auditor"},
	}
	if err := store.SaveUser(context.Background(), original); err != nil {
		t.Fatal(err)
	}
	hasher := &resetRecordingHasher{}
	service := NewService(store)
	service.SetPasswordHasher(hasher)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"})

	updated, err := service.ResetUserPassword(ctx, original.ID, UserPasswordResetInput{Password: "new-password"})
	if err != nil {
		t.Fatalf("ResetUserPassword() error = %v", err)
	}
	if hasher.password != "new-password" {
		t.Fatalf("hasher input = %q", hasher.password)
	}
	if updated.PasswordHash != "encoded-reset-password" || !updated.PasswordChangedAt.After(oldChangedAt) {
		t.Fatalf("updated credential = %+v", updated)
	}
	if updated.Username != original.Username || updated.DisplayName != original.DisplayName || updated.Nickname != original.Nickname || updated.Avatar != original.Avatar || updated.Email != original.Email || updated.Phone != original.Phone || updated.LastLoginIP != original.LastLoginIP || !updated.LastLoginAt.Equal(original.LastLoginAt) || updated.TenantID != original.TenantID || updated.OrgID != original.OrgID || updated.Active != original.Active || strings.Join(updated.RoleIDs, ",") != strings.Join(original.RoleIDs, ",") {
		t.Fatalf("reset changed non-credential state: before=%+v after=%+v", original, updated)
	}
	stored, err := store.FindUser(context.Background(), original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.PasswordHash != "encoded-reset-password" || !stored.PasswordChangedAt.Equal(updated.PasswordChangedAt) || len(stored.RoleIDs) != len(original.RoleIDs) || stored.Active != original.Active {
		t.Fatalf("stored reset user = %+v", stored)
	}

	if _, err := service.ResetUserPassword(ctx, original.ID, UserPasswordResetInput{Password: "short"}); !errors.Is(err, ErrInvalidUser) {
		t.Fatalf("short password error = %v, want ErrInvalidUser", err)
	}
	unchanged, err := store.FindUser(context.Background(), original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.PasswordHash != "encoded-reset-password" {
		t.Fatalf("invalid reset mutated hash = %q", unchanged.PasswordHash)
	}
}

func TestServiceResetUserPasswordEnforcesTenantOrganizationAndHasherBoundary(t *testing.T) {
	store := NewMemoryStore()
	original := domain.User{ID: "target", Username: "alice", PasswordHash: "old-hash", TenantID: "tenant-a", OrgID: "org-a", Active: true}
	if err := store.SaveUser(context.Background(), original); err != nil {
		t.Fatal(err)
	}
	service := NewService(store)
	service.SetPasswordHasher(&resetRecordingHasher{})
	if _, err := service.ResetUserPassword(tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-b"}), original.ID, UserPasswordResetInput{Password: "new-password"}); !errors.Is(err, tenant.ErrCrossTenant) {
		t.Fatalf("cross-tenant reset error = %v", err)
	}
	if _, err := service.ResetUserPassword(tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-b"}), original.ID, UserPasswordResetInput{Password: "new-password"}); !errors.Is(err, tenant.ErrOrganizationDenied) {
		t.Fatalf("cross-organization reset error = %v", err)
	}
	withoutHasher := NewService(store)
	if _, err := withoutHasher.ResetUserPassword(tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"}), original.ID, UserPasswordResetInput{Password: "new-password"}); !errors.Is(err, ErrPasswordHasherMissing) {
		t.Fatalf("missing hasher error = %v", err)
	}
}

func TestServiceReplaceRoleUsersScopesAndPreservesOtherRelationships(t *testing.T) {
	store := NewMemoryStore()
	users := []domain.User{
		{ID: "u1", Username: "alice", TenantID: "tenant-a", OrgID: "org-a", Active: true, RoleIDs: []string{"role-editor", "role-other"}},
		{ID: "u2", Username: "bob", TenantID: "tenant-a", OrgID: "org-a", Active: true, RoleIDs: []string{"role-other"}},
		{ID: "u3", Username: "carol", TenantID: "tenant-a", OrgID: "org-b", Active: true, RoleIDs: []string{"role-editor"}},
		{ID: "u4", Username: "dave", TenantID: "tenant-b", OrgID: "org-a", Active: true, RoleIDs: []string{"role-editor"}},
	}
	for _, user := range users {
		if err := store.SaveUser(context.Background(), user); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveRole(context.Background(), domain.Role{ID: "role-editor", Name: "Editor", Active: true, UserIDs: []string{"u1", "u3", "u4"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRole(context.Background(), domain.Role{ID: "role-other", Name: "Other", Active: true}); err != nil {
		t.Fatal(err)
	}
	service := NewService(store)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	updated, err := service.ReplaceRoleUsers(ctx, "role-editor", RoleUsersInput{UserIDs: []string{"u2"}})
	if err != nil {
		t.Fatalf("ReplaceRoleUsers() error = %v", err)
	}
	if len(updated.UserIDs) != 1 || updated.UserIDs[0] != "u2" {
		t.Fatalf("updated role users = %+v", updated)
	}
	u1, _ := store.FindUser(context.Background(), "u1")
	u2, _ := store.FindUser(context.Background(), "u2")
	u3, _ := store.FindUser(context.Background(), "u3")
	u4, _ := store.FindUser(context.Background(), "u4")
	if containsString(u1.RoleIDs, "role-editor") || !containsString(u1.RoleIDs, "role-other") || !containsString(u2.RoleIDs, "role-editor") || !containsString(u3.RoleIDs, "role-editor") || !containsString(u4.RoleIDs, "role-editor") {
		t.Fatalf("role relationships after scoped replace: u1=%v u2=%v u3=%v u4=%v", u1.RoleIDs, u2.RoleIDs, u3.RoleIDs, u4.RoleIDs)
	}
	if _, err := service.ReplaceRoleUsers(ctx, "role-editor", RoleUsersInput{UserIDs: []string{"u3"}}); !errors.Is(err, tenant.ErrOrganizationDenied) {
		t.Fatalf("cross-organization assignment error = %v", err)
	}
	if _, err := service.ReplaceRoleUsers(ctx, "role-editor", RoleUsersInput{UserIDs: []string{"u2", "u2"}}); !errors.Is(err, ErrInvalidUser) {
		t.Fatalf("duplicate assignment error = %v", err)
	}
	if _, err := service.ReplaceRoleUsers(ctx, "role-editor", RoleUsersInput{UserIDs: make([]string, MaxRoleAssignmentUsers+1)}); !errors.Is(err, ErrInvalidRoleAssignment) {
		t.Fatalf("oversized assignment error = %v", err)
	}
	cleared, err := service.ReplaceRoleUsers(ctx, "role-editor", RoleUsersInput{UserIDs: []string{}})
	if err != nil || len(cleared.UserIDs) != 0 {
		t.Fatalf("empty assignment clear = %+v err=%v", cleared, err)
	}
	u2, _ = store.FindUser(context.Background(), "u2")
	if containsString(u2.RoleIDs, "role-editor") || !containsString(u2.RoleIDs, "role-other") {
		t.Fatalf("clear changed unrelated relationships: %v", u2.RoleIDs)
	}
}

func TestServiceDeleteUserSoftDeletesWithinScopeAndIsIdempotent(t *testing.T) {
	store := NewMemoryStore()
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	original := domain.User{
		ID: "u-delete", Username: "alice", DisplayName: "Alice", TenantID: "tenant-a", OrgID: "org-a",
		Active: true, PasswordHash: "bcrypt-hash", RoleIDs: []string{"role-reader"}, Email: "alice@example.test",
	}
	if err := store.SaveUser(ctx, original); err != nil {
		t.Fatal(err)
	}

	deleted, err := NewService(store).DeleteUser(ctx, original.ID)
	if err != nil {
		t.Fatalf("DeleteUser() error = %v", err)
	}
	if deleted.Active || deleted.PasswordHash != original.PasswordHash || len(deleted.RoleIDs) != 1 || deleted.RoleIDs[0] != "role-reader" {
		t.Fatalf("soft-deleted user = %+v", deleted)
	}
	stored, err := store.FindUser(ctx, original.ID)
	if err != nil || stored.Active || stored.PasswordHash != original.PasswordHash || stored.Username != original.Username {
		t.Fatalf("stored soft-deleted user = %+v err=%v", stored, err)
	}
	if _, err := NewService(store).DeleteUser(ctx, original.ID); err != nil {
		t.Fatalf("idempotent DeleteUser() error = %v", err)
	}
	if _, err := NewService(store).DeleteUser(ctx, "missing"); !errors.Is(err, domain.ErrResourceNotFound) {
		t.Fatalf("missing DeleteUser() error = %v", err)
	}
	if _, err := NewService(store).DeleteUser(tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-b"}), original.ID); !errors.Is(err, tenant.ErrOrganizationDenied) {
		t.Fatalf("cross-organization DeleteUser() error = %v", err)
	}
	if _, err := NewService(store).DeleteUser(tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-b"}), original.ID); !errors.Is(err, tenant.ErrCrossTenant) {
		t.Fatalf("cross-tenant DeleteUser() error = %v", err)
	}
}

func TestServiceBatchUpdateUserStatusReturnsScopedPerItemResults(t *testing.T) {
	store := NewMemoryStore()
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	users := []domain.User{
		{ID: "u-active", Username: "alice", TenantID: "tenant-a", OrgID: "org-a", Active: true, PasswordHash: "hash-a", RoleIDs: []string{"role-a"}},
		{ID: "u-disabled", Username: "bob", TenantID: "tenant-a", OrgID: "org-a", Active: false, PasswordHash: "hash-b", RoleIDs: []string{"role-b"}},
		{ID: "u-other-tenant", Username: "carol", TenantID: "tenant-b", OrgID: "org-a", Active: true, PasswordHash: "hash-c"},
	}
	for _, user := range users {
		if err := store.SaveUser(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	results, err := NewService(store).BatchUpdateUserStatus(ctx, UserBatchStatusInput{Items: []UserStatusChangeInput{
		{ID: "u-active", Active: false},
		{ID: "u-disabled", Active: true},
		{ID: "missing", Active: false},
		{ID: "u-other-tenant", Active: false},
		{ID: "u-active", Active: true},
	}})
	if err != nil {
		t.Fatalf("BatchUpdateUserStatus() error = %v", err)
	}
	if len(results) != 5 || results[0].Err != nil || results[0].User.Active || results[1].Err != nil || !results[1].User.Active {
		t.Fatalf("status results = %+v", results)
	}
	if !errors.Is(results[2].Err, domain.ErrResourceNotFound) || !errors.Is(results[3].Err, domain.ErrResourceNotFound) || !errors.Is(results[4].Err, ErrInvalidUser) {
		t.Fatalf("per-item errors = %+v", results)
	}
	stored, err := store.FindUser(ctx, "u-active")
	if err != nil || stored.Active || stored.PasswordHash != "hash-a" || len(stored.RoleIDs) != 1 {
		t.Fatalf("stored active target = %+v err=%v", stored, err)
	}
	if _, err := NewService(store).BatchUpdateUserStatus(ctx, UserBatchStatusInput{Items: make([]UserStatusChangeInput, 101)}); !errors.Is(err, ErrInvalidUserBatch) {
		t.Fatalf("oversized batch error = %v, want ErrInvalidUserBatch", err)
	}
}
