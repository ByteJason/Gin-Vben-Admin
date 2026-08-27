package migrations_test

import "testing"

func TestTaskDefinitionSchemaIsTenantScoped(t *testing.T) {
	requireFields(t, "task_definitions", "tenant_id", "org_id", "payload_schema", "concurrency_policy", "deleted_at", "created_at", "updated_at")
}
