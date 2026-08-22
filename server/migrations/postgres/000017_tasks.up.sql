CREATE TABLE IF NOT EXISTS task_definitions (
 id VARCHAR(64) PRIMARY KEY, tenant_id VARCHAR(128) NOT NULL, org_id VARCHAR(128) NOT NULL DEFAULT '', name VARCHAR(191) NOT NULL,
 type VARCHAR(32) NOT NULL CHECK (type IN ('manual','http','webhook')), payload_schema JSONB NOT NULL, cron VARCHAR(128) NOT NULL DEFAULT '', timezone VARCHAR(64) NOT NULL DEFAULT 'UTC', enabled BOOLEAN NOT NULL DEFAULT TRUE,
 concurrency INTEGER NOT NULL DEFAULT 1 CHECK (concurrency > 0), concurrency_policy VARCHAR(16) NOT NULL DEFAULT 'forbid' CHECK (concurrency_policy IN ('allow','forbid','replace')), timeout_ms BIGINT NOT NULL DEFAULT 30000, max_attempts INTEGER NOT NULL DEFAULT 1 CHECK (max_attempts > 0), idempotency_key VARCHAR(191) NOT NULL DEFAULT '',
 deleted_at TIMESTAMPTZ(6), created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
 CONSTRAINT uq_task_definitions_scope_name UNIQUE (tenant_id, org_id, name)
);
CREATE INDEX IF NOT EXISTS idx_task_definitions_scope_enabled ON task_definitions (tenant_id, org_id, enabled);
