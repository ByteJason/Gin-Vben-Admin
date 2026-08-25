package iam

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
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

func TestServiceListAccessCodesLoadsPoliciesOnceAndAppliesWildcardDenyAndActiveFilters(t *testing.T) {
	permissions := permissionRepositoryStub{permissions: []domain.Permission{
		{ID: "z.read", Method: "GET", Path: "/z", Active: true},
		{ID: "a.read", Method: "GET", Path: "/a", Active: true},
		{ID: "a.read", Method: "GET", Path: "/a", Active: true},
		{ID: "write.denied", Method: "POST", Path: "/write", Active: true},
		{ID: "disabled.read", Method: "GET", Path: "/disabled", Active: false},
		{ID: " ", Method: "GET", Path: "/blank", Active: true},
	}}
	policies := &countingPolicyStore{policies: []domain.Policy{
		{RoleID: "role-super-admin", Domain: "tenant-a", Method: "*", Path: "*", Effect: domain.EffectAllow},
		{RoleID: "role-super-admin", Domain: "tenant-a", Method: "POST", Path: "/write", Effect: domain.EffectDeny},
	}}
	service := NewServiceWithRepositories(nil, nil, nil, permissions, policies, nil)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a"})

	codes, err := service.ListAccessCodes(ctx, domain.Subject{UserID: "u1", RoleIDs: []string{"role-super-admin"}})
	if err != nil {
		t.Fatalf("ListAccessCodes() error = %v", err)
	}
	if !sort.StringsAreSorted(codes) || !containsString(codes, "a.read") || !containsString(codes, "z.read") || containsString(codes, "write.denied") || containsString(codes, "disabled.read") {
		t.Fatalf("codes = %v, want sorted custom allows without denied/disabled entries", codes)
	}
	if policies.listCalls != 1 {
		t.Fatalf("ListPolicies() calls = %d, want exactly one bounded policy read", policies.listCalls)
	}
}

func TestServiceListAccessCodesEmptyAndTenantIsolated(t *testing.T) {
	t.Run("empty persistent permissions use the production fallback", func(t *testing.T) {
		policies := &countingPolicyStore{}
		service := NewServiceWithRepositories(nil, nil, nil, permissionRepositoryStub{}, policies, nil)
		codes, err := service.ListAccessCodes(context.Background(), domain.Subject{UserID: "u1"})
		if err != nil || codes == nil || len(codes) != 0 {
			t.Fatalf("codes=%v err=%v, want non-nil empty result", codes, err)
		}
		if policies.listCalls != 1 {
			t.Fatalf("ListPolicies() calls = %d, want one bounded fallback evaluation", policies.listCalls)
		}
	})

	t.Run("tenant domain policy cannot cross boundaries", func(t *testing.T) {
		permissions := permissionRepositoryStub{permissions: []domain.Permission{{ID: "orders.read", Method: "GET", Path: "/orders", Active: true}}}
		policies := &countingPolicyStore{policies: []domain.Policy{{RoleID: "reader", Domain: "tenant-a", Method: "GET", Path: "/orders", Effect: domain.EffectAllow}}}
		service := NewServiceWithRepositories(nil, nil, nil, permissions, policies, nil)
		ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-b"})
		codes, err := service.ListAccessCodes(ctx, domain.Subject{UserID: "u1", RoleIDs: []string{"reader"}})
		if err != nil || codes == nil || len(codes) != 0 {
			t.Fatalf("cross-tenant codes=%v err=%v, want non-nil empty result", codes, err)
		}
	})
}

func TestServiceResolveSubjectFiltersDisabledMissingAndOutOfScopeRoles(t *testing.T) {
	store := NewMemoryStore()
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	for _, role := range []domain.Role{
		{ID: "active-global", TenantID: "tenant-a", Active: true},
		{ID: "active-org", TenantID: "tenant-a", OrgID: "org-a", Active: true},
		{ID: "disabled", TenantID: "tenant-a", OrgID: "org-a", Active: false},
		{ID: "other-org", TenantID: "tenant-a", OrgID: "org-b", Active: true},
		{ID: "other-tenant", TenantID: "tenant-b", Active: true},
	} {
		if err := store.SaveRole(ctx, role); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(store)
	user := domain.User{
		ID: "u1", TenantID: "tenant-a", OrgID: "org-a", Active: true,
		RoleIDs: []string{"other-tenant", "missing", "disabled", "active-org", "other-org", "active-global", "active-global"},
	}
	subject, err := service.ResolveSubject(ctx, user)
	if err != nil {
		t.Fatalf("ResolveSubject() error=%v", err)
	}
	if want := []string{"active-global", "active-org"}; !reflect.DeepEqual(subject.RoleIDs, want) {
		t.Fatalf("effective roles=%v want=%v", subject.RoleIDs, want)
	}
	if subject.UserID != "u1" || subject.Domain != "tenant-a" {
		t.Fatalf("subject=%+v", subject)
	}
}

func TestServiceResolveSubjectPreservesDirectUserPolicyAfterRoleDisable(t *testing.T) {
	store := NewMemoryStore()
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a"})
	if err := store.SaveRole(ctx, domain.Role{ID: "reader", TenantID: "tenant-a", Active: false}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePolicy(ctx, domain.Policy{RoleID: "reader", Domain: "tenant-a", Method: "GET", Path: "/role-only", Effect: domain.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePolicy(ctx, domain.Policy{Subject: "u1", Domain: "tenant-a", Method: "GET", Path: "/direct", Effect: domain.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	service := NewService(store)
	subject, err := service.ResolveSubject(ctx, domain.User{ID: "u1", TenantID: "tenant-a", Active: true, RoleIDs: []string{"reader"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(subject.RoleIDs) != 0 {
		t.Fatalf("disabled role leaked into subject: %v", subject.RoleIDs)
	}
	if allowed, err := service.Authorize(ctx, subject, domain.Request{Method: "GET", Path: "/direct"}); err != nil || !allowed {
		t.Fatalf("direct user grant allowed=%v err=%v", allowed, err)
	}
	if allowed, err := service.Authorize(ctx, subject, domain.Request{Method: "GET", Path: "/role-only"}); err == nil || allowed {
		t.Fatalf("disabled role grant allowed=%v err=%v", allowed, err)
	}
}

func TestServiceListAccessCodesDoesNotLeakOrganizationScopedCatalogOrPolicy(t *testing.T) {
	store := NewMemoryStore()
	tenantWide := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a"})
	orgA := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	orgB := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-b"})
	for _, permission := range []domain.Permission{
		{ID: "org-a-code", Name: "Org A", Method: "GET", Path: "/org-a", Active: true, TenantID: "tenant-a", OrgID: "org-a"},
		{ID: "org-a-policy-code", Name: "Org A policy", Method: "GET", Path: "/org-a-policy", Active: true, TenantID: "tenant-a"},
		{ID: "shared-code", Name: "Shared", Method: "GET", Path: "/shared", Active: true, TenantID: "tenant-a"},
	} {
		writeContext := tenantWide
		if permission.OrgID != "" {
			writeContext = orgA
		}
		if err := store.SavePermission(writeContext, permission); err != nil {
			t.Fatal(err)
		}
	}
	for _, policy := range []domain.Policy{
		{RoleID: "reader", Domain: "tenant-a", OrgID: "org-a", Method: "GET", Path: "/org-a-policy", Effect: domain.EffectAllow},
		{RoleID: "reader", Domain: "tenant-a", Method: "*", Path: "*", Effect: domain.EffectAllow},
	} {
		writeContext := tenantWide
		if policy.OrgID != "" {
			writeContext = orgA
		}
		if err := store.SavePolicy(writeContext, policy); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(store)
	codes, err := service.ListAccessCodes(orgB, domain.Subject{UserID: "u1", RoleIDs: []string{"reader"}, Domain: "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"org-a-policy-code", "shared-code"}
	for _, permission := range ProductionPermissionCatalog() {
		if permission.Active {
			want = append(want, permission.ID)
		}
	}
	sort.Strings(want)
	if !reflect.DeepEqual(codes, want) {
		// The global wildcard legitimately grants global catalog entries, but
		// the organization-private catalog entry must remain invisible.
		t.Fatalf("org-b codes=%v want=%v", codes, want)
	}

	// Remove the global wildcard so the org-A-only policy is the sole grant.
	store.policies = store.policies[:1]
	codes, err = service.ListAccessCodes(orgB, domain.Subject{UserID: "u1", RoleIDs: []string{"reader"}, Domain: "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 0 {
		t.Fatalf("org-A policy leaked to org-B: %v", codes)
	}
}

func TestLegacyEmptyCatalogFallsBackToProductionPermissionsAndMenus(t *testing.T) {
	store := NewMemoryStore()
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "default"})
	if err := store.SavePolicy(ctx, domain.Policy{RoleID: "role-super-admin", Domain: "default", Method: "*", Path: "*", Effect: domain.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	service := NewService(store)
	subject := domain.Subject{UserID: "1", RoleIDs: []string{"role-super-admin"}, Domain: "default"}
	codes, err := service.ListAccessCodes(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	wantCodes := make([]string, 0, len(ProductionPermissionCatalog()))
	for _, permission := range ProductionPermissionCatalog() {
		if permission.Active {
			wantCodes = append(wantCodes, permission.ID)
		}
	}
	sort.Strings(wantCodes)
	if !reflect.DeepEqual(codes, wantCodes) {
		t.Fatalf("legacy access codes=%v want=%v", codes, wantCodes)
	}
	routes, err := service.ListMenuRoutesForSubject(ctx, subject)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 4 {
		t.Fatalf("legacy production route roots=%d routes=%+v", len(routes), routes)
	}
}

func TestLegacyPartialPermissionCatalogStillUnlocksProductionMenuFallback(t *testing.T) {
	store := NewMemoryStore()
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "default"})
	if err := store.SavePermission(ctx, domain.Permission{
		ID: "legacy:reports:read", Name: "Legacy report", Method: http.MethodGet,
		Path: "/api/admin/v1/legacy/reports", Active: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePolicy(ctx, domain.Policy{RoleID: "role-super-admin", Domain: "default", Method: "*", Path: "*", Effect: domain.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	service := NewService(store)
	routes, err := service.ListMenuRoutesForSubject(ctx, domain.Subject{
		UserID: "1", RoleIDs: []string{"role-super-admin"}, Domain: "default",
	})
	if err != nil {
		t.Fatal(err)
	}
	leafCount := 0
	for _, root := range routes {
		leafCount += len(root.Children)
	}
	if len(routes) != 4 || leafCount != 14 {
		t.Fatalf("legacy partial catalog routes=%d leaves=%d routes=%+v", len(routes), leafCount, routes)
	}
}

func TestPersistentDisabledPermissionOverridesProductionBackfillByID(t *testing.T) {
	store := NewMemoryStore()
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "default"})
	if err := store.SavePermission(ctx, domain.Permission{
		ID: "dashboard:overview:read", Name: "Disabled dashboard", Method: http.MethodGet,
		Path: "/api/admin/v1/dashboard/summary", Active: false,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePolicy(ctx, domain.Policy{RoleID: "role-super-admin", Domain: "default", Method: "*", Path: "*", Effect: domain.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	codes, err := NewService(store).ListAccessCodes(ctx, domain.Subject{UserID: "1", RoleIDs: []string{"role-super-admin"}, Domain: "default"})
	if err != nil {
		t.Fatal(err)
	}
	if containsString(codes, "dashboard:overview:read") {
		t.Fatalf("explicitly disabled production permission was backfilled: %v", codes)
	}
}

func TestListAccessCodesDoesNotTreatParameterizedPolicyAsBroaderWildcardPermission(t *testing.T) {
	permissions := permissionRepositoryStub{permissions: ProductionPermissionCatalog()}
	policies := &countingPolicyStore{policies: []domain.Policy{{
		RoleID: "observer", Domain: "tenant-a", Method: http.MethodGet,
		Path: "/api/admin/v1/observability/settings/*", Effect: domain.EffectAllow,
	}}}
	service := NewServiceWithRepositories(nil, nil, nil, permissions, policies, nil)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a"})
	codes, err := service.ListAccessCodes(ctx, domain.Subject{UserID: "u1", RoleIDs: []string{"observer"}, Domain: "tenant-a"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"system:observability:read"}; !reflect.DeepEqual(codes, want) {
		t.Fatalf("parameterized policy codes=%v want=%v", codes, want)
	}
}

type permissionRepositoryStub struct {
	permissions []domain.Permission
	err         error
}

func (s permissionRepositoryStub) ListPermissions(context.Context) ([]domain.Permission, error) {
	if s.err != nil {
		return nil, s.err
	}
	return append([]domain.Permission(nil), s.permissions...), nil
}

type countingPolicyStore struct {
	policies  []domain.Policy
	err       error
	listCalls int
}

func (s *countingPolicyStore) ListPolicies(context.Context) ([]domain.Policy, error) {
	s.listCalls++
	if s.err != nil {
		return nil, s.err
	}
	return append([]domain.Policy(nil), s.policies...), nil
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

func TestServiceReplaceRolePermissionsAtomicallyReplacesRolePolicies(t *testing.T) {
	store := NewMemoryStore()
	if err := store.SaveRole(context.Background(), domain.Role{ID: "role-editor", Name: "Editor", TenantID: "tenant-a", OrgID: "org-a", Active: true}); err != nil {
		t.Fatal(err)
	}
	permissions := []domain.Permission{
		{ID: "users.read", Name: "Read users", Method: "GET", Path: "/users"},
		{ID: "users.write", Name: "Write users", Method: "POST", Path: "/users"},
		{ID: "roles.read", Name: "Read roles", Method: "GET", Path: "/roles"},
	}
	for _, permission := range permissions {
		if err := store.SavePermission(context.Background(), permission); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SavePolicy(context.Background(), domain.Policy{Subject: "u-direct", Method: "GET", Path: "/direct", Effect: domain.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePolicy(context.Background(), domain.Policy{RoleID: "role-editor", PermissionID: "old", Domain: "tenant-a", Method: "DELETE", Path: "/old", Effect: domain.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	service := NewService(store)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	updated, err := service.ReplaceRolePermissions(ctx, "role-editor", RolePermissionsInput{PermissionIDs: []string{"roles.read", "users.read"}})
	if err != nil {
		t.Fatalf("ReplaceRolePermissions() error = %v", err)
	}
	if got, want := updated.PermissionIDs, []string{"roles.read", "users.read"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("permission IDs = %v, want %v", got, want)
	}
	policies, err := store.ListPolicies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rolePolicies := make(map[string]domain.Policy)
	for _, policy := range policies {
		if policy.RoleID == "role-editor" {
			rolePolicies[policy.PermissionID] = policy
		}
	}
	if len(rolePolicies) != 2 || rolePolicies["roles.read"].Path != "/roles" || rolePolicies["users.read"].Path != "/users" {
		t.Fatalf("role policies = %+v", rolePolicies)
	}
	if _, ok := rolePolicies["old"]; ok {
		t.Fatal("old role policy was not removed")
	}
	if len(policies) != 3 {
		t.Fatalf("direct policy was not preserved: %+v", policies)
	}

	if _, err := service.ReplaceRolePermissions(ctx, "role-editor", RolePermissionsInput{PermissionIDs: []string{"users.read", "users.read"}}); !errors.Is(err, ErrInvalidRolePermissionAssignment) {
		t.Fatalf("duplicate permission error = %v", err)
	}
	if _, err := service.ReplaceRolePermissions(ctx, "role-editor", RolePermissionsInput{PermissionIDs: []string{"missing"}}); !errors.Is(err, domain.ErrResourceNotFound) {
		t.Fatalf("missing permission error = %v", err)
	}
	if _, err := service.ReplaceRolePermissions(tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-b"}), "role-editor", RolePermissionsInput{}); !errors.Is(err, tenant.ErrCrossTenant) {
		t.Fatalf("cross-tenant permission error = %v", err)
	}
}

func TestServiceListRolesKeepsGlobalAndCurrentOrganizationOnly(t *testing.T) {
	store := NewMemoryStore()
	for _, role := range []domain.Role{
		{ID: "role-global", Name: "Global", TenantID: "tenant-a", Active: true},
		{ID: "role-a", Name: "Org A", TenantID: "tenant-a", OrgID: "org-a", Active: true},
		{ID: "role-b", Name: "Org B", TenantID: "tenant-a", OrgID: "org-b", Active: true},
		{ID: "role-other-tenant", Name: "Other tenant", TenantID: "tenant-b", OrgID: "org-a", Active: true},
	} {
		if err := store.SaveRole(context.Background(), role); err != nil {
			t.Fatal(err)
		}
	}
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	roles, err := NewService(store).ListRoles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(roles) != 2 || roles[0].ID != "role-a" || roles[1].ID != "role-global" {
		t.Fatalf("roles=%#v", roles)
	}
}

func TestPlatformAdminMemoryAdapterStaysWithinSelectedTenant(t *testing.T) {
	store := NewMemoryStore()
	for _, user := range []domain.User{
		{ID: "user-a", Username: "alice", TenantID: "tenant-a", OrgID: "org-a", Active: true},
		{ID: "user-b", Username: "bob", TenantID: "tenant-b", OrgID: "org-b", Active: true},
	} {
		if err := store.SaveUser(context.Background(), user); err != nil {
			t.Fatal(err)
		}
	}
	for _, role := range []domain.Role{
		{ID: "role-a", Name: "Tenant A", TenantID: "tenant-a", OrgID: "org-a", Active: true},
		{ID: "role-b", Name: "Tenant B", TenantID: "tenant-b", OrgID: "org-b", Active: true},
	} {
		if err := store.SaveRole(context.Background(), role); err != nil {
			t.Fatal(err)
		}
	}
	for _, menu := range []domain.Menu{
		{ID: "menu-a", Name: "Tenant A", Path: "/a", TenantID: "tenant-a", OrgID: "org-a", Active: true, Visible: true},
		{ID: "menu-b", Name: "Tenant B", Path: "/b", TenantID: "tenant-b", OrgID: "org-b", Active: true, Visible: true},
	} {
		if err := store.SaveMenu(context.Background(), menu); err != nil {
			t.Fatal(err)
		}
	}

	ctx := tenant.WithContext(context.Background(), tenant.Context{
		TenantID: "tenant-a", Organization: "org-a", PlatformAdmin: true,
	})
	service := NewService(store)
	page, err := service.ListUsersPage(ctx, domain.UserListQuery{})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != "user-a" {
		t.Fatalf("platform user page=%+v err=%v, want selected tenant only", page, err)
	}
	roles, err := service.ListRoles(ctx)
	if err != nil || len(roles) != 1 || roles[0].ID != "role-a" {
		t.Fatalf("platform roles=%+v err=%v, want selected tenant only", roles, err)
	}
	menus, err := service.ListMenus(ctx)
	if err != nil || len(menus) != 1 || menus[0].ID != "menu-a" {
		t.Fatalf("platform menus=%+v err=%v, want selected tenant only", menus, err)
	}
	if _, err := service.GetAuthorizationUser(ctx, "user-b"); !errors.Is(err, tenant.ErrCrossTenant) {
		t.Fatalf("cross-tenant authorization user error=%v, want cross tenant", err)
	}
}

func TestServiceReplaceRolePermissionsRejectsOversizedPayload(t *testing.T) {
	store := NewMemoryStore()
	if err := store.SaveRole(context.Background(), domain.Role{ID: "role-editor", TenantID: "tenant-a", Active: true}); err != nil {
		t.Fatal(err)
	}
	service := NewService(store)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a"})
	if _, err := service.ReplaceRolePermissions(ctx, "role-editor", RolePermissionsInput{PermissionIDs: make([]string, MaxRolePermissionBindings+1)}); !errors.Is(err, ErrInvalidRolePermissionAssignment) {
		t.Fatalf("oversized permission error = %v", err)
	}
}

func TestServiceReplaceRoleDataScopesAtomicallyReplacesRoleRows(t *testing.T) {
	store := NewMemoryStore()
	roles := []domain.Role{
		{ID: "role-editor", Name: "Editor", TenantID: "tenant-a", OrgID: "org-a", Active: true},
		{ID: "role-other", Name: "Other", TenantID: "tenant-a", OrgID: "org-b", Active: true},
	}
	for _, role := range roles {
		if err := store.SaveRole(context.Background(), role); err != nil {
			t.Fatal(err)
		}
	}
	for _, scope := range []domain.DataScope{
		{RoleID: "role-editor", Domain: "tenant-a", OrgID: "org-a", Resource: "old", Scope: domain.ScopeOwn, IDs: []string{"old-1"}},
		{RoleID: "role-other", Domain: "tenant-a", OrgID: "org-b", Resource: "other", Scope: domain.ScopeOrg, IDs: []string{"org-b"}},
		{Subject: "user-direct", Domain: "tenant-a", OrgID: "org-a", Resource: "direct", Scope: domain.ScopeOwn, IDs: []string{"user-1"}},
	} {
		if err := store.SaveDataScope(context.Background(), scope); err != nil {
			t.Fatal(err)
		}
	}
	service := NewService(store)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	updated, err := service.ReplaceRoleDataScopes(ctx, "role-editor", RoleDataScopesInput{Scopes: []RoleDataScopeBinding{
		{Resource: "orders", Scope: domain.ScopeCustom, IDs: []string{"order-1", " order-2 "}},
		{Resource: "teams", Scope: domain.ScopeOrg, IDs: []string{"org-a"}},
	}})
	if err != nil {
		t.Fatalf("ReplaceRoleDataScopes() error = %v", err)
	}
	if updated.DataScope != domain.ScopeCustom {
		t.Fatalf("role data scope summary = %q", updated.DataScope)
	}
	scopes, err := store.ListDataScopes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	roleResources := map[string]domain.DataScope{}
	for _, scope := range scopes {
		if scope.RoleID == "role-editor" {
			roleResources[scope.Resource] = scope
		}
	}
	if len(roleResources) != 2 || roleResources["orders"].IDs[1] != "order-2" || roleResources["old"].Resource != "" {
		t.Fatalf("role scope replacement = %+v", roleResources)
	}
	if len(roleResources["teams"].IDs) != 1 || roleResources["teams"].OrgID != "org-a" {
		t.Fatalf("org scope relation = %+v", roleResources["teams"])
	}
	var preservedDirect, preservedOther bool
	for _, scope := range scopes {
		preservedDirect = preservedDirect || scope.Subject == "user-direct"
		preservedOther = preservedOther || scope.RoleID == "role-other"
	}
	if !preservedDirect || !preservedOther {
		t.Fatalf("unrelated data scopes were changed: %+v", scopes)
	}
	if _, err := service.ReplaceRoleDataScopes(ctx, "role-editor", RoleDataScopesInput{Scopes: []RoleDataScopeBinding{{Resource: "orders", Scope: domain.ScopeCustom, IDs: []string{"order-1", "order-1"}}}}); !errors.Is(err, ErrInvalidRoleDataScopeAssignment) {
		t.Fatalf("duplicate ID error = %v", err)
	}
	if _, err := service.ReplaceRoleDataScopes(ctx, "role-editor", RoleDataScopesInput{Scopes: []RoleDataScopeBinding{{Resource: "orders", Scope: domain.ScopeOwn}, {Resource: "orders", Scope: domain.ScopeOrg}}}); !errors.Is(err, ErrInvalidRoleDataScopeAssignment) {
		t.Fatalf("duplicate resource error = %v", err)
	}
	if _, err := service.ReplaceRoleDataScopes(tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-b"}), "role-editor", RoleDataScopesInput{}); !errors.Is(err, tenant.ErrCrossTenant) {
		t.Fatalf("cross-tenant error = %v", err)
	}
	cleared, err := service.ReplaceRoleDataScopes(ctx, "role-editor", RoleDataScopesInput{})
	if err != nil || cleared.DataScope != domain.ScopeOwn {
		t.Fatalf("clear data scopes = %+v err=%v", cleared, err)
	}
	scopes, _ = store.ListDataScopes(context.Background())
	for _, scope := range scopes {
		if scope.RoleID == "role-editor" && scope.OrgID == "org-a" {
			t.Fatalf("role data scope was not cleared: %+v", scope)
		}
	}
}

func TestServiceReplaceRoleDataScopesRejectsOversizedPayload(t *testing.T) {
	store := NewMemoryStore()
	if err := store.SaveRole(context.Background(), domain.Role{ID: "role-editor", TenantID: "tenant-a", Active: true}); err != nil {
		t.Fatal(err)
	}
	bindings := make([]RoleDataScopeBinding, MaxRoleDataScopeBindings+1)
	for index := range bindings {
		bindings[index] = RoleDataScopeBinding{Resource: "resource-" + strconv.Itoa(index), Scope: domain.ScopeOwn}
	}
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a"})
	if _, err := NewService(store).ReplaceRoleDataScopes(ctx, "role-editor", RoleDataScopesInput{Scopes: bindings}); !errors.Is(err, ErrInvalidRoleDataScopeAssignment) {
		t.Fatalf("oversized data scope error = %v", err)
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
