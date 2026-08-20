ALTER TABLE auth_sessions
  ADD COLUMN IF NOT EXISTS device_id VARCHAR(128) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS device_name VARCHAR(191) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS ip_address VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS user_agent VARCHAR(512) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_auth_sessions_user_created ON auth_sessions (user_id, created_at);

CREATE TABLE IF NOT EXISTS auth_audit_events (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NULL REFERENCES users (id) ON DELETE SET NULL,
  session_id VARCHAR(64) NOT NULL DEFAULT '',
  event_type VARCHAR(64) NOT NULL,
  outcome VARCHAR(32) NOT NULL,
  request_id VARCHAR(128) NOT NULL DEFAULT '',
  ip_address VARCHAR(64) NOT NULL DEFAULT '',
  user_agent VARCHAR(512) NOT NULL DEFAULT '',
  metadata JSONB NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_auth_audit_user_created ON auth_audit_events (user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_auth_audit_type_created ON auth_audit_events (event_type, created_at);
