ALTER TABLE setting_versions
  ADD COLUMN encrypted BOOLEAN NOT NULL DEFAULT FALSE AFTER sensitive,
  ADD COLUMN source VARCHAR(16) NOT NULL DEFAULT 'database' AFTER encrypted;
