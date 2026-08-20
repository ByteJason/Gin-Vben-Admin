CREATE TABLE IF NOT EXISTS roles (
  id VARCHAR(64) NOT NULL,
  name VARCHAR(191) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  data_scope VARCHAR(32) NOT NULL DEFAULT 'own',
  created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  UNIQUE KEY uq_roles_name (name),
  CONSTRAINT chk_roles_status CHECK (status IN ('active', 'disabled')),
  CONSTRAINT chk_roles_scope CHECK (data_scope IN ('all', 'own', 'org', 'custom'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS user_roles (
  user_id BIGINT UNSIGNED NOT NULL,
  role_id VARCHAR(64) NOT NULL,
  created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (user_id, role_id),
  CONSTRAINT fk_user_roles_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_user_roles_role FOREIGN KEY (role_id) REFERENCES roles (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS menus (
  id VARCHAR(64) NOT NULL,
  parent_id VARCHAR(64) NULL,
  name VARCHAR(191) NOT NULL,
  path VARCHAR(255) NOT NULL,
  visible BOOLEAN NOT NULL DEFAULT TRUE,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  sort_order INT NOT NULL DEFAULT 0,
  created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  UNIQUE KEY uq_menus_path (path),
  KEY idx_menus_parent_order (parent_id, sort_order),
  CONSTRAINT chk_menus_status CHECK (status IN ('active', 'disabled'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS permissions (
  id VARCHAR(64) NOT NULL,
  name VARCHAR(191) NOT NULL,
  method VARCHAR(16) NOT NULL,
  path VARCHAR(255) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  UNIQUE KEY uq_permissions_method_path (method, path),
  CONSTRAINT chk_permissions_status CHECK (status IN ('active', 'disabled'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS iam_policies (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id BIGINT UNSIGNED NULL,
  role_id VARCHAR(64) NULL,
  domain VARCHAR(191) NOT NULL DEFAULT '',
  method VARCHAR(16) NOT NULL,
  path VARCHAR(255) NOT NULL,
  effect VARCHAR(16) NOT NULL DEFAULT 'deny',
  created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  KEY idx_iam_policies_user_match (user_id, domain, method, path),
  KEY idx_iam_policies_role_match (role_id, domain, method, path),
  CONSTRAINT fk_iam_policies_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_iam_policies_role FOREIGN KEY (role_id) REFERENCES roles (id) ON DELETE CASCADE,
  CONSTRAINT chk_iam_policies_subject CHECK (user_id IS NOT NULL OR role_id IS NOT NULL),
  CONSTRAINT chk_iam_policies_effect CHECK (effect IN ('allow', 'deny'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS iam_data_scopes (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id BIGINT UNSIGNED NULL,
  role_id VARCHAR(64) NULL,
  domain VARCHAR(191) NOT NULL DEFAULT '',
  resource VARCHAR(191) NOT NULL,
  scope VARCHAR(32) NOT NULL,
  ids JSON NOT NULL,
  created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  KEY idx_iam_scopes_user (user_id, domain, resource),
  KEY idx_iam_scopes_role (role_id, domain, resource),
  CONSTRAINT fk_iam_scopes_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_iam_scopes_role FOREIGN KEY (role_id) REFERENCES roles (id) ON DELETE CASCADE,
  CONSTRAINT chk_iam_scopes_subject CHECK (user_id IS NOT NULL OR role_id IS NOT NULL),
  CONSTRAINT chk_iam_scopes_scope CHECK (scope IN ('all', 'own', 'org', 'custom'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
