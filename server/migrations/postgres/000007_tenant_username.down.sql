ALTER TABLE users
  DROP CONSTRAINT IF EXISTS uq_users_tenant_username,
  ADD CONSTRAINT users_username_key UNIQUE (username);
