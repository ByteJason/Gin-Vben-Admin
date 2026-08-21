CREATE TABLE IF NOT EXISTS setting_versions (
  id BIGSERIAL PRIMARY KEY,
  key VARCHAR(191) NOT NULL,
  value JSONB NOT NULL,
  version BIGINT NOT NULL,
  sensitive BOOLEAN NOT NULL DEFAULT FALSE,
  updated_by VARCHAR(191) NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  CONSTRAINT uq_setting_versions_key_version UNIQUE (key, version)
);

CREATE INDEX IF NOT EXISTS idx_setting_versions_key_updated ON setting_versions (key, updated_at);
