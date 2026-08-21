ALTER TABLE setting_versions DROP CONSTRAINT IF EXISTS uq_setting_versions_tenant_key_version;
DROP INDEX IF EXISTS idx_setting_versions_tenant_key_updated;
ALTER TABLE setting_versions
  ADD CONSTRAINT uq_setting_versions_key_version UNIQUE ("key", version);
