ALTER TABLE setting_versions
  DROP COLUMN IF EXISTS source,
  DROP COLUMN IF EXISTS encrypted;
