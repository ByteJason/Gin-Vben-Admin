package migrations_test

import "testing"

func TestRBACSchemaIncludesTenantScopedTables(t *testing.T) {
	for _, table := range []string{"roles", "user_roles", "menus", "permissions", "iam_policies", "iam_data_scopes"} {
		requireFields(t, table, "tenant_id")
	}
}
