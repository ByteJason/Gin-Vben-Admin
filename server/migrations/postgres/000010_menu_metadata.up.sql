ALTER TABLE menus DROP CONSTRAINT IF EXISTS menus_path_key;
CREATE UNIQUE INDEX IF NOT EXISTS uq_menus_tenant_path ON menus (tenant_id, path);
ALTER TABLE menus
  ADD COLUMN menu_type VARCHAR(32) NOT NULL DEFAULT 'directory' CHECK (menu_type IN ('directory', 'menu', 'button')),
  ADD COLUMN component VARCHAR(255),
  ADD COLUMN redirect VARCHAR(255),
  ADD COLUMN icon VARCHAR(191),
  ADD COLUMN permission VARCHAR(191),
  ADD COLUMN keep_alive BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN external BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX IF NOT EXISTS idx_menus_tenant_parent_order ON menus (tenant_id, org_id, parent_id, sort_order);
