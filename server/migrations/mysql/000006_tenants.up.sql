CREATE TABLE IF NOT EXISTS tenants (
  id VARCHAR(64) NOT NULL,
  name VARCHAR(191) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  UNIQUE KEY uq_tenants_name (name),
  CONSTRAINT chk_tenants_status CHECK (status IN ('active', 'disabled'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT IGNORE INTO tenants (id, name, status) VALUES ('default', 'Default tenant', 'active');

CREATE TABLE IF NOT EXISTS organizations (
  id VARCHAR(64) NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  parent_id VARCHAR(64) NULL,
  name VARCHAR(191) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  UNIQUE KEY uq_organizations_tenant_name (tenant_id, name),
  KEY idx_organizations_tenant_parent (tenant_id, parent_id),
  CONSTRAINT fk_organizations_tenant FOREIGN KEY (tenant_id) REFERENCES tenants (id),
  CONSTRAINT chk_organizations_status CHECK (status IN ('active', 'disabled'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT IGNORE INTO organizations (id, tenant_id, parent_id, name, status)
VALUES ('default-org', 'default', NULL, 'Default organization', 'active');

ALTER TABLE users
  ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
  ADD COLUMN org_id VARCHAR(64) NULL,
  ADD KEY idx_users_tenant_org (tenant_id, org_id),
  ADD CONSTRAINT fk_users_tenant FOREIGN KEY (tenant_id) REFERENCES tenants (id);

ALTER TABLE user_roles
  ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
  ADD COLUMN org_id VARCHAR(64) NULL,
  ADD KEY idx_user_roles_tenant (tenant_id),
  ADD CONSTRAINT fk_user_roles_tenant FOREIGN KEY (tenant_id) REFERENCES tenants (id);

ALTER TABLE roles
  ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
  ADD COLUMN org_id VARCHAR(64) NULL,
  ADD KEY idx_roles_tenant_org (tenant_id, org_id),
  ADD CONSTRAINT fk_roles_tenant FOREIGN KEY (tenant_id) REFERENCES tenants (id);

ALTER TABLE menus
  ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
  ADD COLUMN org_id VARCHAR(64) NULL,
  ADD KEY idx_menus_tenant_org (tenant_id, org_id),
  ADD CONSTRAINT fk_menus_tenant FOREIGN KEY (tenant_id) REFERENCES tenants (id);

ALTER TABLE permissions
  ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
  ADD COLUMN org_id VARCHAR(64) NULL,
  ADD KEY idx_permissions_tenant_org (tenant_id, org_id),
  ADD CONSTRAINT fk_permissions_tenant FOREIGN KEY (tenant_id) REFERENCES tenants (id);

ALTER TABLE iam_policies
  ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
  ADD COLUMN org_id VARCHAR(64) NULL,
  ADD KEY idx_iam_policies_tenant (tenant_id, org_id),
  ADD CONSTRAINT fk_iam_policies_tenant FOREIGN KEY (tenant_id) REFERENCES tenants (id);

ALTER TABLE iam_data_scopes
  ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
  ADD COLUMN org_id VARCHAR(64) NULL,
  ADD KEY idx_iam_scopes_tenant (tenant_id, org_id),
  ADD CONSTRAINT fk_iam_scopes_tenant FOREIGN KEY (tenant_id) REFERENCES tenants (id);

ALTER TABLE auth_sessions
  ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
  ADD COLUMN org_id VARCHAR(64) NULL,
  ADD KEY idx_auth_sessions_tenant (tenant_id, org_id),
  ADD CONSTRAINT fk_auth_sessions_tenant FOREIGN KEY (tenant_id) REFERENCES tenants (id);

ALTER TABLE auth_audit_events
  ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
  ADD COLUMN org_id VARCHAR(64) NULL,
  ADD KEY idx_auth_audit_tenant (tenant_id, org_id),
  ADD CONSTRAINT fk_auth_audit_tenant FOREIGN KEY (tenant_id) REFERENCES tenants (id);

ALTER TABLE setting_versions
  ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
  ADD COLUMN org_id VARCHAR(64) NULL,
  ADD KEY idx_setting_versions_tenant (tenant_id, org_id),
  ADD CONSTRAINT fk_setting_versions_tenant FOREIGN KEY (tenant_id) REFERENCES tenants (id);
