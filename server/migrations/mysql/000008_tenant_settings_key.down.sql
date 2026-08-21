ALTER TABLE setting_versions
  DROP INDEX uq_setting_versions_tenant_key_version,
  DROP INDEX idx_setting_versions_tenant_key_updated,
  ADD UNIQUE KEY uq_setting_versions_key_version (`key`, version);
