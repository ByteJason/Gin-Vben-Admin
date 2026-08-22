CREATE TABLE IF NOT EXISTS import_export_jobs (
 id VARCHAR(64) PRIMARY KEY, kind VARCHAR(16) NOT NULL CHECK (kind IN ('import','export')), tenant_id VARCHAR(128) NOT NULL, org_id VARCHAR(128) NOT NULL DEFAULT '', actor_id VARCHAR(128) NOT NULL DEFAULT '',
 preview_id VARCHAR(64) NOT NULL DEFAULT '', queue_task_id VARCHAR(64) NOT NULL DEFAULT '', idempotency_key VARCHAR(191) NOT NULL,
 status VARCHAR(32) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','succeeded','failed','cancelled')), format VARCHAR(16) NOT NULL DEFAULT 'csv', total_rows INTEGER NOT NULL DEFAULT 0 CHECK (total_rows >= 0), processed_rows INTEGER NOT NULL DEFAULT 0 CHECK (processed_rows >= 0), error_count INTEGER NOT NULL DEFAULT 0 CHECK (error_count >= 0),
 last_error_code VARCHAR(128) NOT NULL DEFAULT '', download_key VARCHAR(255) NOT NULL DEFAULT '', expires_at TIMESTAMPTZ(6), finished_at TIMESTAMPTZ(6),
 deleted_at TIMESTAMPTZ(6), created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
 CONSTRAINT uq_import_export_scope_idempotency UNIQUE (tenant_id, org_id, idempotency_key), CONSTRAINT chk_import_export_progress CHECK (processed_rows <= total_rows)
);
CREATE INDEX IF NOT EXISTS idx_import_export_scope_status ON import_export_jobs (tenant_id, org_id, kind, status, created_at);
CREATE INDEX IF NOT EXISTS idx_import_export_queue_task ON import_export_jobs (queue_task_id);

CREATE TABLE IF NOT EXISTS import_export_errors (
 id VARCHAR(64) PRIMARY KEY, job_id VARCHAR(64) NOT NULL, row_number INTEGER NOT NULL CHECK (row_number > 0), column_name VARCHAR(128) NOT NULL DEFAULT '', code VARCHAR(128) NOT NULL, message_key VARCHAR(191) NOT NULL,
 deleted_at TIMESTAMPTZ(6), created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_import_export_errors_job_row ON import_export_errors (job_id, row_number, created_at);

CREATE TABLE IF NOT EXISTS import_export_artifacts (
 id VARCHAR(64) PRIMARY KEY, job_id VARCHAR(64) NOT NULL, object_key VARCHAR(255) NOT NULL, sha256 CHAR(64) NOT NULL, size_bytes BIGINT NOT NULL DEFAULT 0 CHECK (size_bytes >= 0), expires_at TIMESTAMPTZ(6),
 deleted_at TIMESTAMPTZ(6), created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
 CONSTRAINT uq_import_export_artifact_job UNIQUE (job_id)
);
CREATE INDEX IF NOT EXISTS idx_import_export_artifact_expiry ON import_export_artifacts (expires_at);
