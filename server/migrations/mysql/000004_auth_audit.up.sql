ALTER TABLE auth_sessions
  ADD COLUMN device_id VARCHAR(128) NOT NULL DEFAULT '',
  ADD COLUMN device_name VARCHAR(191) NOT NULL DEFAULT '',
  ADD COLUMN ip_address VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN user_agent VARCHAR(512) NOT NULL DEFAULT '';

CREATE INDEX idx_auth_sessions_user_created ON auth_sessions (user_id, created_at);

CREATE TABLE auth_audit_events (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id BIGINT UNSIGNED NULL,
  session_id VARCHAR(64) NOT NULL DEFAULT '',
  event_type VARCHAR(64) NOT NULL,
  outcome VARCHAR(32) NOT NULL,
  request_id VARCHAR(128) NOT NULL DEFAULT '',
  ip_address VARCHAR(64) NOT NULL DEFAULT '',
  user_agent VARCHAR(512) NOT NULL DEFAULT '',
  metadata JSON NULL,
  created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  KEY idx_auth_audit_user_created (user_id, created_at),
  KEY idx_auth_audit_type_created (event_type, created_at),
  CONSTRAINT fk_auth_audit_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
