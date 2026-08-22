package migrations_test

import (
	"strings"
	"testing"

	"example.com/gin-vben-admin/server/migrations"
)

func TestTaskRunMigrationHasStatusLogAndLifecycleMetadata(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		up, err := migrations.FS.ReadFile(driver + "/000018_task_runs.up.sql")
		if err != nil {
			t.Fatalf("read %s task-runs migration: %v", driver, err)
		}
		down, err := migrations.FS.ReadFile(driver + "/000018_task_runs.down.sql")
		if err != nil || len(strings.TrimSpace(string(down))) == 0 {
			t.Fatalf("%s task-runs down migration missing: %v", driver, err)
		}
		sql := strings.ToLower(string(up))
		for _, token := range []string{"task_runs", "task_run_logs", "idempotency_key", "dead_letter", "cancelled", "deleted_at", "created_at", "updated_at"} {
			if !strings.Contains(sql, token) {
				t.Fatalf("%s task-runs migration missing %q", driver, token)
			}
		}
	}
}
