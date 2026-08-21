package migrations

import (
	"strings"
	"testing"
)

func TestMySQLSettingsMigrationQuotesReservedIdentifiers(t *testing.T) {
	sql, err := FS.ReadFile("mysql/000005_settings.up.sql")
	if err != nil {
		t.Fatalf("read mysql settings migration: %v", err)
	}
	source := string(sql)
	for _, identifier := range []string{"value", "version", "sensitive", "updated_by"} {
		quoted := "`" + identifier + "`"
		if !strings.Contains(source, quoted) {
			t.Fatalf("mysql settings migration does not quote reserved/portable identifier %q", identifier)
		}
	}
}
