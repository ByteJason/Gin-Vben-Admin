package migrations_test

import (
	"strings"
	"testing"

	"github.com/ByteJason/Gin-Vben-Admin/server/migrations"
)

func TestTaskDefinitionMigrationIsPairedAndTenantScoped(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		up, err := migrations.FS.ReadFile(driver + "/000017_tasks.up.sql")
		if err != nil {
			t.Fatalf("read %s task migration: %v", driver, err)
		}
		down, err := migrations.FS.ReadFile(driver + "/000017_tasks.down.sql")
		if err != nil || len(strings.TrimSpace(string(down))) == 0 {
			t.Fatalf("%s task down migration missing: %v", driver, err)
		}
		sql := strings.ToLower(string(up))
		for _, token := range []string{"task_definitions", "tenant_id", "org_id", "payload_schema", "concurrency_policy", "deleted_at", "created_at", "updated_at", "primary key"} {
			if !strings.Contains(sql, token) {
				t.Fatalf("%s task migration missing %q", driver, token)
			}
		}
	}
}
