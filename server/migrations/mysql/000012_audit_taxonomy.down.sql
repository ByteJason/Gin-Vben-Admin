DROP INDEX idx_auth_audit_category_created ON auth_audit_events;
ALTER TABLE auth_audit_events DROP COLUMN category;
