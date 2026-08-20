DROP TABLE IF EXISTS auth_audit_events;
DROP INDEX IF EXISTS idx_auth_sessions_user_created;
ALTER TABLE auth_sessions
  DROP COLUMN IF EXISTS device_id,
  DROP COLUMN IF EXISTS device_name,
  DROP COLUMN IF EXISTS ip_address,
  DROP COLUMN IF EXISTS user_agent;
