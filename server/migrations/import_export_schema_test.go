package migrations_test

import "testing"

func TestImportExportSchemaHasBoundedJobsErrorsArtifactsAndLifecycleFields(t *testing.T) {
	requireFields(t, "gvba_import_jobs", "idempotency_key", "processed_rows", "expires_at", "deleted_at", "created_at", "updated_at")
	requireFields(t, "gvba_import_errors", "job_id", "row_number", "message_key")
	requireFields(t, "gvba_import_artifacts", "job_id", "sha256", "size_bytes", "expires_at")
}
