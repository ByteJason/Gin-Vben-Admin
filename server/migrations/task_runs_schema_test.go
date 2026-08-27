package migrations_test

import "testing"

func TestTaskRunSchemaHasStatusLogsAndLifecycleFields(t *testing.T) {
	requireFields(t, "task_runs", "idempotency_key", "status", "deleted_at", "created_at", "updated_at")
	requireFields(t, "task_run_logs", "run_id", "status", "error_code", "created_at", "updated_at")
}
