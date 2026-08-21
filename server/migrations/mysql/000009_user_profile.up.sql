ALTER TABLE users
  ADD COLUMN username_normalized VARCHAR(191) NULL,
  ADD COLUMN email VARCHAR(254) NULL,
  ADD COLUMN email_normalized VARCHAR(254) NULL,
  ADD COLUMN nickname VARCHAR(191) NOT NULL DEFAULT '',
  ADD COLUMN avatar VARCHAR(512) NOT NULL DEFAULT '',
  ADD COLUMN phone VARCHAR(32) NULL,
  ADD COLUMN last_login_ip VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN last_login_at TIMESTAMP(6) NULL,
  ADD COLUMN password_changed_at TIMESTAMP(6) NULL,
  ADD KEY idx_users_tenant_status_created (tenant_id, status, created_at);

UPDATE users
SET username_normalized = LOWER(TRIM(username))
WHERE username_normalized IS NULL;

UPDATE users
SET email_normalized = LOWER(TRIM(email))
WHERE email IS NOT NULL AND email_normalized IS NULL;

ALTER TABLE users
  MODIFY COLUMN username_normalized VARCHAR(191) NOT NULL,
  ADD UNIQUE KEY uq_users_tenant_username_normalized (tenant_id, username_normalized),
  ADD UNIQUE KEY uq_users_tenant_email_normalized (tenant_id, email_normalized),
  ADD CONSTRAINT chk_users_phone_e164 CHECK (phone IS NULL OR phone REGEXP '^\+[1-9][0-9]{7,14}$');
