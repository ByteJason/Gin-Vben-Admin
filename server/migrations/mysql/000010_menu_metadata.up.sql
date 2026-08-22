ALTER TABLE menus
  DROP INDEX uq_menus_path,
  ADD UNIQUE KEY uq_menus_tenant_path (tenant_id, path),
  ADD COLUMN menu_type VARCHAR(32) NOT NULL DEFAULT 'directory' AFTER path,
  ADD COLUMN component VARCHAR(255) NULL AFTER menu_type,
  ADD COLUMN redirect VARCHAR(255) NULL AFTER component,
  ADD COLUMN icon VARCHAR(191) NULL AFTER redirect,
  ADD COLUMN permission VARCHAR(191) NULL AFTER icon,
  ADD COLUMN keep_alive BOOLEAN NOT NULL DEFAULT FALSE AFTER visible,
  ADD COLUMN external BOOLEAN NOT NULL DEFAULT FALSE AFTER keep_alive,
  ADD KEY idx_menus_tenant_parent_order (tenant_id, org_id, parent_id, sort_order),
  ADD CONSTRAINT chk_menus_type CHECK (menu_type IN ('directory', 'menu', 'button'));
