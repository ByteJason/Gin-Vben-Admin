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
// Keys is a human-readable mapping (for example, "gvba_iam_user_roles.user_id -> gvba_iam_users.id").
type Relation struct {
	From        string
	To          string
	Kind        RelationKind
	Keys        string
	TenantKey   string
	Description string
}

var relations = []Relation{
	{From: "gvba_sys_organizations", To: "gvba_sys_tenants", Kind: RelationBelongsTo, Keys: "gvba_sys_organizations.tenant_id -> gvba_sys_tenants.id", TenantKey: "tenant_id", Description: "组织所属租户"},
	{From: "gvba_sys_organizations", To: "gvba_sys_organizations", Kind: RelationBelongsTo, Keys: "gvba_sys_organizations.parent_id -> gvba_sys_organizations.id", TenantKey: "tenant_id", Description: "组织父子树"},
	{From: "gvba_iam_users", To: "gvba_sys_tenants", Kind: RelationBelongsTo, Keys: "gvba_iam_users.tenant_id -> gvba_sys_tenants.id", TenantKey: "tenant_id", Description: "用户所属租户"},
	{From: "gvba_iam_users", To: "gvba_sys_organizations", Kind: RelationBelongsTo, Keys: "gvba_iam_users.org_id -> gvba_sys_organizations.id", TenantKey: "tenant_id", Description: "用户所属组织"},
	{From: "gvba_iam_roles", To: "gvba_sys_tenants", Kind: RelationBelongsTo, Keys: "gvba_iam_roles.tenant_id -> gvba_sys_tenants.id", TenantKey: "tenant_id", Description: "角色所属租户"},
	{From: "gvba_iam_roles", To: "gvba_sys_organizations", Kind: RelationBelongsTo, Keys: "gvba_iam_roles.org_id -> gvba_sys_organizations.id", TenantKey: "tenant_id", Description: "角色所属组织"},
	{From: "gvba_iam_user_roles", To: "gvba_iam_users", Kind: RelationBelongsTo, Keys: "gvba_iam_user_roles.user_id -> gvba_iam_users.id", TenantKey: "tenant_id", Description: "用户角色分配"},
	{From: "gvba_iam_user_roles", To: "gvba_iam_roles", Kind: RelationBelongsTo, Keys: "gvba_iam_user_roles.role_id -> gvba_iam_roles.id", TenantKey: "tenant_id", Description: "角色成员关系"},
	{From: "gvba_iam_menus", To: "gvba_sys_tenants", Kind: RelationBelongsTo, Keys: "gvba_iam_menus.tenant_id -> gvba_sys_tenants.id", TenantKey: "tenant_id", Description: "菜单所属租户"},
	{From: "gvba_iam_menus", To: "gvba_sys_organizations", Kind: RelationBelongsTo, Keys: "gvba_iam_menus.org_id -> gvba_sys_organizations.id", TenantKey: "tenant_id", Description: "菜单所属组织"},
	{From: "gvba_iam_menus", To: "gvba_iam_menus", Kind: RelationBelongsTo, Keys: "gvba_iam_menus.parent_id -> gvba_iam_menus.id", TenantKey: "tenant_id", Description: "菜单父子树"},
	{From: "gvba_iam_permissions", To: "gvba_sys_tenants", Kind: RelationBelongsTo, Keys: "gvba_iam_permissions.tenant_id -> gvba_sys_tenants.id", TenantKey: "tenant_id", Description: "权限所属租户"},
	{From: "gvba_iam_permissions", To: "gvba_sys_organizations", Kind: RelationBelongsTo, Keys: "gvba_iam_permissions.org_id -> gvba_sys_organizations.id", TenantKey: "tenant_id", Description: "权限所属组织"},
	{From: "gvba_iam_policies", To: "gvba_iam_users", Kind: RelationBelongsTo, Keys: "gvba_iam_policies.user_id -> gvba_iam_users.id", TenantKey: "tenant_id", Description: "用户策略主体"},
	{From: "gvba_iam_policies", To: "gvba_iam_roles", Kind: RelationBelongsTo, Keys: "gvba_iam_policies.role_id -> gvba_iam_roles.id", TenantKey: "tenant_id", Description: "角色策略主体"},
	{From: "gvba_iam_data_scopes", To: "gvba_iam_users", Kind: RelationBelongsTo, Keys: "gvba_iam_data_scopes.user_id -> gvba_iam_users.id", TenantKey: "tenant_id", Description: "用户数据范围"},
	{From: "gvba_iam_data_scopes", To: "gvba_iam_roles", Kind: RelationBelongsTo, Keys: "gvba_iam_data_scopes.role_id -> gvba_iam_roles.id", TenantKey: "tenant_id", Description: "角色数据范围"},
	{From: "gvba_auth_sessions", To: "gvba_iam_users", Kind: RelationBelongsTo, Keys: "gvba_auth_sessions.user_id -> gvba_iam_users.id", TenantKey: "tenant_id", Description: "用户认证会话"},
	{From: "gvba_audit_auth_events", To: "gvba_iam_users", Kind: RelationBelongsTo, Keys: "gvba_audit_auth_events.user_id -> gvba_iam_users.id", TenantKey: "tenant_id", Description: "认证审计用户"},
	{From: "gvba_audit_auth_events", To: "gvba_auth_sessions", Kind: RelationBelongsTo, Keys: "gvba_audit_auth_events.session_id -> gvba_auth_sessions.id", TenantKey: "tenant_id", Description: "认证审计会话"},
	{From: "gvba_sys_setting_versions", To: "gvba_sys_tenants", Kind: RelationBelongsTo, Keys: "gvba_sys_setting_versions.tenant_id -> gvba_sys_tenants.id", TenantKey: "tenant_id", Description: "租户设置版本"},
	{From: "gvba_storage_file_objects", To: "gvba_sys_tenants", Kind: RelationBelongsTo, Keys: "gvba_storage_file_objects.tenant_id -> gvba_sys_tenants.id", TenantKey: "tenant_id", Description: "文件所属租户"},
	{From: "gvba_storage_file_objects", To: "gvba_iam_users", Kind: RelationBelongsTo, Keys: "gvba_storage_file_objects.owner_id -> gvba_iam_users.id", TenantKey: "tenant_id", Description: "文件所有者（逻辑身份键）"},
	{From: "gvba_storage_file_objects", To: "gvba_storage_media_categories", Kind: RelationBelongsTo, Keys: "gvba_storage_file_objects.category_id -> gvba_storage_media_categories.id", TenantKey: "tenant_id", Description: "文件媒体分类"},
	{From: "gvba_storage_media_usages", To: "gvba_storage_file_objects", Kind: RelationBelongsTo, Keys: "gvba_storage_media_usages.resource_id -> gvba_storage_file_objects.id", TenantKey: "tenant_id", Description: "媒体引用资源"},
	{From: "gvba_notify_smtp_accounts", To: "gvba_sys_tenants", Kind: RelationBelongsTo, Keys: "gvba_notify_smtp_accounts.tenant_id -> gvba_sys_tenants.id", TenantKey: "tenant_id", Description: "SMTP账户所属租户"},
	{From: "gvba_notify_email_messages", To: "gvba_notify_smtp_accounts", Kind: RelationBelongsTo, Keys: "gvba_notify_email_messages.smtp_account_id -> gvba_notify_smtp_accounts.id", TenantKey: "tenant_id", Description: "邮件发送账户"},
	{From: "gvba_notify_email_messages", To: "gvba_iam_users", Kind: RelationBelongsTo, Keys: "gvba_notify_email_messages.sender_id -> gvba_iam_users.id", TenantKey: "tenant_id", Description: "邮件发送者（逻辑身份键）"},
	{From: "gvba_notify_email_recipients", To: "gvba_notify_email_messages", Kind: RelationBelongsTo, Keys: "gvba_notify_email_recipients.message_id -> gvba_notify_email_messages.id", TenantKey: "tenant_id", Description: "邮件收件人"},
	{From: "gvba_notify_email_delivery_attempts", To: "gvba_notify_email_messages", Kind: RelationBelongsTo, Keys: "gvba_notify_email_delivery_attempts.message_id -> gvba_notify_email_messages.id", TenantKey: "tenant_id", Description: "邮件投递记录"},
	{From: "gvba_notify_email_delivery_attempts", To: "gvba_notify_smtp_accounts", Kind: RelationBelongsTo, Keys: "gvba_notify_email_delivery_attempts.account_id -> gvba_notify_smtp_accounts.id", TenantKey: "tenant_id", Description: "邮件投递账户"},
	{From: "gvba_notify_caller_accounts", To: "gvba_notify_callers", Kind: RelationBelongsTo, Keys: "gvba_notify_caller_accounts.caller_id -> gvba_notify_callers.id", TenantKey: "tenant_id", Description: "通知调用者绑定"},
	{From: "gvba_notify_caller_accounts", To: "gvba_notify_smtp_accounts", Kind: RelationBelongsTo, Keys: "gvba_notify_caller_accounts.smtp_account_id -> gvba_notify_smtp_accounts.id", TenantKey: "tenant_id", Description: "通知绑定SMTP账户"},
	{From: "gvba_notify_template_locales", To: "gvba_notify_templates", Kind: RelationBelongsTo, Keys: "gvba_notify_template_locales.template_id -> gvba_notify_templates.id", TenantKey: "tenant_id", Description: "模板语言版本"},
	{From: "gvba_notify_template_versions", To: "gvba_notify_templates", Kind: RelationBelongsTo, Keys: "gvba_notify_template_versions.template_id -> gvba_notify_templates.id", TenantKey: "tenant_id", Description: "模板发布快照"},
	{From: "gvba_verify_policies", To: "gvba_notify_callers", Kind: RelationBelongsTo, Keys: "gvba_verify_policies.caller_id -> gvba_notify_callers.id", TenantKey: "tenant_id", Description: "验证码调用者策略"},
	{From: "gvba_verify_challenges", To: "gvba_notify_callers", Kind: RelationBelongsTo, Keys: "gvba_verify_challenges.caller_id -> gvba_notify_callers.id", TenantKey: "tenant_id", Description: "验证码调用者挑战"},
	{From: "gvba_verify_challenges", To: "gvba_notify_email_messages", Kind: RelationBelongsTo, Keys: "gvba_verify_challenges.message_id -> gvba_notify_email_messages.id", TenantKey: "tenant_id", Description: "验证码邮件消息"},
	{From: "gvba_dict_items", To: "gvba_dict_types", Kind: RelationBelongsTo, Keys: "gvba_dict_items.type_code -> gvba_dict_types.code", TenantKey: "tenant_id", Description: "字典项类型"},
	{From: "gvba_dict_cache_versions", To: "gvba_dict_types", Kind: RelationBelongsTo, Keys: "gvba_dict_cache_versions.type_code -> gvba_dict_types.code", TenantKey: "tenant_id", Description: "字典缓存版本"},
	{From: "gvba_task_runs", To: "gvba_task_definitions", Kind: RelationBelongsTo, Keys: "gvba_task_runs.task_id -> gvba_task_definitions.id", TenantKey: "tenant_id", Description: "任务运行记录"},
	{From: "gvba_task_run_logs", To: "gvba_task_runs", Kind: RelationBelongsTo, Keys: "gvba_task_run_logs.run_id -> gvba_task_runs.id", TenantKey: "tenant_id", Description: "任务运行日志"},
	{From: "gvba_import_jobs", To: "gvba_iam_users", Kind: RelationBelongsTo, Keys: "gvba_import_jobs.actor_id -> gvba_iam_users.id", TenantKey: "tenant_id", Description: "导入导出操作者（逻辑身份键）"},
	{From: "gvba_import_errors", To: "gvba_import_jobs", Kind: RelationBelongsTo, Keys: "gvba_import_errors.job_id -> gvba_import_jobs.id", TenantKey: "tenant_id", Description: "导入导出错误"},
	{From: "gvba_import_artifacts", To: "gvba_import_jobs", Kind: RelationBelongsTo, Keys: "gvba_import_artifacts.job_id -> gvba_import_jobs.id", TenantKey: "tenant_id", Description: "导入导出工件"},

	// Reverse cardinalities are kept as metadata for query-model builders. They
	// do not add slice fields to the migration models.
	{From: "gvba_sys_tenants", To: "gvba_sys_organizations", Kind: RelationHasMany, Keys: "gvba_sys_tenants.id -> gvba_sys_organizations.tenant_id", TenantKey: "tenant_id", Description: "租户下的组织"},
	{From: "gvba_sys_tenants", To: "gvba_iam_users", Kind: RelationHasMany, Keys: "gvba_sys_tenants.id -> gvba_iam_users.tenant_id", TenantKey: "tenant_id", Description: "租户下的用户"},
	{From: "gvba_sys_tenants", To: "gvba_iam_roles", Kind: RelationHasMany, Keys: "gvba_sys_tenants.id -> gvba_iam_roles.tenant_id", TenantKey: "tenant_id", Description: "租户下的角色"},
	{From: "gvba_sys_tenants", To: "gvba_iam_menus", Kind: RelationHasMany, Keys: "gvba_sys_tenants.id -> gvba_iam_menus.tenant_id", TenantKey: "tenant_id", Description: "租户下的菜单"},
	{From: "gvba_sys_tenants", To: "gvba_iam_permissions", Kind: RelationHasMany, Keys: "gvba_sys_tenants.id -> gvba_iam_permissions.tenant_id", TenantKey: "tenant_id", Description: "租户下的权限"},
	{From: "gvba_sys_tenants", To: "gvba_auth_sessions", Kind: RelationHasMany, Keys: "gvba_sys_tenants.id -> gvba_auth_sessions.tenant_id", TenantKey: "tenant_id", Description: "租户下的会话"},
	{From: "gvba_sys_tenants", To: "gvba_audit_auth_events", Kind: RelationHasMany, Keys: "gvba_sys_tenants.id -> gvba_audit_auth_events.tenant_id", TenantKey: "tenant_id", Description: "租户下的认证审计"},
	{From: "gvba_sys_tenants", To: "gvba_sys_setting_versions", Kind: RelationHasMany, Keys: "gvba_sys_tenants.id -> gvba_sys_setting_versions.tenant_id", TenantKey: "tenant_id", Description: "租户设置版本"},
	{From: "gvba_sys_tenants", To: "gvba_storage_file_objects", Kind: RelationHasMany, Keys: "gvba_sys_tenants.id -> gvba_storage_file_objects.tenant_id", TenantKey: "tenant_id", Description: "租户文件"},
	{From: "gvba_sys_tenants", To: "gvba_notify_smtp_accounts", Kind: RelationHasMany, Keys: "gvba_sys_tenants.id -> gvba_notify_smtp_accounts.tenant_id", TenantKey: "tenant_id", Description: "租户 SMTP 账户"},
	{From: "gvba_sys_tenants", To: "gvba_dict_types", Kind: RelationHasMany, Keys: "gvba_sys_tenants.id -> gvba_dict_types.tenant_id", TenantKey: "tenant_id", Description: "租户字典类型"},
	{From: "gvba_sys_tenants", To: "gvba_task_definitions", Kind: RelationHasMany, Keys: "gvba_sys_tenants.id -> gvba_task_definitions.tenant_id", TenantKey: "tenant_id", Description: "租户任务定义"},
	{From: "gvba_sys_tenants", To: "gvba_import_jobs", Kind: RelationHasMany, Keys: "gvba_sys_tenants.id -> gvba_import_jobs.tenant_id", TenantKey: "tenant_id", Description: "租户导入导出作业"},
	{From: "gvba_sys_organizations", To: "gvba_sys_organizations", Kind: RelationHasMany, Keys: "gvba_sys_organizations.id -> gvba_sys_organizations.parent_id", TenantKey: "tenant_id", Description: "组织子节点"},
	{From: "gvba_sys_organizations", To: "gvba_iam_users", Kind: RelationHasMany, Keys: "gvba_sys_organizations.id -> gvba_iam_users.org_id", TenantKey: "tenant_id", Description: "组织用户"},
	{From: "gvba_sys_organizations", To: "gvba_iam_roles", Kind: RelationHasMany, Keys: "gvba_sys_organizations.id -> gvba_iam_roles.org_id", TenantKey: "tenant_id", Description: "组织角色"},
	{From: "gvba_sys_organizations", To: "gvba_iam_menus", Kind: RelationHasMany, Keys: "gvba_sys_organizations.id -> gvba_iam_menus.org_id", TenantKey: "tenant_id", Description: "组织菜单"},
	{From: "gvba_sys_organizations", To: "gvba_iam_permissions", Kind: RelationHasMany, Keys: "gvba_sys_organizations.id -> gvba_iam_permissions.org_id", TenantKey: "tenant_id", Description: "组织权限"},
	{From: "gvba_iam_users", To: "gvba_iam_user_roles", Kind: RelationHasMany, Keys: "gvba_iam_users.id -> gvba_iam_user_roles.user_id", TenantKey: "tenant_id", Description: "用户角色分配"},
	{From: "gvba_iam_users", To: "gvba_iam_policies", Kind: RelationHasMany, Keys: "gvba_iam_users.id -> gvba_iam_policies.user_id", TenantKey: "tenant_id", Description: "用户策略"},
	{From: "gvba_iam_users", To: "gvba_iam_data_scopes", Kind: RelationHasMany, Keys: "gvba_iam_users.id -> gvba_iam_data_scopes.user_id", TenantKey: "tenant_id", Description: "用户数据范围"},
	{From: "gvba_iam_users", To: "gvba_auth_sessions", Kind: RelationHasMany, Keys: "gvba_iam_users.id -> gvba_auth_sessions.user_id", TenantKey: "tenant_id", Description: "用户会话"},
	{From: "gvba_iam_users", To: "gvba_audit_auth_events", Kind: RelationHasMany, Keys: "gvba_iam_users.id -> gvba_audit_auth_events.user_id", TenantKey: "tenant_id", Description: "用户认证审计"},
	{From: "gvba_iam_roles", To: "gvba_iam_user_roles", Kind: RelationHasMany, Keys: "gvba_iam_roles.id -> gvba_iam_user_roles.role_id", TenantKey: "tenant_id", Description: "角色成员"},
	{From: "gvba_iam_roles", To: "gvba_iam_policies", Kind: RelationHasMany, Keys: "gvba_iam_roles.id -> gvba_iam_policies.role_id", TenantKey: "tenant_id", Description: "角色策略"},
	{From: "gvba_iam_roles", To: "gvba_iam_data_scopes", Kind: RelationHasMany, Keys: "gvba_iam_roles.id -> gvba_iam_data_scopes.role_id", TenantKey: "tenant_id", Description: "角色数据范围"},
	{From: "gvba_iam_menus", To: "gvba_iam_menus", Kind: RelationHasMany, Keys: "gvba_iam_menus.id -> gvba_iam_menus.parent_id", TenantKey: "tenant_id", Description: "菜单子节点"},
	{From: "gvba_notify_smtp_accounts", To: "gvba_notify_email_messages", Kind: RelationHasMany, Keys: "gvba_notify_smtp_accounts.id -> gvba_notify_email_messages.smtp_account_id", TenantKey: "tenant_id", Description: "SMTP账户邮件"},
	{From: "gvba_notify_smtp_accounts", To: "gvba_notify_email_delivery_attempts", Kind: RelationHasMany, Keys: "gvba_notify_smtp_accounts.id -> gvba_notify_email_delivery_attempts.account_id", TenantKey: "tenant_id", Description: "SMTP投递尝试"},
	{From: "gvba_notify_email_messages", To: "gvba_notify_email_recipients", Kind: RelationHasMany, Keys: "gvba_notify_email_messages.id -> gvba_notify_email_recipients.message_id", TenantKey: "tenant_id", Description: "邮件收件人"},
	{From: "gvba_notify_email_messages", To: "gvba_notify_email_delivery_attempts", Kind: RelationHasMany, Keys: "gvba_notify_email_messages.id -> gvba_notify_email_delivery_attempts.message_id", TenantKey: "tenant_id", Description: "邮件投递尝试"},
	{From: "gvba_dict_types", To: "gvba_dict_items", Kind: RelationHasMany, Keys: "gvba_dict_types.code -> gvba_dict_items.type_code", TenantKey: "tenant_id", Description: "字典项"},
	{From: "gvba_dict_types", To: "gvba_dict_cache_versions", Kind: RelationHasMany, Keys: "gvba_dict_types.code -> gvba_dict_cache_versions.type_code", TenantKey: "tenant_id", Description: "字典缓存版本"},
	{From: "gvba_task_definitions", To: "gvba_task_runs", Kind: RelationHasMany, Keys: "gvba_task_definitions.id -> gvba_task_runs.task_id", TenantKey: "tenant_id", Description: "任务运行记录"},
	{From: "gvba_task_runs", To: "gvba_task_run_logs", Kind: RelationHasMany, Keys: "gvba_task_runs.id -> gvba_task_run_logs.run_id", TenantKey: "tenant_id", Description: "任务运行日志"},
	{From: "gvba_import_jobs", To: "gvba_import_errors", Kind: RelationHasMany, Keys: "gvba_import_jobs.id -> gvba_import_errors.job_id", TenantKey: "tenant_id", Description: "导入导出错误"},
	{From: "gvba_import_jobs", To: "gvba_import_artifacts", Kind: RelationHasOne, Keys: "gvba_import_jobs.id -> gvba_import_artifacts.job_id", TenantKey: "tenant_id", Description: "导入导出工件（每个作业一个）"},
}

// Relations returns a copy of the planned application-level relationship map.
func Relations() []Relation {
	result := make([]Relation, len(relations))
	copy(result, relations)
	return result
}
