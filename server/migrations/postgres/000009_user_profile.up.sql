ALTER TABLE users
  ADD COLUMN IF NOT EXISTS username_normalized VARCHAR(191),
  ADD COLUMN IF NOT EXISTS email VARCHAR(254),
  ADD COLUMN IF NOT EXISTS email_normalized VARCHAR(254),
  ADD COLUMN IF NOT EXISTS nickname VARCHAR(191) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS avatar VARCHAR(512) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS phone VARCHAR(32),
  ADD COLUMN IF NOT EXISTS last_login_ip VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS last_login_at TIMESTAMPTZ NULL,
  ADD COLUMN IF NOT EXISTS password_changed_at TIMESTAMPTZ NULL;

UPDATE users
SET username_normalized = LOWER(BTRIM(username))
WHERE username_normalized IS NULL;

UPDATE users
SET email_normalized = LOWER(BTRIM(email))
WHERE email IS NOT NULL AND email_normalized IS NULL;

ALTER TABLE users
  ALTER COLUMN username_normalized SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_users_tenant_status_created ON users (tenant_id, status, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS uq_users_tenant_username_normalized ON users (tenant_id, username_normalized);
CREATE UNIQUE INDEX IF NOT EXISTS uq_users_tenant_email_normalized ON users (tenant_id, email_normalized) WHERE email_normalized IS NOT NULL;
ALTER TABLE users
  ADD CONSTRAINT chk_users_phone_e164 CHECK (phone IS NULL OR phone ~ '^\+[1-9][0-9]{7,14}$');
