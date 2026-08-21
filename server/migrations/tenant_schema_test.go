package migrations_test

import (
	"strings"
	"testing"

	"example.com/gin-vben-admin/server/migrations"
)

func TestTenantMigrationAssetsDeclareIsolationSchema(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		up, err := migrations.FS.ReadFile(driver + "/000006_tenants.up.sql")
		if err != nil {
			t.Fatalf("read %s tenant migration: %v", driver, err)
		}
		sql := strings.ToLower(string(up))
		for _, token := range []string{"tenants", "organizations", "tenant_id", "org_id", "default"} {
			if !strings.Contains(sql, token) {
				t.Fatalf("%s tenant migration missing %q", driver, token)
			}
		}
		down, err := migrations.FS.ReadFile(driver + "/000006_tenants.down.sql")
		if err != nil || len(strings.TrimSpace(string(down))) == 0 {
			t.Fatalf("%s tenant migration has no down asset: %v", driver, err)
		}
	}
}

func TestTenantMigrationAddsTenantScopeToCoreTables(t *testing.T) {
	up, err := migrations.FS.ReadFile("mysql/000006_tenants.up.sql")
	if err != nil {
		t.Fatalf("read mysql tenant migration: %v", err)
	}
	sql := strings.ToLower(string(up))
	for _, table := range []string{"users", "user_roles", "roles", "menus", "permissions", "iam_policies", "iam_data_scopes", "auth_sessions", "auth_audit_events", "setting_versions"} {
		if !strings.Contains(sql, "alter table "+table) {
			t.Fatalf("tenant migration does not alter %s", table)
		}
	}
}

func TestTenantMigrationScopesUsernameUniqueness(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		up, err := migrations.FS.ReadFile(driver + "/000007_tenant_username.up.sql")
		if err != nil {
			t.Fatalf("read %s username migration: %v", driver, err)
		}
		sql := strings.ToLower(string(up))
		if !strings.Contains(sql, "tenant_id") || !strings.Contains(sql, "username") {
			t.Fatalf("%s username migration does not scope uniqueness: %s", driver, sql)
		}
		down, err := migrations.FS.ReadFile(driver + "/000007_tenant_username.down.sql")
		if err != nil || len(strings.TrimSpace(string(down))) == 0 {
			t.Fatalf("%s username migration has no down asset: %v", driver, err)
		}
	}
}

func TestTenantSettingsMigrationScopesVersionUniqueness(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		up, err := migrations.FS.ReadFile(driver + "/000008_tenant_settings_key.up.sql")
		if err != nil {
			t.Fatalf("read %s settings migration: %v", driver, err)
		}
		sql := strings.ToLower(string(up))
		for _, token := range []string{"setting_versions", "tenant_id", "key", "version"} {
			if !strings.Contains(sql, token) {
				t.Fatalf("%s settings migration missing %q", driver, token)
			}
		}
		down, err := migrations.FS.ReadFile(driver + "/000008_tenant_settings_key.down.sql")
		if err != nil || len(strings.TrimSpace(string(down))) == 0 {
			t.Fatalf("%s settings migration has no down asset: %v", driver, err)
		}
	}
}
