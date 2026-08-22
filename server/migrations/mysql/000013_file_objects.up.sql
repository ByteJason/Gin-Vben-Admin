CREATE TABLE IF NOT EXISTS file_objects (
  id VARCHAR(64) NOT NULL,
  tenant_id VARCHAR(128) NOT NULL,
  org_id VARCHAR(128) NULL,
  object_key VARCHAR(255) NOT NULL,
  name VARCHAR(255) NOT NULL,
  mime VARCHAR(191) NOT NULL,
  size BIGINT NOT NULL,
  sha256 CHAR(64) NULL,
  owner_id VARCHAR(128) NOT NULL,
  acl VARCHAR(16) NOT NULL DEFAULT 'private',
  created_at DATETIME(6) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_file_objects_tenant_key (tenant_id, object_key),
  KEY idx_file_objects_tenant_created (tenant_id, created_at),
  KEY idx_file_objects_cleanup (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
