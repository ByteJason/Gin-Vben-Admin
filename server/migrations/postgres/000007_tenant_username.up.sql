ALTER TABLE users
  DROP CONSTRAINT IF EXISTS users_username_key,
  ADD CONSTRAINT uq_users_tenant_username UNIQUE (tenant_id, username);
