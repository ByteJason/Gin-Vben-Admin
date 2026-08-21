ALTER TABLE setting_versions
  DROP FOREIGN KEY fk_setting_versions_tenant,
  DROP INDEX idx_setting_versions_tenant,
  DROP COLUMN org_id,
  DROP COLUMN tenant_id;

ALTER TABLE auth_audit_events
  DROP FOREIGN KEY fk_auth_audit_tenant,
  DROP INDEX idx_auth_audit_tenant,
  DROP COLUMN org_id,
  DROP COLUMN tenant_id;

ALTER TABLE auth_sessions
  DROP FOREIGN KEY fk_auth_sessions_tenant,
  DROP INDEX idx_auth_sessions_tenant,
  DROP COLUMN org_id,
  DROP COLUMN tenant_id;

ALTER TABLE iam_data_scopes
  DROP FOREIGN KEY fk_iam_scopes_tenant,
  DROP INDEX idx_iam_scopes_tenant,
  DROP COLUMN org_id,
  DROP COLUMN tenant_id;

ALTER TABLE iam_policies
  DROP FOREIGN KEY fk_iam_policies_tenant,
  DROP INDEX idx_iam_policies_tenant,
  DROP COLUMN org_id,
  DROP COLUMN tenant_id;

ALTER TABLE permissions
  DROP FOREIGN KEY fk_permissions_tenant,
  DROP INDEX idx_permissions_tenant_org,
  DROP COLUMN org_id,
  DROP COLUMN tenant_id;

ALTER TABLE menus
  DROP FOREIGN KEY fk_menus_tenant,
  DROP INDEX idx_menus_tenant_org,
  DROP COLUMN org_id,
  DROP COLUMN tenant_id;

ALTER TABLE roles
  DROP FOREIGN KEY fk_roles_tenant,
  DROP INDEX idx_roles_tenant_org,
  DROP COLUMN org_id,
  DROP COLUMN tenant_id;

ALTER TABLE user_roles
  DROP FOREIGN KEY fk_user_roles_tenant,
  DROP INDEX idx_user_roles_tenant,
  DROP COLUMN org_id,
  DROP COLUMN tenant_id;

ALTER TABLE users
  DROP FOREIGN KEY fk_users_tenant,
  DROP INDEX idx_users_tenant_org,
  DROP COLUMN org_id,
  DROP COLUMN tenant_id;

DROP TABLE IF EXISTS organizations;
DROP TABLE IF EXISTS tenants;
