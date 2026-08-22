CREATE TABLE IF NOT EXISTS task_definitions (
 id VARCHAR(64) NOT NULL, tenant_id VARCHAR(128) NOT NULL, org_id VARCHAR(128) NOT NULL DEFAULT '', name VARCHAR(191) NOT NULL,
 type VARCHAR(32) NOT NULL, payload_schema JSON NOT NULL, cron VARCHAR(128) NOT NULL DEFAULT '', timezone VARCHAR(64) NOT NULL DEFAULT 'UTC', enabled BOOLEAN NOT NULL DEFAULT TRUE,
 concurrency INT NOT NULL DEFAULT 1, concurrency_policy VARCHAR(16) NOT NULL DEFAULT 'forbid', timeout_ms BIGINT UNSIGNED NOT NULL DEFAULT 30000, max_attempts INT UNSIGNED NOT NULL DEFAULT 1, idempotency_key VARCHAR(191) NOT NULL DEFAULT '',
 deleted_at TIMESTAMP(6) NULL, created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
 PRIMARY KEY (id), UNIQUE KEY uq_task_definitions_scope_name (tenant_id, org_id, name), KEY idx_task_definitions_scope_enabled (tenant_id, org_id, enabled),
 CONSTRAINT chk_task_definitions_type CHECK (type IN ('manual','http','webhook')), CONSTRAINT chk_task_definitions_concurrency CHECK (concurrency > 0), CONSTRAINT chk_task_definitions_policy CHECK (concurrency_policy IN ('allow','forbid','replace')), CONSTRAINT chk_task_definitions_attempts CHECK (max_attempts > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
