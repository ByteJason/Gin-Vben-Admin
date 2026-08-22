package migrations_test

import (
	"strings"
	"testing"

	"example.com/gin-vben-admin/server/migrations"
)

func TestImportExportMigrationHasBoundedJobsErrorsArtifactsAndLifecycleMetadata(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		up, err := migrations.FS.ReadFile(driver + "/000019_import_export_jobs.up.sql")
		if err != nil {
			t.Fatalf("read %s import/export migration: %v", driver, err)
		}
		down, err := migrations.FS.ReadFile(driver + "/000019_import_export_jobs.down.sql")
		if err != nil || len(strings.TrimSpace(string(down))) == 0 {
			t.Fatalf("%s import/export down migration missing: %v", driver, err)
		}
		sql := strings.ToLower(string(up))
		for _, token := range []string{"import_export_jobs", "import_export_errors", "import_export_artifacts", "idempotency_key", "processed_rows", "expires_at", "deleted_at", "created_at", "updated_at"} {
			if !strings.Contains(sql, token) {
				t.Fatalf("%s migration missing %q", driver, token)
			}
		}
	}
}
