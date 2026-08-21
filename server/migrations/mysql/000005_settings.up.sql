CREATE TABLE setting_versions (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  `key` VARCHAR(191) NOT NULL,
  `value` JSON NOT NULL,
  version BIGINT NOT NULL,
  sensitive BOOLEAN NOT NULL DEFAULT FALSE,
  updated_by VARCHAR(191) NOT NULL,
  updated_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  UNIQUE KEY uq_setting_versions_key_version (`key`, version),
  KEY idx_setting_versions_key_updated (`key`, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
