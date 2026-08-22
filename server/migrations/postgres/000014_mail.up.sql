CREATE TABLE IF NOT EXISTS smtp_accounts (
  id VARCHAR(64) PRIMARY KEY,
  tenant_id VARCHAR(128) NOT NULL,
  org_id VARCHAR(128),
  account_name VARCHAR(128) NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  host VARCHAR(255) NOT NULL,
  port INTEGER NOT NULL,
  username VARCHAR(255) NOT NULL DEFAULT '',
  password_ciphertext BYTEA,
  weight INTEGER NOT NULL DEFAULT 1,
  from_email VARCHAR(320) NOT NULL,
  from_name VARCHAR(255) NOT NULL DEFAULT '',
  implicit_tls BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  deleted_at TIMESTAMPTZ,
  CONSTRAINT uq_smtp_accounts_tenant_name UNIQUE (tenant_id, account_name),
  CONSTRAINT uq_smtp_accounts_tenant_endpoint UNIQUE (tenant_id, host, port, username)
);
CREATE INDEX IF NOT EXISTS idx_smtp_accounts_scope ON smtp_accounts (tenant_id, org_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_smtp_accounts_enabled ON smtp_accounts (tenant_id, enabled, deleted_at);

CREATE TABLE IF NOT EXISTS email_messages (
  id VARCHAR(64) PRIMARY KEY,
  tenant_id VARCHAR(128) NOT NULL,
  org_id VARCHAR(128),
  smtp_account_id VARCHAR(64),
  sender_id VARCHAR(128),
  subject VARCHAR(998) NOT NULL,
  body_ciphertext BYTEA NOT NULL,
  body_digest CHAR(64) NOT NULL,
  status VARCHAR(16) NOT NULL,
  attempt_count INTEGER NOT NULL DEFAULT 0,
  provider_message_id VARCHAR(255),
  last_error_code VARCHAR(64),
  sent_at TIMESTAMPTZ,
  idempotency_key VARCHAR(128),
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  deleted_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_email_messages_scope ON email_messages (tenant_id, org_id, created_at);
CREATE INDEX IF NOT EXISTS idx_email_messages_status ON email_messages (tenant_id, status, created_at);
CREATE INDEX IF NOT EXISTS idx_email_messages_idempotency ON email_messages (tenant_id, idempotency_key);

CREATE TABLE IF NOT EXISTS email_recipients (
  id VARCHAR(64) PRIMARY KEY,
  message_id VARCHAR(64) NOT NULL,
  kind VARCHAR(16) NOT NULL DEFAULT 'to',
  address VARCHAR(320) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  deleted_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_email_recipients_message ON email_recipients (message_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_email_recipients_address ON email_recipients (address);

CREATE TABLE IF NOT EXISTS email_delivery_attempts (
  id VARCHAR(64) PRIMARY KEY,
  message_id VARCHAR(64) NOT NULL,
  account_id VARCHAR(64) NOT NULL,
  attempt_no INTEGER NOT NULL,
  stage VARCHAR(32) NOT NULL DEFAULT '',
  code VARCHAR(64) NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL,
  deleted_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_email_attempts_message ON email_delivery_attempts (message_id, attempt_no);
CREATE INDEX IF NOT EXISTS idx_email_attempts_account ON email_delivery_attempts (account_id, created_at);
