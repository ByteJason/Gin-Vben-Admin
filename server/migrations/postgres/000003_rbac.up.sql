CREATE TABLE IF NOT EXISTS roles (
  id VARCHAR(64) PRIMARY KEY,
  name VARCHAR(191) NOT NULL UNIQUE,
  status VARCHAR(32) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
  data_scope VARCHAR(32) NOT NULL DEFAULT 'own' CHECK (data_scope IN ('all', 'own', 'org', 'custom')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS user_roles (
  user_id BIGINT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
  role_id VARCHAR(64) NOT NULL REFERENCES roles (id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (user_id, role_id)
);

CREATE TABLE IF NOT EXISTS menus (
  id VARCHAR(64) PRIMARY KEY,
  parent_id VARCHAR(64) NULL,
  name VARCHAR(191) NOT NULL,
  path VARCHAR(255) NOT NULL UNIQUE,
  visible BOOLEAN NOT NULL DEFAULT TRUE,
  status VARCHAR(32) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
  sort_order INTEGER NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_menus_parent_order ON menus (parent_id, sort_order);

CREATE TABLE IF NOT EXISTS permissions (
  id VARCHAR(64) PRIMARY KEY,
  name VARCHAR(191) NOT NULL,
  method VARCHAR(16) NOT NULL,
  path VARCHAR(255) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (method, path)
);

CREATE TABLE IF NOT EXISTS iam_policies (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NULL REFERENCES users (id) ON DELETE CASCADE,
  role_id VARCHAR(64) NULL REFERENCES roles (id) ON DELETE CASCADE,
  domain VARCHAR(191) NOT NULL DEFAULT '',
  method VARCHAR(16) NOT NULL,
  path VARCHAR(255) NOT NULL,
  effect VARCHAR(16) NOT NULL DEFAULT 'deny' CHECK (effect IN ('allow', 'deny')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CHECK (user_id IS NOT NULL OR role_id IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS idx_iam_policies_user_match ON iam_policies (user_id, domain, method, path);
CREATE INDEX IF NOT EXISTS idx_iam_policies_role_match ON iam_policies (role_id, domain, method, path);

CREATE TABLE IF NOT EXISTS iam_data_scopes (
  id BIGSERIAL PRIMARY KEY,
  user_id BIGINT NULL REFERENCES users (id) ON DELETE CASCADE,
  role_id VARCHAR(64) NULL REFERENCES roles (id) ON DELETE CASCADE,
  domain VARCHAR(191) NOT NULL DEFAULT '',
  resource VARCHAR(191) NOT NULL,
  scope VARCHAR(32) NOT NULL CHECK (scope IN ('all', 'own', 'org', 'custom')),
  ids JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CHECK (user_id IS NOT NULL OR role_id IS NOT NULL)
);
CREATE INDEX IF NOT EXISTS idx_iam_scopes_user ON iam_data_scopes (user_id, domain, resource);
CREATE INDEX IF NOT EXISTS idx_iam_scopes_role ON iam_data_scopes (role_id, domain, resource);
