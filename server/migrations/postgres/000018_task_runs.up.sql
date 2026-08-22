CREATE TABLE IF NOT EXISTS task_runs (
 id VARCHAR(64) PRIMARY KEY, task_id VARCHAR(64) NOT NULL, tenant_id VARCHAR(128) NOT NULL, org_id VARCHAR(128) NOT NULL DEFAULT '',
 queue_task_id VARCHAR(64) NOT NULL DEFAULT '', idempotency_key VARCHAR(191) NOT NULL,
 status VARCHAR(32) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','running','succeeded','failed','dead_letter','cancelled')),
 payload_digest CHAR(64) NOT NULL, attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0), max_attempts INTEGER NOT NULL DEFAULT 1 CHECK (max_attempts > 0),
 last_error_code VARCHAR(128) NOT NULL DEFAULT '', started_at TIMESTAMPTZ(6), finished_at TIMESTAMPTZ(6),
 deleted_at TIMESTAMPTZ(6), created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
 CONSTRAINT uq_task_runs_scope_idempotency UNIQUE (tenant_id, org_id, idempotency_key)
);
CREATE INDEX IF NOT EXISTS idx_task_runs_scope_task_status ON task_runs (tenant_id, org_id, task_id, status, created_at);
CREATE INDEX IF NOT EXISTS idx_task_runs_queue_task ON task_runs (queue_task_id);

CREATE TABLE IF NOT EXISTS task_run_logs (
 id VARCHAR(64) PRIMARY KEY, run_id VARCHAR(64) NOT NULL, attempt INTEGER NOT NULL DEFAULT 0,
 status VARCHAR(32) NOT NULL CHECK (status IN ('pending','running','succeeded','failed','dead_letter','cancelled')),
 error_code VARCHAR(128) NOT NULL DEFAULT '', message VARCHAR(512) NOT NULL DEFAULT '',
 deleted_at TIMESTAMPTZ(6), created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_task_run_logs_run_created ON task_run_logs (run_id, created_at);
