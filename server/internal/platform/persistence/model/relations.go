package model

// RelationKind describes an application-level relationship. These descriptors
// are documentation/query metadata only: they intentionally contain no GORM
// association fields, so passing All() to a Migrator never creates foreign keys
// or implicit many-to-many tables.
type RelationKind string

const (
	RelationBelongsTo RelationKind = "belongs_to"
	RelationHasOne    RelationKind = "has_one"
	RelationHasMany   RelationKind = "has_many"
)

// Relation describes a tenant-scoped relationship that query models may use.
// Keys is a human-readable mapping (for example, "user_roles.user_id -> users.id").
type Relation struct {
	From        string
	To          string
	Kind        RelationKind
	Keys        string
	TenantKey   string
	Description string
}

var relations = []Relation{
	{From: "organizations", To: "tenants", Kind: RelationBelongsTo, Keys: "organizations.tenant_id -> tenants.id", TenantKey: "tenant_id", Description: "组织所属租户"},
	{From: "organizations", To: "organizations", Kind: RelationBelongsTo, Keys: "organizations.parent_id -> organizations.id", TenantKey: "tenant_id", Description: "组织父子树"},
	{From: "users", To: "tenants", Kind: RelationBelongsTo, Keys: "users.tenant_id -> tenants.id", TenantKey: "tenant_id", Description: "用户所属租户"},
	{From: "users", To: "organizations", Kind: RelationBelongsTo, Keys: "users.org_id -> organizations.id", TenantKey: "tenant_id", Description: "用户所属组织"},
	{From: "roles", To: "tenants", Kind: RelationBelongsTo, Keys: "roles.tenant_id -> tenants.id", TenantKey: "tenant_id", Description: "角色所属租户"},
	{From: "roles", To: "organizations", Kind: RelationBelongsTo, Keys: "roles.org_id -> organizations.id", TenantKey: "tenant_id", Description: "角色所属组织"},
	{From: "user_roles", To: "users", Kind: RelationBelongsTo, Keys: "user_roles.user_id -> users.id", TenantKey: "tenant_id", Description: "用户角色分配"},
	{From: "user_roles", To: "roles", Kind: RelationBelongsTo, Keys: "user_roles.role_id -> roles.id", TenantKey: "tenant_id", Description: "角色成员关系"},
	{From: "menus", To: "tenants", Kind: RelationBelongsTo, Keys: "menus.tenant_id -> tenants.id", TenantKey: "tenant_id", Description: "菜单所属租户"},
	{From: "menus", To: "organizations", Kind: RelationBelongsTo, Keys: "menus.org_id -> organizations.id", TenantKey: "tenant_id", Description: "菜单所属组织"},
	{From: "menus", To: "menus", Kind: RelationBelongsTo, Keys: "menus.parent_id -> menus.id", TenantKey: "tenant_id", Description: "菜单父子树"},
	{From: "permissions", To: "tenants", Kind: RelationBelongsTo, Keys: "permissions.tenant_id -> tenants.id", TenantKey: "tenant_id", Description: "权限所属租户"},
	{From: "permissions", To: "organizations", Kind: RelationBelongsTo, Keys: "permissions.org_id -> organizations.id", TenantKey: "tenant_id", Description: "权限所属组织"},
	{From: "iam_policies", To: "users", Kind: RelationBelongsTo, Keys: "iam_policies.user_id -> users.id", TenantKey: "tenant_id", Description: "用户策略主体"},
	{From: "iam_policies", To: "roles", Kind: RelationBelongsTo, Keys: "iam_policies.role_id -> roles.id", TenantKey: "tenant_id", Description: "角色策略主体"},
	{From: "iam_data_scopes", To: "users", Kind: RelationBelongsTo, Keys: "iam_data_scopes.user_id -> users.id", TenantKey: "tenant_id", Description: "用户数据范围"},
	{From: "iam_data_scopes", To: "roles", Kind: RelationBelongsTo, Keys: "iam_data_scopes.role_id -> roles.id", TenantKey: "tenant_id", Description: "角色数据范围"},
	{From: "auth_sessions", To: "users", Kind: RelationBelongsTo, Keys: "auth_sessions.user_id -> users.id", TenantKey: "tenant_id", Description: "用户认证会话"},
	{From: "auth_audit_events", To: "users", Kind: RelationBelongsTo, Keys: "auth_audit_events.user_id -> users.id", TenantKey: "tenant_id", Description: "认证审计用户"},
	{From: "auth_audit_events", To: "auth_sessions", Kind: RelationBelongsTo, Keys: "auth_audit_events.session_id -> auth_sessions.id", TenantKey: "tenant_id", Description: "认证审计会话"},
	{From: "setting_versions", To: "tenants", Kind: RelationBelongsTo, Keys: "setting_versions.tenant_id -> tenants.id", TenantKey: "tenant_id", Description: "租户设置版本"},
	{From: "file_objects", To: "tenants", Kind: RelationBelongsTo, Keys: "file_objects.tenant_id -> tenants.id", TenantKey: "tenant_id", Description: "文件所属租户"},
	{From: "file_objects", To: "users", Kind: RelationBelongsTo, Keys: "file_objects.owner_id -> users.id", TenantKey: "tenant_id", Description: "文件所有者（逻辑身份键）"},
	{From: "smtp_accounts", To: "tenants", Kind: RelationBelongsTo, Keys: "smtp_accounts.tenant_id -> tenants.id", TenantKey: "tenant_id", Description: "SMTP账户所属租户"},
	{From: "email_messages", To: "smtp_accounts", Kind: RelationBelongsTo, Keys: "email_messages.smtp_account_id -> smtp_accounts.id", TenantKey: "tenant_id", Description: "邮件发送账户"},
	{From: "email_messages", To: "users", Kind: RelationBelongsTo, Keys: "email_messages.sender_id -> users.id", TenantKey: "tenant_id", Description: "邮件发送者（逻辑身份键）"},
	{From: "email_recipients", To: "email_messages", Kind: RelationBelongsTo, Keys: "email_recipients.message_id -> email_messages.id", TenantKey: "tenant_id", Description: "邮件收件人"},
	{From: "email_delivery_attempts", To: "email_messages", Kind: RelationBelongsTo, Keys: "email_delivery_attempts.message_id -> email_messages.id", TenantKey: "tenant_id", Description: "邮件投递记录"},
	{From: "email_delivery_attempts", To: "smtp_accounts", Kind: RelationBelongsTo, Keys: "email_delivery_attempts.account_id -> smtp_accounts.id", TenantKey: "tenant_id", Description: "邮件投递账户"},
	{From: "dictionary_items", To: "dictionary_types", Kind: RelationBelongsTo, Keys: "dictionary_items.type_code -> dictionary_types.code", TenantKey: "tenant_id", Description: "字典项类型"},
	{From: "dictionary_cache_versions", To: "dictionary_types", Kind: RelationBelongsTo, Keys: "dictionary_cache_versions.type_code -> dictionary_types.code", TenantKey: "tenant_id", Description: "字典缓存版本"},
	{From: "task_runs", To: "task_definitions", Kind: RelationBelongsTo, Keys: "task_runs.task_id -> task_definitions.id", TenantKey: "tenant_id", Description: "任务运行记录"},
	{From: "task_run_logs", To: "task_runs", Kind: RelationBelongsTo, Keys: "task_run_logs.run_id -> task_runs.id", TenantKey: "tenant_id", Description: "任务运行日志"},
	{From: "import_export_jobs", To: "users", Kind: RelationBelongsTo, Keys: "import_export_jobs.actor_id -> users.id", TenantKey: "tenant_id", Description: "导入导出操作者（逻辑身份键）"},
	{From: "import_export_errors", To: "import_export_jobs", Kind: RelationBelongsTo, Keys: "import_export_errors.job_id -> import_export_jobs.id", TenantKey: "tenant_id", Description: "导入导出错误"},
	{From: "import_export_artifacts", To: "import_export_jobs", Kind: RelationBelongsTo, Keys: "import_export_artifacts.job_id -> import_export_jobs.id", TenantKey: "tenant_id", Description: "导入导出工件"},

	// Reverse cardinalities are kept as metadata for query-model builders. They
	// do not add slice fields to the migration models.
	{From: "tenants", To: "organizations", Kind: RelationHasMany, Keys: "tenants.id -> organizations.tenant_id", TenantKey: "tenant_id", Description: "租户下的组织"},
	{From: "tenants", To: "users", Kind: RelationHasMany, Keys: "tenants.id -> users.tenant_id", TenantKey: "tenant_id", Description: "租户下的用户"},
	{From: "tenants", To: "roles", Kind: RelationHasMany, Keys: "tenants.id -> roles.tenant_id", TenantKey: "tenant_id", Description: "租户下的角色"},
	{From: "tenants", To: "menus", Kind: RelationHasMany, Keys: "tenants.id -> menus.tenant_id", TenantKey: "tenant_id", Description: "租户下的菜单"},
	{From: "tenants", To: "permissions", Kind: RelationHasMany, Keys: "tenants.id -> permissions.tenant_id", TenantKey: "tenant_id", Description: "租户下的权限"},
	{From: "tenants", To: "auth_sessions", Kind: RelationHasMany, Keys: "tenants.id -> auth_sessions.tenant_id", TenantKey: "tenant_id", Description: "租户下的会话"},
	{From: "tenants", To: "auth_audit_events", Kind: RelationHasMany, Keys: "tenants.id -> auth_audit_events.tenant_id", TenantKey: "tenant_id", Description: "租户下的认证审计"},
	{From: "tenants", To: "setting_versions", Kind: RelationHasMany, Keys: "tenants.id -> setting_versions.tenant_id", TenantKey: "tenant_id", Description: "租户设置版本"},
	{From: "tenants", To: "file_objects", Kind: RelationHasMany, Keys: "tenants.id -> file_objects.tenant_id", TenantKey: "tenant_id", Description: "租户文件"},
	{From: "tenants", To: "smtp_accounts", Kind: RelationHasMany, Keys: "tenants.id -> smtp_accounts.tenant_id", TenantKey: "tenant_id", Description: "租户 SMTP 账户"},
	{From: "tenants", To: "dictionary_types", Kind: RelationHasMany, Keys: "tenants.id -> dictionary_types.tenant_id", TenantKey: "tenant_id", Description: "租户字典类型"},
	{From: "tenants", To: "task_definitions", Kind: RelationHasMany, Keys: "tenants.id -> task_definitions.tenant_id", TenantKey: "tenant_id", Description: "租户任务定义"},
	{From: "tenants", To: "import_export_jobs", Kind: RelationHasMany, Keys: "tenants.id -> import_export_jobs.tenant_id", TenantKey: "tenant_id", Description: "租户导入导出作业"},
	{From: "organizations", To: "organizations", Kind: RelationHasMany, Keys: "organizations.id -> organizations.parent_id", TenantKey: "tenant_id", Description: "组织子节点"},
	{From: "organizations", To: "users", Kind: RelationHasMany, Keys: "organizations.id -> users.org_id", TenantKey: "tenant_id", Description: "组织用户"},
	{From: "organizations", To: "roles", Kind: RelationHasMany, Keys: "organizations.id -> roles.org_id", TenantKey: "tenant_id", Description: "组织角色"},
	{From: "organizations", To: "menus", Kind: RelationHasMany, Keys: "organizations.id -> menus.org_id", TenantKey: "tenant_id", Description: "组织菜单"},
	{From: "organizations", To: "permissions", Kind: RelationHasMany, Keys: "organizations.id -> permissions.org_id", TenantKey: "tenant_id", Description: "组织权限"},
	{From: "users", To: "user_roles", Kind: RelationHasMany, Keys: "users.id -> user_roles.user_id", TenantKey: "tenant_id", Description: "用户角色分配"},
	{From: "users", To: "iam_policies", Kind: RelationHasMany, Keys: "users.id -> iam_policies.user_id", TenantKey: "tenant_id", Description: "用户策略"},
	{From: "users", To: "iam_data_scopes", Kind: RelationHasMany, Keys: "users.id -> iam_data_scopes.user_id", TenantKey: "tenant_id", Description: "用户数据范围"},
	{From: "users", To: "auth_sessions", Kind: RelationHasMany, Keys: "users.id -> auth_sessions.user_id", TenantKey: "tenant_id", Description: "用户会话"},
	{From: "users", To: "auth_audit_events", Kind: RelationHasMany, Keys: "users.id -> auth_audit_events.user_id", TenantKey: "tenant_id", Description: "用户认证审计"},
	{From: "roles", To: "user_roles", Kind: RelationHasMany, Keys: "roles.id -> user_roles.role_id", TenantKey: "tenant_id", Description: "角色成员"},
	{From: "roles", To: "iam_policies", Kind: RelationHasMany, Keys: "roles.id -> iam_policies.role_id", TenantKey: "tenant_id", Description: "角色策略"},
	{From: "roles", To: "iam_data_scopes", Kind: RelationHasMany, Keys: "roles.id -> iam_data_scopes.role_id", TenantKey: "tenant_id", Description: "角色数据范围"},
	{From: "menus", To: "menus", Kind: RelationHasMany, Keys: "menus.id -> menus.parent_id", TenantKey: "tenant_id", Description: "菜单子节点"},
	{From: "smtp_accounts", To: "email_messages", Kind: RelationHasMany, Keys: "smtp_accounts.id -> email_messages.smtp_account_id", TenantKey: "tenant_id", Description: "SMTP账户邮件"},
	{From: "smtp_accounts", To: "email_delivery_attempts", Kind: RelationHasMany, Keys: "smtp_accounts.id -> email_delivery_attempts.account_id", TenantKey: "tenant_id", Description: "SMTP投递尝试"},
	{From: "email_messages", To: "email_recipients", Kind: RelationHasMany, Keys: "email_messages.id -> email_recipients.message_id", TenantKey: "tenant_id", Description: "邮件收件人"},
	{From: "email_messages", To: "email_delivery_attempts", Kind: RelationHasMany, Keys: "email_messages.id -> email_delivery_attempts.message_id", TenantKey: "tenant_id", Description: "邮件投递尝试"},
	{From: "dictionary_types", To: "dictionary_items", Kind: RelationHasMany, Keys: "dictionary_types.code -> dictionary_items.type_code", TenantKey: "tenant_id", Description: "字典项"},
	{From: "dictionary_types", To: "dictionary_cache_versions", Kind: RelationHasMany, Keys: "dictionary_types.code -> dictionary_cache_versions.type_code", TenantKey: "tenant_id", Description: "字典缓存版本"},
	{From: "task_definitions", To: "task_runs", Kind: RelationHasMany, Keys: "task_definitions.id -> task_runs.task_id", TenantKey: "tenant_id", Description: "任务运行记录"},
	{From: "task_runs", To: "task_run_logs", Kind: RelationHasMany, Keys: "task_runs.id -> task_run_logs.run_id", TenantKey: "tenant_id", Description: "任务运行日志"},
	{From: "import_export_jobs", To: "import_export_errors", Kind: RelationHasMany, Keys: "import_export_jobs.id -> import_export_errors.job_id", TenantKey: "tenant_id", Description: "导入导出错误"},
	{From: "import_export_jobs", To: "import_export_artifacts", Kind: RelationHasOne, Keys: "import_export_jobs.id -> import_export_artifacts.job_id", TenantKey: "tenant_id", Description: "导入导出工件（每个作业一个）"},
}

// Relations returns a copy of the planned application-level relationship map.
func Relations() []Relation {
	result := make([]Relation, len(relations))
	copy(result, relations)
	return result
}
