DROP TABLE IF EXISTS auth_audit_events;
DROP INDEX idx_auth_sessions_user_created ON auth_sessions;
ALTER TABLE auth_sessions
  DROP COLUMN device_id,
  DROP COLUMN device_name,
  DROP COLUMN ip_address,
  DROP COLUMN user_agent;
