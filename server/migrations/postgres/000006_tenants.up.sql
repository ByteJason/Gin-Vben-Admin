CREATE TABLE IF NOT EXISTS tenants (
  id VARCHAR(64) PRIMARY KEY,
  name VARCHAR(191) NOT NULL UNIQUE,
  status VARCHAR(32) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO tenants (id, name, status) VALUES ('default', 'Default tenant', 'active')
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS organizations (
  id VARCHAR(64) PRIMARY KEY,
  tenant_id VARCHAR(64) NOT NULL REFERENCES tenants (id),
  parent_id VARCHAR(64) NULL,
  name VARCHAR(191) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT uq_organizations_tenant_name UNIQUE (tenant_id, name)
);
CREATE INDEX IF NOT EXISTS idx_organizations_tenant_parent ON organizations (tenant_id, parent_id);

INSERT INTO organizations (id, tenant_id, parent_id, name, status)
VALUES ('default-org', 'default', NULL, 'Default organization', 'active')
ON CONFLICT (id) DO NOTHING;

ALTER TABLE users
  ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT 'default' REFERENCES tenants (id),
  ADD COLUMN org_id VARCHAR(64) NULL;
CREATE INDEX IF NOT EXISTS idx_users_tenant_org ON users (tenant_id, org_id);

ALTER TABLE user_roles
  ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT 'default' REFERENCES tenants (id),
  ADD COLUMN org_id VARCHAR(64) NULL;
CREATE INDEX IF NOT EXISTS idx_user_roles_tenant ON user_roles (tenant_id);

ALTER TABLE roles
  ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT 'default' REFERENCES tenants (id),
  ADD COLUMN org_id VARCHAR(64) NULL;
CREATE INDEX IF NOT EXISTS idx_roles_tenant_org ON roles (tenant_id, org_id);

ALTER TABLE menus
  ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT 'default' REFERENCES tenants (id),
  ADD COLUMN org_id VARCHAR(64) NULL;
CREATE INDEX IF NOT EXISTS idx_menus_tenant_org ON menus (tenant_id, org_id);

ALTER TABLE permissions
  ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT 'default' REFERENCES tenants (id),
  ADD COLUMN org_id VARCHAR(64) NULL;
CREATE INDEX IF NOT EXISTS idx_permissions_tenant_org ON permissions (tenant_id, org_id);

ALTER TABLE iam_policies
  ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT 'default' REFERENCES tenants (id),
  ADD COLUMN org_id VARCHAR(64) NULL;
CREATE INDEX IF NOT EXISTS idx_iam_policies_tenant ON iam_policies (tenant_id, org_id);

ALTER TABLE iam_data_scopes
  ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT 'default' REFERENCES tenants (id),
  ADD COLUMN org_id VARCHAR(64) NULL;
CREATE INDEX IF NOT EXISTS idx_iam_scopes_tenant ON iam_data_scopes (tenant_id, org_id);

ALTER TABLE auth_sessions
  ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT 'default' REFERENCES tenants (id),
  ADD COLUMN org_id VARCHAR(64) NULL;
CREATE INDEX IF NOT EXISTS idx_auth_sessions_tenant ON auth_sessions (tenant_id, org_id);

ALTER TABLE auth_audit_events
  ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT 'default' REFERENCES tenants (id),
  ADD COLUMN org_id VARCHAR(64) NULL;
CREATE INDEX IF NOT EXISTS idx_auth_audit_tenant ON auth_audit_events (tenant_id, org_id);

ALTER TABLE setting_versions
  ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT 'default' REFERENCES tenants (id),
  ADD COLUMN org_id VARCHAR(64) NULL;
CREATE INDEX IF NOT EXISTS idx_setting_versions_tenant ON setting_versions (tenant_id, org_id);
