package iam

import (
	"context"
	"errors"
	"testing"

	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

func menuTenantContext() context.Context {
	return tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
}

func boolPtr(value bool) *bool { return &value }

func TestMenuWriterValidatesRegistryAndProjectsRoutes(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(store)
	ctx := menuTenantContext()
	root, err := service.CreateMenu(ctx, MenuCreateInput{
		ID: "dashboard", Name: "Dashboard", Path: "/dashboard", Type: domain.MenuTypeDirectory,
		Visible: boolPtr(true), Active: boolPtr(true), Sort: 20,
	})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	if root.TenantID != "tenant-a" || root.OrgID != "org-a" {
		t.Fatalf("scope not applied: %+v", root)
	}
	if _, err := service.CreateMenu(ctx, MenuCreateInput{
		ID: "analytics", ParentID: root.ID, Name: "Analytics", Path: "/dashboard/analytics",
		Type: domain.MenuTypeMenu, Component: "/dashboard/analytics/index.vue", Sort: 1,
	}); err != nil {
		t.Fatalf("create page: %v", err)
	}
	if _, err := service.CreateMenu(ctx, MenuCreateInput{
		ID: "dashboard.refresh", ParentID: root.ID, Name: "Refresh", Path: "/dashboard/refresh",
		Type: domain.MenuTypeButton, Permission: "dashboard:refresh", Sort: 2,
	}); err != nil {
		t.Fatalf("create button: %v", err)
	}
	if _, err := service.CreateMenu(ctx, MenuCreateInput{
		ID: "bad", Name: "Bad", Path: "/bad", Type: domain.MenuTypeMenu, Component: "/tmp/arbitrary.vue",
	}); !errors.Is(err, ErrComponentNotRegistered) {
		t.Fatalf("unregistered component error=%v", err)
	}
	routes, err := service.ListMenuRoutes(ctx)
	if err != nil {
		t.Fatalf("routes: %v", err)
	}
	if len(routes) != 1 || routes[0].Name != "dashboard" || len(routes[0].Children) != 1 || routes[0].Children[0].Component != "/dashboard/analytics/index.vue" {
		t.Fatalf("routes=%+v", routes)
	}
	if routes[0].Meta.Title != "Dashboard" || routes[0].Children[0].Meta.Title != "Analytics" {
		t.Fatalf("route metadata=%+v", routes)
	}
}

func TestMenuWriterRejectsCyclesAndProtectsChildrenOnDelete(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(store)
	ctx := menuTenantContext()
	for _, input := range []MenuCreateInput{
		{ID: "root", Name: "Root", Path: "/root", Type: domain.MenuTypeDirectory},
		{ID: "child", ParentID: "root", Name: "Child", Path: "/root/child", Type: domain.MenuTypeDirectory},
	} {
		if _, err := service.CreateMenu(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.DeleteMenu(ctx, "root"); !errors.Is(err, ErrMenuHasChildren) {
		t.Fatalf("delete parent error=%v", err)
	}
	if err := service.ReorderMenus(ctx, MenuReorderInput{Items: []domain.MenuOrder{{ID: "root", ParentID: "child", Sort: 2}, {ID: "child", ParentID: "root", Sort: 1}}}); !errors.Is(err, ErrInvalidMenu) {
		t.Fatalf("cycle reorder error=%v", err)
	}
	if err := service.ReorderMenus(ctx, MenuReorderInput{Items: []domain.MenuOrder{{ID: "child", ParentID: "", Sort: 1}}}); err != nil {
		t.Fatalf("valid reorder error=%v", err)
	}
	if err := service.DeleteMenu(ctx, "child"); err != nil {
		t.Fatalf("delete child error=%v", err)
	}
}

func TestBuildMenuRoutesPreservesExistingAbsoluteChildURLAcrossProductGroups(t *testing.T) {
	routes, err := BuildMenuRoutes([]domain.Menu{
		{ID: "menu-system-config", Name: "系统配置", Path: "/configuration", Type: domain.MenuTypeDirectory, Visible: true, Active: true},
		{ID: "menu-system-settings", ParentID: "menu-system-config", Name: "系统设置", Path: "/system/settings", Type: domain.MenuTypeMenu, Component: "/system/settings/index.vue", Visible: true, Active: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || len(routes[0].Children) != 1 || routes[0].Children[0].Path != "/system/settings" {
		t.Fatalf("routes=%+v", routes)
	}
}

func TestFilterMenusByAccessCodesKeepsContainersAndPublicNodesWithoutLeakingHiddenBranches(t *testing.T) {
	menus := []domain.Menu{
		{ID: "restricted-group", Name: "Restricted", Path: "/restricted", Type: domain.MenuTypeDirectory, Permission: "group.read", Visible: true, Active: true},
		{ID: "allowed-child", ParentID: "restricted-group", Name: "Allowed", Path: "/restricted/allowed", Type: domain.MenuTypeMenu, Component: "/iam/users/index.vue", Permission: "users.read", Visible: true, Active: true},
		{ID: "denied-child", ParentID: "restricted-group", Name: "Denied", Path: "/restricted/denied", Type: domain.MenuTypeMenu, Component: "/iam/roles/index.vue", Permission: "roles.read", Visible: true, Active: true},
		{ID: "public", Name: "Public", Path: "/public", Type: domain.MenuTypeMenu, Component: "/iam/permissions/index.vue", Visible: true, Active: true},
		{ID: "empty-group", Name: "Empty", Path: "/empty", Type: domain.MenuTypeDirectory, Visible: true, Active: true},
		{ID: "hidden-group", Name: "Hidden", Path: "/hidden", Type: domain.MenuTypeDirectory, Visible: false, Active: true},
		{ID: "hidden-public-child", ParentID: "hidden-group", Name: "Hidden child", Path: "/hidden/child", Type: domain.MenuTypeMenu, Component: "/iam/menus/index.vue", Visible: true, Active: true},
	}

	routes, err := BuildMenuRoutes(filterMenusByAccessCodes(menus, []string{"users.read"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 || routes[0].Name != "public" || routes[1].Name != "restricted-group" || len(routes[1].Children) != 1 || routes[1].Children[0].Name != "allowed-child" {
		t.Fatalf("filtered routes=%+v", routes)
	}
	withoutCodes, err := BuildMenuRoutes(filterMenusByAccessCodes(menus, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(withoutCodes) != 1 || withoutCodes[0].Name != "public" {
		t.Fatalf("zero-code routes should retain only public leaves: %+v", withoutCodes)
	}
}
