CREATE TABLE IF NOT EXISTS app_metadata (
  metadata_key VARCHAR(191) NOT NULL,
  metadata_value JSON NOT NULL,
  version BIGINT UNSIGNED NOT NULL,
  created_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (metadata_key),
  CONSTRAINT chk_app_metadata_version CHECK (version > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

INSERT IGNORE INTO app_metadata (metadata_key, metadata_value, version)
VALUES ('product', JSON_OBJECT('name', 'gin-vben-admin'), 1);
