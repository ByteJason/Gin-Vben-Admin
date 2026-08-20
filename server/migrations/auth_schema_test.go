package migrations_test

import (
	"io/fs"
	"strings"
	"testing"

	"example.com/gin-vben-admin/server/migrations"
)

func TestSecondMigrationCreatesAuthTables(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		content, err := fs.ReadFile(migrations.FS, driver+"/000002_auth.up.sql")
		if err != nil {
			t.Fatalf("read %s auth migration: %v", driver, err)
		}
		sql := strings.ToLower(string(content))
		for _, token := range []string{"create table", "users", "auth_sessions", "password_hash", "refresh_token_hash", "family_id", "locked_until"} {
			if !strings.Contains(sql, token) {
				t.Fatalf("%s auth migration missing %q", driver, token)
			}
		}
	}
}
