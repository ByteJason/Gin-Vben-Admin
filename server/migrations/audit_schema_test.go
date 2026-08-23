package migrations_test

import (
	"strings"
	"testing"

	"github.com/ByteJason/Gin-Vben-Admin/server/migrations"
)

func TestAuthAuditMigrationHasDeviceFieldsAndEventTable(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		up, err := migrations.FS.ReadFile(driver + "/000004_auth_audit.up.sql")
		if err != nil {
			t.Fatalf("%s audit migration: %v", driver, err)
		}
		down, err := migrations.FS.ReadFile(driver + "/000004_auth_audit.down.sql")
		if err != nil {
			t.Fatalf("%s audit down migration: %v", driver, err)
		}
		for _, token := range []string{"auth_audit_events", "device_id", "device_name", "ip_address", "user_agent", "request_id"} {
			if !strings.Contains(string(up), token) {
				t.Fatalf("%s up migration missing %q", driver, token)
			}
		}
		if !strings.Contains(string(down), "auth_audit_events") || !strings.Contains(string(down), "device_id") {
			t.Fatalf("%s down migration does not reverse audit/device changes", driver)
		}
	}
}
