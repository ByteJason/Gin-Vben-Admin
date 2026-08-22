DROP INDEX IF EXISTS idx_auth_audit_category_created;
ALTER TABLE auth_audit_events DROP COLUMN IF EXISTS category;
