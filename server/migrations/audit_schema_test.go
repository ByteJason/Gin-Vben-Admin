package migrations_test

import "testing"

func TestAuthAuditSchemaIncludesDeviceAndRequestFields(t *testing.T) {
	requireFields(t, "gvba_auth_sessions", "device_id", "device_name", "ip_address", "user_agent")
	requireFields(t, "gvba_audit_auth_events", "ip_address", "user_agent", "request_id", "category")
}
