ALTER TABLE users DROP CONSTRAINT IF EXISTS chk_users_phone_e164;
DROP INDEX IF EXISTS uq_users_tenant_email_normalized;
DROP INDEX IF EXISTS uq_users_tenant_username_normalized;
DROP INDEX IF EXISTS idx_users_tenant_status_created;
ALTER TABLE users
  DROP COLUMN IF EXISTS password_changed_at,
  DROP COLUMN IF EXISTS last_login_at,
  DROP COLUMN IF EXISTS last_login_ip,
  DROP COLUMN IF EXISTS phone,
  DROP COLUMN IF EXISTS avatar,
  DROP COLUMN IF EXISTS nickname,
  DROP COLUMN IF EXISTS email_normalized,
  DROP COLUMN IF EXISTS email,
  DROP COLUMN IF EXISTS username_normalized;
