ALTER TABLE setting_versions DROP CONSTRAINT IF EXISTS uq_setting_versions_key_version;
ALTER TABLE setting_versions
  ADD CONSTRAINT uq_setting_versions_tenant_key_version UNIQUE (tenant_id, "key", version);
CREATE INDEX IF NOT EXISTS idx_setting_versions_tenant_key_updated ON setting_versions (tenant_id, "key", updated_at);
