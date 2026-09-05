package migrations_test

import "testing"

func TestRBACSchemaIncludesTenantScopedTables(t *testing.T) {
	for _, table := range []string{"gvba_iam_roles", "gvba_iam_user_roles", "gvba_iam_menus", "gvba_iam_permissions", "gvba_iam_policies", "gvba_iam_data_scopes"} {
		requireFields(t, table, "tenant_id")
	}
}
