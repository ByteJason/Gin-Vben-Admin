CREATE TABLE IF NOT EXISTS file_objects (
  id VARCHAR(64) PRIMARY KEY,
  tenant_id VARCHAR(128) NOT NULL,
  org_id VARCHAR(128),
  object_key VARCHAR(255) NOT NULL,
  name VARCHAR(255) NOT NULL,
  mime VARCHAR(191) NOT NULL,
  size BIGINT NOT NULL,
  sha256 CHAR(64),
  owner_id VARCHAR(128) NOT NULL,
  acl VARCHAR(16) NOT NULL DEFAULT 'private',
  created_at TIMESTAMPTZ NOT NULL,
  CONSTRAINT uq_file_objects_tenant_key UNIQUE (tenant_id, object_key)
);
CREATE INDEX IF NOT EXISTS idx_file_objects_tenant_created ON file_objects (tenant_id, created_at);
CREATE INDEX IF NOT EXISTS idx_file_objects_cleanup ON file_objects (created_at);
