ALTER TABLE menus
  DROP CHECK chk_menus_type,
  DROP INDEX idx_menus_tenant_parent_order,
  DROP COLUMN external,
  DROP COLUMN keep_alive,
  DROP COLUMN permission,
  DROP COLUMN icon,
  DROP COLUMN redirect,
  DROP COLUMN component,
  DROP COLUMN menu_type,
  DROP INDEX uq_menus_tenant_path,
  ADD UNIQUE KEY uq_menus_path (path);
