ALTER TABLE users
  DROP INDEX uq_users_tenant_username,
  ADD UNIQUE KEY uq_users_username (username);
