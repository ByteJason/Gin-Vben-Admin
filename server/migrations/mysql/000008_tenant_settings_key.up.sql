ALTER TABLE setting_versions
  DROP INDEX uq_setting_versions_key_version,
  ADD UNIQUE KEY uq_setting_versions_tenant_key_version (tenant_id, `key`, version),
  ADD KEY idx_setting_versions_tenant_key_updated (tenant_id, `key`, updated_at);
