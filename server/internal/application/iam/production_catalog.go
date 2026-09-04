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
		// The first five roots and their requested children are the production
		// information architecture. Legacy IDs stay stable so existing grants and
		// audit references remain addressable during the rename/move migration.
		// The dashboard is a page route itself; it no longer owns a nested runtime
		// child. Existing installations migrate the former child out of navigation.
		{ID: "menu-overview", Name: "仪表盘", Path: "/dashboard", Type: domain.MenuTypeMenu, Component: "/dashboard/analytics/index.vue", Icon: "lucide:layout-dashboard", Permission: "dashboard:overview:read", Sort: 0, Visible: true, Active: true},

		{ID: "menu-operations", Name: "运维监控", Path: "/ops", Type: domain.MenuTypeDirectory, Redirect: "/ops/server-status", Icon: "lucide:activity", Sort: 10, Visible: true, Active: true},
		{ID: "menu-operations-monitor", ParentID: "menu-operations", Name: "服务器状态", Path: "/ops/server-status", Type: domain.MenuTypeMenu, Component: "/system/monitor/index.vue", Icon: "lucide:monitor-cog", Permission: "ops:server-status:read", Sort: 0, Visible: true, Active: true},
		{ID: "menu-operations-audit", ParentID: "menu-operations", Name: "操作历史", Path: "/ops/operation-history", Type: domain.MenuTypeMenu, Component: "/ops/operation-history/index.vue", Icon: "lucide:scroll-text", Permission: "ops:operation-history:read", Sort: 10, Visible: true, Active: true},
		{ID: "menu-operations-login-logs", ParentID: "menu-operations", Name: "登录日志", Path: "/ops/login-logs", Type: domain.MenuTypeMenu, Component: "/ops/login-logs/index.vue", Icon: "lucide:log-in", Permission: "ops:login-logs:read", Sort: 20, Visible: true, Active: true},
		{ID: "menu-operations-tasks", ParentID: "menu-operations", Name: "定时任务", Path: "/ops/tasks", Type: domain.MenuTypeMenu, Component: "/system/tasks/index.vue", Icon: "lucide:workflow", Permission: "ops:tasks:read", Sort: 30, Visible: true, Active: true},
		{ID: "menu-operations-data-jobs", ParentID: "menu-operations", Name: "数据作业", Path: "/ops/data-jobs", Type: domain.MenuTypeMenu, Component: "/system/import-export/index.vue", Icon: "lucide:file-spreadsheet", Permission: "ops:data-jobs:read", Sort: 40, Visible: true, Active: true},

		{ID: "menu-identity", Name: "后台权限", Path: "/iam", Type: domain.MenuTypeDirectory, Redirect: "/iam/roles", Icon: "lucide:shield-check", Sort: 20, Visible: true, Active: true},
		{ID: "menu-identity-roles", ParentID: "menu-identity", Name: "角色管理", Path: "/iam/roles", Type: domain.MenuTypeMenu, Component: "/iam/roles/index.vue", Icon: "lucide:shield-check", Permission: "iam:roles:read", Sort: 0, Visible: true, Active: true},
		{ID: "menu-identity-menus", ParentID: "menu-identity", Name: "菜单管理", Path: "/iam/menus", Type: domain.MenuTypeMenu, Component: "/iam/menus/index.vue", Icon: "lucide:menu", Permission: "iam:menus:read", Sort: 10, Visible: true, Active: true},
		{ID: "menu-identity-permissions", ParentID: "menu-identity", Name: "权限管理", Path: "/iam/permissions", Type: domain.MenuTypeMenu, Component: "/iam/permissions/index.vue", Icon: "lucide:key-round", Permission: "iam:permissions:read", Sort: 20, Visible: true, Active: true},
		{ID: "menu-identity-users", ParentID: "menu-identity", Name: "用户管理", Path: "/iam/users", Type: domain.MenuTypeMenu, Component: "/iam/users/index.vue", Icon: "lucide:user-round-search", Permission: "iam:users:read", Sort: 30, Visible: true, Active: true},

		{ID: "menu-system-config", Name: "系统管理", Path: "/system", Type: domain.MenuTypeDirectory, Redirect: "/system/settings", Icon: "lucide:settings", Sort: 30, Visible: true, Active: true},
		{ID: "menu-system-dictionary", ParentID: "menu-system-config", Name: "字典管理", Path: "/system/dictionary", Type: domain.MenuTypeMenu, Component: "/system/dictionary/index.vue", Icon: "lucide:book-open", Permission: "system:dictionary:read", Sort: 0, Visible: true, Active: true},
		// Retain the installer IDs as inactive records so existing grants and
		// audit references remain addressable without exposing the retired page.
		{ID: "menu-system-parameters", ParentID: "menu-system-config", Name: "参数管理", Path: "/system/parameters", Type: domain.MenuTypeMenu, Component: "/system/settings/index.vue", Icon: "lucide:sliders-horizontal", Permission: "system:parameters:read", Sort: 10, Visible: false, Active: false},
		{ID: "menu-system-settings", ParentID: "menu-system-config", Name: "系统配置", Path: "/system/settings", Type: domain.MenuTypeMenu, Component: "/system/settings/index.vue", Icon: "lucide:settings", Permission: "system:settings:read", Sort: 20, Visible: true, Active: true},
		{ID: "menu-system-mail", ParentID: "menu-system-config", Name: "邮件服务", Path: "/system/mail", Type: domain.MenuTypeMenu, Component: "/system/mail/index.vue", Icon: "lucide:mail", Permission: "system:mail:read", Sort: 30, Visible: true, Active: true},
		{ID: "menu-system-observability", ParentID: "menu-system-config", Name: "可观测设置", Path: "/system/observability", Type: domain.MenuTypeMenu, Component: "/system/observability/index.vue", Icon: "lucide:gauge", Permission: "system:observability:read", Sort: 40, Visible: false, Active: false},

		{ID: "menu-media", Name: "媒体管理", Path: "/media", Type: domain.MenuTypeDirectory, Redirect: "/media/library", Icon: "lucide:images", Sort: 40, Visible: true, Active: true},
		{ID: "menu-media-library", ParentID: "menu-media", Name: "媒体库", Path: "/media/library", Type: domain.MenuTypeMenu, Component: "/system/files/index.vue", Icon: "lucide:folder-open", Permission: "media:library:read", Sort: 0, Visible: true, Active: true},
	}
}

// ProductionPermissionCatalog supplies stable codes for legacy installations
// whose permission table predates the production information architecture.
// Persistent rows win by ID, including explicit disables; missing IDs are
// backfilled in memory so an unrelated legacy row cannot empty menu fallback.
func ProductionPermissionCatalog() []domain.Permission {
	return []domain.Permission{
		// The canonical permission points at overview; the summary comment keeps
		// the legacy contract discoverable for clients that still resolve it.
		{ID: "dashboard:overview:read", Name: "查看仪表盘", Method: http.MethodGet, Path: "/api/admin/v1/dashboard/overview", Active: true}, // legacy Path: "/api/admin/v1/dashboard/summary"
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
		{ID: "system:parameters:read", Name: "查看系统参数", Method: http.MethodGet, Path: "/api/admin/v1/settings", Active: true},
		{ID: "system:parameters:manage", Name: "管理系统参数", Method: "*", Path: "/api/admin/v1/settings", Active: true},
		{ID: "system:dictionary:read", Name: "查看字典", Method: http.MethodGet, Path: "/api/admin/v1/dictionaries/*", Active: true},
		{ID: "system:dictionary:manage", Name: "管理字典", Method: "*", Path: "/api/admin/v1/dictionaries/*", Active: true},
		{ID: "system:mail:read", Name: "查看邮件服务", Method: http.MethodGet, Path: "/api/admin/v1/mail/*", Active: true},
		{ID: "system:mail:manage", Name: "管理邮件服务", Method: "*", Path: "/api/admin/v1/mail/*", Active: true},
		{ID: "system:mail:test", Name: "测试邮件账号", Method: http.MethodPost, Path: "/api/admin/v1/mail/accounts/:id/test", Active: true},
		// Common notification resources use their own permission families so a
		// caller/template editor can be delegated without granting every mail
		// account operation. The trailing wildcard covers the collection root and
		// its registered resource routes while preserving method boundaries.
		{ID: "notification:callers:read", Name: "查看通知调用者", Method: http.MethodGet, Path: "/api/admin/v1/notification/callers/*", Active: true},
		{ID: "notification:callers:manage", Name: "管理通知调用者", Method: "*", Path: "/api/admin/v1/notification/callers/*", Active: true},
		{ID: "notification:accounts:read", Name: "查看通知账号", Method: http.MethodGet, Path: "/api/admin/v1/notification/accounts/*", Active: true},
		{ID: "notification:accounts:manage", Name: "管理通知账号", Method: "*", Path: "/api/admin/v1/notification/accounts/*", Active: true},
		{ID: "notification:templates:read", Name: "查看通知模板", Method: http.MethodGet, Path: "/api/admin/v1/notification/templates/*", Active: true},
		{ID: "notification:templates:manage", Name: "管理通知模板", Method: "*", Path: "/api/admin/v1/notification/templates/*", Active: true},
		{ID: "notification:templates:publish", Name: "发布通知模板", Method: http.MethodPost, Path: "/api/admin/v1/notification/templates/:id/publish", Active: true},
		{ID: "notification:templates:test", Name: "测试通知模板", Method: http.MethodPost, Path: "/api/admin/v1/notification/templates/:id/test", Active: true},
		{ID: "notification:verification:read", Name: "查看验证码挑战", Method: http.MethodGet, Path: "/api/admin/v1/notification/verification/*", Active: true},
		{ID: "notification:verification:manage", Name: "管理验证码挑战", Method: "*", Path: "/api/admin/v1/notification/verification/*", Active: true},
		{ID: "notification:verification-policies:read", Name: "查看验证码策略", Method: http.MethodGet, Path: "/api/admin/v1/notification/verification-policies/*", Active: true},
		{ID: "notification:verification-policies:manage", Name: "管理验证码策略", Method: http.MethodPatch, Path: "/api/admin/v1/notification/verification-policies/:policy_key", Active: true},
		{ID: "system:files:read", Name: "查看文件", Method: http.MethodGet, Path: "/api/admin/v1/files/*", Active: true},
		{ID: "system:files:manage", Name: "管理文件", Method: "*", Path: "/api/admin/v1/files/*", Active: true},
		// Media is the canonical resource surface. The IAM evaluator keeps an
		// explicit /files <-> /media compatibility bridge so persisted grants
		// from the migration window continue to authorize both adapters.
		{ID: "media:library:read", Name: "查看媒体库", Method: http.MethodGet, Path: "/api/admin/v1/media/*", Active: true},
		{ID: "media:library:manage", Name: "管理媒体库", Method: "*", Path: "/api/admin/v1/media/*", Active: true},
		{ID: "system:observability:read", Name: "查看可观测设置", Method: http.MethodGet, Path: "/api/admin/v1/observability/settings/*", Active: true},
		{ID: "system:observability:manage", Name: "管理可观测设置", Method: http.MethodPut, Path: "/api/admin/v1/observability/settings/*", Active: true},
		{ID: "ops:monitor:read", Name: "查看资源监控", Method: http.MethodGet, Path: "/api/admin/v1/ops/monitor", Active: true},
		{ID: "ops:server-status:read", Name: "查看服务器状态", Method: http.MethodGet, Path: "/api/admin/v1/ops/server-status", Active: true},
		{ID: "ops:audit:read", Name: "查看审计日志", Method: http.MethodGet, Path: "/api/admin/v1/audit/*", Active: true},
		{ID: "ops:operation-history:read", Name: "查看操作历史", Method: http.MethodGet, Path: "/api/admin/v1/ops/operation-history", Active: true},
		{ID: "ops:login-logs:read", Name: "查看登录日志", Method: http.MethodGet, Path: "/api/admin/v1/ops/login-logs", Active: true},
		{ID: "ops:tasks:read", Name: "查看任务", Method: http.MethodGet, Path: "/api/admin/v1/tasks/*", Active: true},
		{ID: "ops:tasks:manage", Name: "管理任务", Method: "*", Path: "/api/admin/v1/tasks/*", Active: true},
		{ID: "ops:data-jobs:read", Name: "查看数据作业", Method: http.MethodGet, Path: "/api/admin/v1/import-export/*", Active: true},
		{ID: "ops:data-jobs:manage", Name: "管理数据作业", Method: "*", Path: "/api/admin/v1/import-export/*", Active: true},
	}
}
