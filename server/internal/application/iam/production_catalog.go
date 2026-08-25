package iam

import (
	"net/http"

	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
)

// ProductionMenuCatalog is the immutable compatibility catalog shared by the
// installer and the authenticated mixed-mode fallback. Persistent menu rows
// remain authoritative whenever a tenant has any configured menu data.
func ProductionMenuCatalog() []domain.Menu {
	return []domain.Menu{
		{ID: "menu-overview", Name: "概览", Path: "/dashboard", Type: domain.MenuTypeDirectory, Redirect: "/dashboard/analytics", Icon: "lucide:layout-dashboard", Sort: 0, Visible: true, Active: true},
		{ID: "menu-overview-runtime", ParentID: "menu-overview", Name: "运行概览", Path: "/dashboard/analytics", Type: domain.MenuTypeMenu, Component: "/dashboard/analytics/index.vue", Icon: "lucide:area-chart", Permission: "dashboard:overview:read", Sort: 0, Visible: true, Active: true},

		{ID: "menu-identity", Name: "身份与权限", Path: "/iam", Type: domain.MenuTypeDirectory, Redirect: "/iam/users", Icon: "lucide:shield-check", Sort: 20, Visible: true, Active: true},
		{ID: "menu-identity-users", ParentID: "menu-identity", Name: "用户管理", Path: "/iam/users", Type: domain.MenuTypeMenu, Component: "/iam/users/index.vue", Icon: "lucide:user-round-search", Permission: "iam:users:read", Sort: 0, Visible: true, Active: true},
		{ID: "menu-identity-roles", ParentID: "menu-identity", Name: "角色管理", Path: "/iam/roles", Type: domain.MenuTypeMenu, Component: "/iam/roles/index.vue", Icon: "lucide:shield-check", Permission: "iam:roles:read", Sort: 10, Visible: true, Active: true},
		{ID: "menu-identity-menus", ParentID: "menu-identity", Name: "菜单管理", Path: "/iam/menus", Type: domain.MenuTypeMenu, Component: "/iam/menus/index.vue", Icon: "lucide:menu", Permission: "iam:menus:read", Sort: 20, Visible: true, Active: true},
		{ID: "menu-identity-permissions", ParentID: "menu-identity", Name: "权限列表", Path: "/iam/permissions", Type: domain.MenuTypeMenu, Component: "/iam/permissions/index.vue", Icon: "lucide:key-round", Permission: "iam:permissions:read", Sort: 30, Visible: true, Active: true},

		{ID: "menu-system-config", Name: "系统配置", Path: "/configuration", Type: domain.MenuTypeDirectory, Redirect: "/system/settings", Icon: "lucide:settings", Sort: 30, Visible: true, Active: true},
		{ID: "menu-system-settings", ParentID: "menu-system-config", Name: "系统设置", Path: "/system/settings", Type: domain.MenuTypeMenu, Component: "/system/settings/index.vue", Icon: "lucide:sliders-horizontal", Permission: "system:settings:read", Sort: 0, Visible: true, Active: true},
		{ID: "menu-system-dictionary", ParentID: "menu-system-config", Name: "字典管理", Path: "/system/dictionary", Type: domain.MenuTypeMenu, Component: "/system/dictionary/index.vue", Icon: "lucide:book-open", Permission: "system:dictionary:read", Sort: 10, Visible: true, Active: true},
		{ID: "menu-system-mail", ParentID: "menu-system-config", Name: "邮件服务", Path: "/system/mail", Type: domain.MenuTypeMenu, Component: "/system/mail/index.vue", Icon: "lucide:mail", Permission: "system:mail:read", Sort: 20, Visible: true, Active: true},
		{ID: "menu-system-files", ParentID: "menu-system-config", Name: "文件中心", Path: "/system/files", Type: domain.MenuTypeMenu, Component: "/system/files/index.vue", Icon: "lucide:folder-open", Permission: "system:files:read", Sort: 30, Visible: true, Active: true},
		{ID: "menu-system-observability", ParentID: "menu-system-config", Name: "可观测设置", Path: "/system/observability", Type: domain.MenuTypeMenu, Component: "/system/observability/index.vue", Icon: "lucide:gauge", Permission: "system:observability:read", Sort: 40, Visible: true, Active: true},

		{ID: "menu-operations", Name: "运维中心", Path: "/operations", Type: domain.MenuTypeDirectory, Redirect: "/system/monitor", Icon: "lucide:activity", Sort: 40, Visible: true, Active: true},
		{ID: "menu-operations-monitor", ParentID: "menu-operations", Name: "资源监控", Path: "/system/monitor", Type: domain.MenuTypeMenu, Component: "/system/monitor/index.vue", Icon: "lucide:monitor-cog", Permission: "ops:monitor:read", Sort: 0, Visible: true, Active: true},
		{ID: "menu-operations-audit", ParentID: "menu-operations", Name: "审计日志", Path: "/system/audit", Type: domain.MenuTypeMenu, Component: "/system/audit/index.vue", Icon: "lucide:scroll-text", Permission: "ops:audit:read", Sort: 10, Visible: true, Active: true},
		{ID: "menu-operations-tasks", ParentID: "menu-operations", Name: "任务管理", Path: "/system/tasks", Type: domain.MenuTypeMenu, Component: "/system/tasks/index.vue", Icon: "lucide:workflow", Permission: "ops:tasks:read", Sort: 20, Visible: true, Active: true},
		{ID: "menu-operations-data-jobs", ParentID: "menu-operations", Name: "数据作业", Path: "/system/import-export", Type: domain.MenuTypeMenu, Component: "/system/import-export/index.vue", Icon: "lucide:file-spreadsheet", Permission: "ops:data-jobs:read", Sort: 30, Visible: true, Active: true},
	}
}

// ProductionPermissionCatalog supplies stable codes for legacy installations
// whose permission table predates the production information architecture.
// Persistent rows win by ID, including explicit disables; missing IDs are
// backfilled in memory so an unrelated legacy row cannot empty menu fallback.
func ProductionPermissionCatalog() []domain.Permission {
	return []domain.Permission{
		{ID: "dashboard:overview:read", Name: "查看运行概览", Method: http.MethodGet, Path: "/api/admin/v1/dashboard/summary", Active: true},
		{ID: "iam:users:read", Name: "查看用户", Method: http.MethodGet, Path: "/api/admin/v1/iam/users/*", Active: true},
		{ID: "iam:users:manage", Name: "管理用户", Method: "*", Path: "/api/admin/v1/iam/users/*", Active: true},
		{ID: "iam:roles:read", Name: "查看角色", Method: http.MethodGet, Path: "/api/admin/v1/iam/roles/*", Active: true},
		{ID: "iam:roles:manage", Name: "管理角色", Method: "*", Path: "/api/admin/v1/iam/roles/*", Active: true},
		{ID: "iam:menus:read", Name: "查看菜单", Method: http.MethodGet, Path: "/api/admin/v1/iam/menus/*", Active: true},
		{ID: "iam:menus:manage", Name: "管理菜单", Method: "*", Path: "/api/admin/v1/iam/menus/*", Active: true},
		{ID: "iam:components:read", Name: "查看菜单组件", Method: http.MethodGet, Path: "/api/admin/v1/iam/components", Active: true},
		{ID: "iam:permissions:read", Name: "查看权限", Method: http.MethodGet, Path: "/api/admin/v1/iam/permissions", Active: true},
		{ID: "iam:policies:read", Name: "查看权限策略", Method: http.MethodGet, Path: "/api/admin/v1/iam/policies", Active: true},
		{ID: "iam:policies:manage", Name: "管理权限策略", Method: http.MethodPost, Path: "/api/admin/v1/iam/policies", Active: true},
		{ID: "iam:data-scopes:read", Name: "查看数据范围", Method: http.MethodGet, Path: "/api/admin/v1/iam/data-scopes", Active: true},
		{ID: "iam:data-scopes:manage", Name: "管理数据范围", Method: http.MethodPost, Path: "/api/admin/v1/iam/data-scopes", Active: true},
		{ID: "system:settings:read", Name: "查看系统设置", Method: http.MethodGet, Path: "/api/admin/v1/settings/*", Active: true},
		{ID: "system:settings:manage", Name: "管理系统设置", Method: "*", Path: "/api/admin/v1/settings/*", Active: true},
		{ID: "system:dictionary:read", Name: "查看字典", Method: http.MethodGet, Path: "/api/admin/v1/dictionaries/*", Active: true},
		{ID: "system:dictionary:manage", Name: "管理字典", Method: "*", Path: "/api/admin/v1/dictionaries/*", Active: true},
		{ID: "system:mail:read", Name: "查看邮件服务", Method: http.MethodGet, Path: "/api/admin/v1/mail/*", Active: true},
		{ID: "system:mail:manage", Name: "管理邮件服务", Method: "*", Path: "/api/admin/v1/mail/*", Active: true},
		{ID: "system:files:read", Name: "查看文件", Method: http.MethodGet, Path: "/api/admin/v1/files/*", Active: true},
		{ID: "system:files:manage", Name: "管理文件", Method: "*", Path: "/api/admin/v1/files/*", Active: true},
		{ID: "system:observability:read", Name: "查看可观测设置", Method: http.MethodGet, Path: "/api/admin/v1/observability/settings/*", Active: true},
		{ID: "system:observability:manage", Name: "管理可观测设置", Method: http.MethodPut, Path: "/api/admin/v1/observability/settings/*", Active: true},
		{ID: "ops:monitor:read", Name: "查看资源监控", Method: http.MethodGet, Path: "/api/admin/v1/ops/monitor", Active: true},
		{ID: "ops:audit:read", Name: "查看审计日志", Method: http.MethodGet, Path: "/api/admin/v1/audit/*", Active: true},
		{ID: "ops:tasks:read", Name: "查看任务", Method: http.MethodGet, Path: "/api/admin/v1/tasks/*", Active: true},
		{ID: "ops:tasks:manage", Name: "管理任务", Method: "*", Path: "/api/admin/v1/tasks/*", Active: true},
		{ID: "ops:data-jobs:read", Name: "查看数据作业", Method: http.MethodGet, Path: "/api/admin/v1/import-export/*", Active: true},
		{ID: "ops:data-jobs:manage", Name: "管理数据作业", Method: "*", Path: "/api/admin/v1/import-export/*", Active: true},
	}
}
