package iam

import (
	"context"
	"errors"
	"testing"

	domain "example.com/gin-vben-admin/server/internal/domain/iam"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
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
