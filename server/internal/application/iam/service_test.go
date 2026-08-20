package iam

import (
	"context"
	"errors"
	"testing"

	domain "example.com/gin-vben-admin/server/internal/domain/iam"
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
