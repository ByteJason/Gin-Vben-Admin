package migrations_test

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/ByteJason/Gin-Vben-Admin/server/migrations"
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

func TestUserProfileMigrationAddsNormalizedIdentifiersAndProfileFields(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		up, err := fs.ReadFile(migrations.FS, driver+"/000009_user_profile.up.sql")
		if err != nil {
			t.Fatalf("read %s user profile migration: %v", driver, err)
		}
		sql := strings.ToLower(string(up))
		for _, token := range []string{"alter table users", "username_normalized", "email_normalized", "nickname", "avatar", "phone", "last_login_ip", "last_login_at", "password_changed_at", "unique", "e164", "lower", "trim"} {
			if !strings.Contains(sql, token) {
				t.Fatalf("%s user profile migration missing %q", driver, token)
			}
		}
		down, err := fs.ReadFile(migrations.FS, driver+"/000009_user_profile.down.sql")
		if err != nil || len(strings.TrimSpace(string(down))) == 0 {
			t.Fatalf("%s user profile migration has no down asset: %v", driver, err)
		}
	}
}
