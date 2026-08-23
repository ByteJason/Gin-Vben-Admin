package migrations_test

import (
	"strings"
	"testing"

	"github.com/ByteJason/Gin-Vben-Admin/server/migrations"
)

func TestRBACMigrationIsPresentAndPaired(t *testing.T) {
	for _, driver := range []string{"mysql", "postgres"} {
		for _, suffix := range []string{"up.sql", "down.sql"} {
			path := driver + "/000003_rbac." + suffix
			content, err := migrations.FS.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			if len(strings.TrimSpace(string(content))) == 0 {
				t.Fatalf("%s is empty", path)
			}
		}
		up, _ := migrations.FS.ReadFile(driver + "/000003_rbac.up.sql")
		for _, token := range []string{"roles", "user_roles", "menus", "permissions", "iam_policies", "iam_data_scopes"} {
			if !strings.Contains(string(up), token) {
				t.Fatalf("%s migration missing %q", driver, token)
			}
		}
	}
}
