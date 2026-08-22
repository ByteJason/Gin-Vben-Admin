CREATE TABLE IF NOT EXISTS task_runs (
 id VARCHAR(64) NOT NULL, task_id VARCHAR(64) NOT NULL, tenant_id VARCHAR(128) NOT NULL, org_id VARCHAR(128) NOT NULL DEFAULT '',
 queue_task_id VARCHAR(64) NOT NULL DEFAULT '', idempotency_key VARCHAR(191) NOT NULL, status VARCHAR(32) NOT NULL DEFAULT 'pending',
 payload_digest CHAR(64) NOT NULL, attempt_count INT UNSIGNED NOT NULL DEFAULT 0, max_attempts INT UNSIGNED NOT NULL DEFAULT 1,
 last_error_code VARCHAR(128) NOT NULL DEFAULT '', started_at TIMESTAMP(6) NULL, finished_at TIMESTAMP(6) NULL,
 deleted_at TIMESTAMP(6) NULL, created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
 PRIMARY KEY (id), UNIQUE KEY uq_task_runs_scope_idempotency (tenant_id, org_id, idempotency_key),
 KEY idx_task_runs_scope_task_status (tenant_id, org_id, task_id, status, created_at), KEY idx_task_runs_queue_task (queue_task_id),
 CONSTRAINT chk_task_runs_status CHECK (status IN ('pending','running','succeeded','failed','dead_letter','cancelled')),
 CONSTRAINT chk_task_runs_attempts CHECK (attempt_count >= 0 AND max_attempts > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS task_run_logs (
 id VARCHAR(64) NOT NULL, run_id VARCHAR(64) NOT NULL, attempt INT UNSIGNED NOT NULL DEFAULT 0, status VARCHAR(32) NOT NULL,
 error_code VARCHAR(128) NOT NULL DEFAULT '', message VARCHAR(512) NOT NULL DEFAULT '',
 deleted_at TIMESTAMP(6) NULL, created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
 PRIMARY KEY (id), KEY idx_task_run_logs_run_created (run_id, created_at),
 CONSTRAINT chk_task_run_logs_status CHECK (status IN ('pending','running','succeeded','failed','dead_letter','cancelled'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
