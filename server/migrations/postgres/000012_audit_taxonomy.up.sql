ALTER TABLE auth_audit_events
  ADD COLUMN IF NOT EXISTS category VARCHAR(16) NOT NULL DEFAULT 'operation';

UPDATE auth_audit_events SET category = 'login' WHERE event_type LIKE 'auth.%';
UPDATE auth_audit_events SET category = 'system' WHERE event_type LIKE 'system.%';

CREATE INDEX IF NOT EXISTS idx_auth_audit_category_created ON auth_audit_events (category, created_at);
