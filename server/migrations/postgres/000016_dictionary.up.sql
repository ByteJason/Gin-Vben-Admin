CREATE TABLE IF NOT EXISTS dictionary_types (
  id VARCHAR(64) PRIMARY KEY,
  tenant_id VARCHAR(128) NOT NULL DEFAULT '',
  org_id VARCHAR(128) NOT NULL DEFAULT '',
  code VARCHAR(191) NOT NULL,
  name_zh_cn VARCHAR(191) NOT NULL DEFAULT '',
  name_en_us VARCHAR(191) NOT NULL DEFAULT '',
  description VARCHAR(512) NOT NULL DEFAULT '',
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  sort_order INTEGER NOT NULL DEFAULT 0,
  system_owned BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
  deleted_at TIMESTAMPTZ(6) NULL,
  CONSTRAINT uq_dictionary_types_scope_code UNIQUE (tenant_id, org_id, code),
  CONSTRAINT chk_dictionary_types_status CHECK (status IN ('active', 'disabled')),
  CONSTRAINT chk_dictionary_types_sort CHECK (sort_order >= 0)
);
CREATE INDEX IF NOT EXISTS idx_dictionary_types_scope_status ON dictionary_types (tenant_id, org_id, status, sort_order);

CREATE TABLE IF NOT EXISTS dictionary_items (
  id VARCHAR(64) PRIMARY KEY,
  tenant_id VARCHAR(128) NOT NULL DEFAULT '',
  org_id VARCHAR(128) NOT NULL DEFAULT '',
  type_code VARCHAR(191) NOT NULL,
  item_value VARCHAR(191) NOT NULL,
  label_zh_cn VARCHAR(191) NOT NULL DEFAULT '',
  label_en_us VARCHAR(191) NOT NULL DEFAULT '',
  description VARCHAR(512) NOT NULL DEFAULT '',
  tag VARCHAR(64) NOT NULL DEFAULT '',
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  sort_order INTEGER NOT NULL DEFAULT 0,
  system_owned BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
  deleted_at TIMESTAMPTZ(6) NULL,
  CONSTRAINT uq_dictionary_items_scope_value UNIQUE (tenant_id, org_id, type_code, item_value),
  CONSTRAINT chk_dictionary_items_status CHECK (status IN ('active', 'disabled')),
  CONSTRAINT chk_dictionary_items_sort CHECK (sort_order >= 0)
);
CREATE INDEX IF NOT EXISTS idx_dictionary_items_scope_type ON dictionary_items (tenant_id, org_id, type_code, status, sort_order);

CREATE TABLE IF NOT EXISTS dictionary_cache_versions (
  id VARCHAR(64) PRIMARY KEY,
  tenant_id VARCHAR(128) NOT NULL DEFAULT '',
  org_id VARCHAR(128) NOT NULL DEFAULT '',
  type_code VARCHAR(191) NOT NULL,
  version BIGINT NOT NULL DEFAULT 0,
  created_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMPTZ(6) NOT NULL DEFAULT CURRENT_TIMESTAMP,
  deleted_at TIMESTAMPTZ(6) NULL,
  CONSTRAINT uq_dictionary_cache_scope_type UNIQUE (tenant_id, org_id, type_code),
  CONSTRAINT chk_dictionary_cache_version CHECK (version >= 0)
);

INSERT INTO dictionary_types (id, tenant_id, org_id, code, name_zh_cn, name_en_us, description, status, sort_order, system_owned)
VALUES ('system-common-status', '', '', 'common.status', '通用状态', 'Common status', '系统预置状态字典', 'active', 0, TRUE)
ON CONFLICT (id) DO NOTHING;
INSERT INTO dictionary_items (id, tenant_id, org_id, type_code, item_value, label_zh_cn, label_en_us, status, sort_order, system_owned)
VALUES
  ('system-common-status-active', '', '', 'common.status', 'active', '启用', 'Active', 'active', 1, TRUE),
  ('system-common-status-disabled', '', '', 'common.status', 'disabled', '停用', 'Disabled', 'active', 2, TRUE)
ON CONFLICT (id) DO NOTHING;
INSERT INTO dictionary_cache_versions (id, tenant_id, org_id, type_code, version)
VALUES ('system-common-status-cache', '', '', 'common.status', 1)
ON CONFLICT (id) DO NOTHING;
