package model

var tableComments = map[string]string{
	"gvba_sys_app_metadata":               "应用元数据",
	"gvba_sys_tenants":                    "租户",
	"gvba_sys_organizations":              "组织",
	"gvba_iam_users":                      "用户",
	"gvba_iam_roles":                      "角色",
	"gvba_iam_user_roles":                 "用户角色关系",
	"gvba_iam_menus":                      "菜单",
	"gvba_iam_permissions":                "权限",
	"gvba_iam_policies":                   "访问策略",
	"gvba_iam_data_scopes":                "数据范围",
	"gvba_auth_sessions":                  "认证会话",
	"gvba_audit_auth_events":              "认证审计事件",
	"gvba_sys_setting_versions":           "设置版本",
	"gvba_storage_file_objects":           "文件对象",
	"gvba_storage_media_categories":       "媒体分类",
	"gvba_storage_media_usages":           "媒体引用",
	"gvba_notify_smtp_accounts":           "SMTP账户",
	"gvba_notify_email_messages":          "邮件消息",
	"gvba_notify_email_recipients":        "邮件收件人",
	"gvba_notify_email_delivery_attempts": "邮件投递尝试",
	"gvba_notify_callers":                 "通知调用者",
	"gvba_notify_caller_accounts":         "通知调用者账户绑定",
	"gvba_notify_templates":               "通知模板",
	"gvba_notify_template_locales":        "通知模板语言版本",
	"gvba_notify_template_versions":       "通知模板发布快照",
	"gvba_verify_policies":                "验证码策略",
	"gvba_verify_challenges":              "验证码挑战",
	"gvba_dict_types":                     "字典类型",
	"gvba_dict_items":                     "字典项",
	"gvba_dict_cache_versions":            "字典缓存版本",
	"gvba_task_definitions":               "任务定义",
	"gvba_task_runs":                      "任务运行记录",
	"gvba_task_run_logs":                  "任务运行日志",
	"gvba_import_jobs":                    "导入导出作业",
	"gvba_import_errors":                  "导入导出错误",
	"gvba_import_artifacts":               "导入导出工件",
}

// TableComments returns a copy of the table comment registry.
func TableComments() map[string]string {
	result := make(map[string]string, len(tableComments))
	for name, comment := range tableComments {
		result[name] = comment
	}
	return result
}

// TableComment returns the short human-readable comment for a table.
func TableComment(table string) string { return tableComments[table] }
