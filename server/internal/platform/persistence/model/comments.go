package model

var tableComments = map[string]string{
	"app_metadata":                   "应用元数据",
	"tenants":                        "租户",
	"organizations":                  "组织",
	"users":                          "用户",
	"roles":                          "角色",
	"user_roles":                     "用户角色关系",
	"menus":                          "菜单",
	"permissions":                    "权限",
	"iam_policies":                   "访问策略",
	"iam_data_scopes":                "数据范围",
	"auth_sessions":                  "认证会话",
	"auth_audit_events":              "认证审计事件",
	"setting_versions":               "设置版本",
	"file_objects":                   "文件对象",
	"media_categories":               "媒体分类",
	"media_usages":                   "媒体引用",
	"smtp_accounts":                  "SMTP账户",
	"email_messages":                 "邮件消息",
	"email_recipients":               "邮件收件人",
	"email_delivery_attempts":        "邮件投递尝试",
	"notification_callers":           "通知调用者",
	"notification_caller_accounts":   "通知调用者账户绑定",
	"notification_templates":         "通知模板",
	"notification_template_locales":  "通知模板语言版本",
	"notification_template_versions": "通知模板发布快照",
	"verification_policies":          "验证码策略",
	"verification_challenges":        "验证码挑战",
	"dictionary_types":               "字典类型",
	"dictionary_items":               "字典项",
	"dictionary_cache_versions":      "字典缓存版本",
	"task_definitions":               "任务定义",
	"task_runs":                      "任务运行记录",
	"task_run_logs":                  "任务运行日志",
	"import_export_jobs":             "导入导出作业",
	"import_export_errors":           "导入导出错误",
	"import_export_artifacts":        "导入导出工件",
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
