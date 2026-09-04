package iam

import (
	"net/http"
	"testing"

	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
)

func TestProductionPermissionCatalogSeparatesAuxiliaryReadAndManageBoundaries(t *testing.T) {
	catalog := ProductionPermissionCatalog()
	byID := make(map[string]struct {
		method string
		path   string
		active bool
	}, len(catalog))
	for _, permission := range catalog {
		byID[permission.ID] = struct {
			method string
			path   string
			active bool
		}{method: permission.Method, path: permission.Path, active: permission.Active}
	}

	for _, want := range []struct {
		readCode   string
		manageCode string
		path       string
	}{
		{readCode: "system:settings:read", manageCode: "system:settings:manage", path: "/api/admin/v1/settings/*"},
		{readCode: "system:dictionary:read", manageCode: "system:dictionary:manage", path: "/api/admin/v1/dictionaries/*"},
		{readCode: "system:mail:read", manageCode: "system:mail:manage", path: "/api/admin/v1/mail/*"},
		{readCode: "system:files:read", manageCode: "system:files:manage", path: "/api/admin/v1/files/*"},
		{readCode: "media:library:read", manageCode: "media:library:manage", path: "/api/admin/v1/media/*"},
		{readCode: "ops:tasks:read", manageCode: "ops:tasks:manage", path: "/api/admin/v1/tasks/*"},
		{readCode: "ops:data-jobs:read", manageCode: "ops:data-jobs:manage", path: "/api/admin/v1/import-export/*"},
		{readCode: "notification:callers:read", manageCode: "notification:callers:manage", path: "/api/admin/v1/notification/callers/*"},
		{readCode: "notification:accounts:read", manageCode: "notification:accounts:manage", path: "/api/admin/v1/notification/accounts/*"},
		{readCode: "notification:templates:read", manageCode: "notification:templates:manage", path: "/api/admin/v1/notification/templates/*"},
		{readCode: "notification:verification:read", manageCode: "notification:verification:manage", path: "/api/admin/v1/notification/verification/*"},
	} {
		read, readOK := byID[want.readCode]
		manage, manageOK := byID[want.manageCode]
		if !readOK || read.method != http.MethodGet || read.path != want.path {
			t.Fatalf("read permission %q = %+v exists=%v", want.readCode, read, readOK)
		}
		if !manageOK || manage.method != "*" || manage.path != want.path {
			t.Fatalf("manage permission %q = %+v exists=%v", want.manageCode, manage, manageOK)
		}
	}
	for _, want := range []domain.Permission{
		{ID: "system:mail:test", Method: http.MethodPost, Path: "/api/admin/v1/mail/accounts/:id/test"},
		{ID: "notification:templates:publish", Method: http.MethodPost, Path: "/api/admin/v1/notification/templates/:id/publish"},
		{ID: "notification:templates:test", Method: http.MethodPost, Path: "/api/admin/v1/notification/templates/:id/test"},
		{ID: "notification:verification-policies:read", Method: http.MethodGet, Path: "/api/admin/v1/notification/verification-policies/*"},
		{ID: "notification:verification-policies:manage", Method: http.MethodPatch, Path: "/api/admin/v1/notification/verification-policies/:policy_key"},
	} {
		got, ok := byID[want.ID]
		if !ok || got.method != want.Method || got.path != want.Path || !got.active {
			t.Fatalf("permission %q=%+v exists=%v", want.ID, got, ok)
		}
	}

	if audit := byID["ops:audit:read"]; audit.method != http.MethodGet || audit.path != "/api/admin/v1/audit/*" {
		t.Fatalf("audit read permission = %+v", audit)
	}
	if _, exists := byID["ops:audit:manage"]; exists {
		t.Fatal("read-only audit API must not publish a manage permission")
	}
	if observability := byID["system:observability:read"]; observability.method != http.MethodGet || observability.path != "/api/admin/v1/observability/settings/*" {
		t.Fatalf("observability read permission = %+v", observability)
	}
	if observability := byID["system:observability:manage"]; observability.method != http.MethodPut || observability.path != "/api/admin/v1/observability/settings/*" {
		t.Fatalf("observability manage permission = %+v", observability)
	}
}

func TestProductionPermissionCatalogIncludesIAMEditorDependencies(t *testing.T) {
	byID := make(map[string]domain.Permission)
	for _, permission := range ProductionPermissionCatalog() {
		byID[permission.ID] = permission
	}
	for _, want := range []domain.Permission{
		{ID: "iam:components:read", Method: http.MethodGet, Path: "/api/admin/v1/iam/components"},
		{ID: "iam:policies:read", Method: http.MethodGet, Path: "/api/admin/v1/iam/policies"},
		{ID: "iam:policies:manage", Method: http.MethodPost, Path: "/api/admin/v1/iam/policies"},
		{ID: "iam:data-scopes:read", Method: http.MethodGet, Path: "/api/admin/v1/iam/data-scopes"},
		{ID: "iam:data-scopes:manage", Method: http.MethodPost, Path: "/api/admin/v1/iam/data-scopes"},
	} {
		got, ok := byID[want.ID]
		if !ok || got.Method != want.Method || got.Path != want.Path || !got.Active {
			t.Fatalf("permission %q=%+v exists=%v", want.ID, got, ok)
		}
	}
}

func TestProductionMenuCatalogUsesDashboardAsDirectFirstLevelPage(t *testing.T) {
	menus := ProductionMenuCatalog()
	byID := make(map[string]domain.Menu, len(menus))
	for _, menu := range menus {
		byID[menu.ID] = menu
	}
	dashboard, ok := byID["menu-overview"]
	if !ok {
		t.Fatal("production catalog is missing the dashboard menu")
	}
	if dashboard.ParentID != "" || dashboard.Path != "/dashboard" || dashboard.Type != domain.MenuTypeMenu || dashboard.Component != "/dashboard/analytics/index.vue" || dashboard.Redirect != "" || dashboard.Permission != "dashboard:overview:read" {
		t.Fatalf("dashboard menu=%+v", dashboard)
	}
	if _, exists := byID["menu-overview-runtime"]; exists {
		t.Fatal("retired nested runtime menu remains in the production catalog")
	}
	if byID["menu-identity-menus"].Name != "菜单管理" || byID["menu-identity-permissions"].Name != "权限管理" {
		t.Fatalf("renamed IAM menus=%q/%q", byID["menu-identity-menus"].Name, byID["menu-identity-permissions"].Name)
	}
	for _, id := range []string{"menu-system-parameters", "menu-system-observability"} {
		menu, exists := byID[id]
		if !exists {
			t.Fatalf("retired menu %q is missing from the compatibility catalog", id)
		}
		if menu.Visible || menu.Active {
			t.Fatalf("retired menu %q remains visible/active: %+v", id, menu)
		}
	}
}

func TestCanonicalizeProductionMenusHidesRetiredSettingsEntries(t *testing.T) {
	menus := canonicalizeProductionMenus([]domain.Menu{
		{ID: "menu-system-parameters", Name: "参数管理", Path: "/system/parameters", Type: domain.MenuTypeMenu, Visible: true, Active: true},
		{ID: "menu-system-observability", Name: "可观测设置", Path: "/system/observability", Type: domain.MenuTypeMenu, Visible: true, Active: true},
		{ID: "tenant-custom", Name: "自定义", Path: "/custom", Type: domain.MenuTypeMenu, Visible: true, Active: true},
	})
	byID := make(map[string]domain.Menu, len(menus))
	for _, menu := range menus {
		byID[menu.ID] = menu
	}
	for _, id := range []string{"menu-system-parameters", "menu-system-observability"} {
		if menu := byID[id]; menu.Visible || menu.Active {
			t.Fatalf("canonicalized retired menu %q = %+v", id, menu)
		}
	}
	if menu := byID["tenant-custom"]; !menu.Visible || !menu.Active {
		t.Fatalf("tenant menu was changed while canonicalizing: %+v", menu)
	}
}
