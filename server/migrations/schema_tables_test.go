package migrations_test

import (
	"reflect"
	"testing"

	"github.com/ByteJason/Gin-Vben-Admin/server/migrations"
)

func TestSingleGORMSchemaContainsAllFreshInstallTables(t *testing.T) {
	if got := migrations.TableNames(); !reflect.DeepEqual(got, expectedSchemaTables) {
		t.Fatalf("schema tables = %v, want %v", got, expectedSchemaTables)
	}
	if got := len(migrations.Models()); got != len(expectedSchemaTables) {
		t.Fatalf("schema model count = %d, want %d", got, len(expectedSchemaTables))
	}
}
