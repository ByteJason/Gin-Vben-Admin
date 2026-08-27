package migrations_test

import "testing"

func TestTenantSchemaDeclaresIsolationAndScopedUniquenessFields(t *testing.T) {
	for _, table := range []string{"organizations", "users", "roles", "menus", "permissions", "iam_policies", "iam_data_scopes", "auth_sessions", "auth_audit_events", "setting_versions"} {
		requireFields(t, table, "tenant_id")
	}
	requireFields(t, "organizations", "parent_id")
	requireFields(t, "users", "username", "username_normalized")
	requireFields(t, "setting_versions", "key", "version")
}
