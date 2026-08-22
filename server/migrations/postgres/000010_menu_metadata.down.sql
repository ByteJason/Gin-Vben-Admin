DROP INDEX IF EXISTS idx_menus_tenant_parent_order;
DROP INDEX IF EXISTS uq_menus_tenant_path;
ALTER TABLE menus
  DROP COLUMN IF EXISTS external,
  DROP COLUMN IF EXISTS keep_alive,
  DROP COLUMN IF EXISTS permission,
  DROP COLUMN IF EXISTS icon,
  DROP COLUMN IF EXISTS redirect,
  DROP COLUMN IF EXISTS component,
  DROP COLUMN IF EXISTS menu_type;
ALTER TABLE menus ADD CONSTRAINT menus_path_key UNIQUE (path);
