CREATE TABLE IF NOT EXISTS app_metadata (
  metadata_key VARCHAR(191) PRIMARY KEY,
  metadata_value JSONB NOT NULL,
  version BIGINT NOT NULL CHECK (version > 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO app_metadata (metadata_key, metadata_value, version)
VALUES ('product', '{"name":"gin-vben-admin"}'::jsonb, 1)
ON CONFLICT (metadata_key) DO NOTHING;
