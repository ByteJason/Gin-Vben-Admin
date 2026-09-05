package migrations_test

import "testing"

func TestTenantSchemaDeclaresIsolationAndScopedUniquenessFields(t *testing.T) {
	for _, table := range []string{"gvba_sys_organizations", "gvba_iam_users", "gvba_iam_roles", "gvba_iam_menus", "gvba_iam_permissions", "gvba_iam_policies", "gvba_iam_data_scopes", "gvba_auth_sessions", "gvba_audit_auth_events", "gvba_sys_setting_versions"} {
		requireFields(t, table, "tenant_id")
	}
	requireFields(t, "gvba_sys_organizations", "parent_id")
	requireFields(t, "gvba_iam_users", "username", "username_normalized")
	requireFields(t, "gvba_sys_setting_versions", "key", "version")
}
