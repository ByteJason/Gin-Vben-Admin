package migrations_test

import "testing"

func TestImportExportSchemaHasBoundedJobsErrorsArtifactsAndLifecycleFields(t *testing.T) {
	requireFields(t, "import_export_jobs", "idempotency_key", "processed_rows", "expires_at", "deleted_at", "created_at", "updated_at")
	requireFields(t, "import_export_errors", "job_id", "row_number", "message_key")
	requireFields(t, "import_export_artifacts", "job_id", "sha256", "size_bytes", "expires_at")
}
