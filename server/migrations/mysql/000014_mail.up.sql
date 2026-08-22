CREATE TABLE IF NOT EXISTS smtp_accounts (
  id VARCHAR(64) NOT NULL,
  tenant_id VARCHAR(128) NOT NULL,
  org_id VARCHAR(128) NULL,
  account_name VARCHAR(128) NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  host VARCHAR(255) NOT NULL,
  port INT NOT NULL,
  username VARCHAR(255) NOT NULL DEFAULT '',
  password_ciphertext MEDIUMBLOB NULL,
  weight INT NOT NULL DEFAULT 1,
  from_email VARCHAR(320) NOT NULL,
  from_name VARCHAR(255) NOT NULL DEFAULT '',
  implicit_tls BOOLEAN NOT NULL DEFAULT FALSE,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  deleted_at DATETIME(6) NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_smtp_accounts_tenant_name (tenant_id, account_name),
  UNIQUE KEY uq_smtp_accounts_tenant_endpoint (tenant_id, host, port, username),
  KEY idx_smtp_accounts_scope (tenant_id, org_id, deleted_at),
  KEY idx_smtp_accounts_enabled (tenant_id, enabled, deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS email_messages (
  id VARCHAR(64) NOT NULL,
  tenant_id VARCHAR(128) NOT NULL,
  org_id VARCHAR(128) NULL,
  smtp_account_id VARCHAR(64) NULL,
  sender_id VARCHAR(128) NULL,
  subject VARCHAR(998) NOT NULL,
  body_ciphertext MEDIUMBLOB NOT NULL,
  body_digest CHAR(64) NOT NULL,
  status VARCHAR(16) NOT NULL,
  attempt_count INT NOT NULL DEFAULT 0,
  provider_message_id VARCHAR(255) NULL,
  last_error_code VARCHAR(64) NULL,
  sent_at DATETIME(6) NULL,
  idempotency_key VARCHAR(128) NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  deleted_at DATETIME(6) NULL,
  PRIMARY KEY (id),
  KEY idx_email_messages_scope (tenant_id, org_id, created_at),
  KEY idx_email_messages_status (tenant_id, status, created_at),
  KEY idx_email_messages_idempotency (tenant_id, idempotency_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS email_recipients (
  id VARCHAR(64) NOT NULL,
  message_id VARCHAR(64) NOT NULL,
  kind VARCHAR(16) NOT NULL DEFAULT 'to',
  address VARCHAR(320) NOT NULL,
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  deleted_at DATETIME(6) NULL,
  PRIMARY KEY (id),
  KEY idx_email_recipients_message (message_id, deleted_at),
  KEY idx_email_recipients_address (address)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS email_delivery_attempts (
  id VARCHAR(64) NOT NULL,
  message_id VARCHAR(64) NOT NULL,
  account_id VARCHAR(64) NOT NULL,
  attempt_no INT NOT NULL,
  stage VARCHAR(32) NOT NULL DEFAULT '',
  code VARCHAR(64) NOT NULL DEFAULT '',
  created_at DATETIME(6) NOT NULL,
  updated_at DATETIME(6) NOT NULL,
  deleted_at DATETIME(6) NULL,
  PRIMARY KEY (id),
  KEY idx_email_attempts_message (message_id, attempt_no),
  KEY idx_email_attempts_account (account_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
