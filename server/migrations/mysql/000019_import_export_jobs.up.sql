CREATE TABLE IF NOT EXISTS import_export_jobs (
 id VARCHAR(64) NOT NULL, kind VARCHAR(16) NOT NULL, tenant_id VARCHAR(128) NOT NULL, org_id VARCHAR(128) NOT NULL DEFAULT '', actor_id VARCHAR(128) NOT NULL DEFAULT '',
 preview_id VARCHAR(64) NOT NULL DEFAULT '', queue_task_id VARCHAR(64) NOT NULL DEFAULT '', idempotency_key VARCHAR(191) NOT NULL,
 status VARCHAR(32) NOT NULL DEFAULT 'pending', format VARCHAR(16) NOT NULL DEFAULT 'csv', total_rows INT UNSIGNED NOT NULL DEFAULT 0, processed_rows INT UNSIGNED NOT NULL DEFAULT 0, error_count INT UNSIGNED NOT NULL DEFAULT 0,
 last_error_code VARCHAR(128) NOT NULL DEFAULT '', download_key VARCHAR(255) NOT NULL DEFAULT '', expires_at TIMESTAMP(6) NULL, finished_at TIMESTAMP(6) NULL,
 deleted_at TIMESTAMP(6) NULL, created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
 PRIMARY KEY (id), UNIQUE KEY uq_import_export_scope_idempotency (tenant_id, org_id, idempotency_key),
 KEY idx_import_export_scope_status (tenant_id, org_id, kind, status, created_at), KEY idx_import_export_queue_task (queue_task_id),
 CONSTRAINT chk_import_export_kind CHECK (kind IN ('import','export')),
 CONSTRAINT chk_import_export_status CHECK (status IN ('pending','running','succeeded','failed','cancelled')),
 CONSTRAINT chk_import_export_progress CHECK (processed_rows <= total_rows)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS import_export_errors (
 id VARCHAR(64) NOT NULL, job_id VARCHAR(64) NOT NULL, row_number INT UNSIGNED NOT NULL, column_name VARCHAR(128) NOT NULL DEFAULT '', code VARCHAR(128) NOT NULL, message_key VARCHAR(191) NOT NULL,
 deleted_at TIMESTAMP(6) NULL, created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
 PRIMARY KEY (id), KEY idx_import_export_errors_job_row (job_id, row_number, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS import_export_artifacts (
 id VARCHAR(64) NOT NULL, job_id VARCHAR(64) NOT NULL, object_key VARCHAR(255) NOT NULL, sha256 CHAR(64) NOT NULL, size_bytes BIGINT UNSIGNED NOT NULL DEFAULT 0, expires_at TIMESTAMP(6) NULL,
 deleted_at TIMESTAMP(6) NULL, created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
 PRIMARY KEY (id), UNIQUE KEY uq_import_export_artifact_job (job_id), KEY idx_import_export_artifact_expiry (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
