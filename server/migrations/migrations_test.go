package migrations_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ByteJason/Gin-Vben-Admin/server/migrations"
)

func TestSchemaFieldsHaveCommentsAndNoForeignKeyRelationships(t *testing.T) {
	for table, parsed := range parsedSchemas(t) {
		if len(parsed.Relationships.Relations) != 0 {
			t.Fatalf("table %s declares GORM relationships: %v", table, parsed.Relationships.Relations)
		}
		for _, field := range parsed.Fields {
			if strings.TrimSpace(field.TagSettings["COMMENT"]) == "" {
				t.Fatalf("table %s field %s has no COMMENT tag", table, field.DBName)
			}
			if hasTagToken(field.StructField.Tag, "foreignKey") || hasTagToken(field.StructField.Tag, "references") || hasTagToken(field.StructField.Tag, "constraint") {
				t.Fatalf("table %s field %s contains a foreign-key tag", table, field.DBName)
			}
		}
	}
}

func TestSchemaRelationshipIDsRemainScalarWithoutForeignKeyDDL(t *testing.T) {
	for _, table := range expectedSchemaTables {
		fields := fieldsFor(t, table)
		for name, field := range fields {
			if !strings.HasSuffix(name, "_id") {
				continue
			}
			kind := field.FieldType.Kind()
			if kind == reflect.Pointer {
				kind = field.FieldType.Elem().Kind()
			}
			if kind != reflect.String && kind != reflect.Uint && kind != reflect.Uint8 && kind != reflect.Uint16 && kind != reflect.Uint32 && kind != reflect.Uint64 && kind != reflect.Int && kind != reflect.Int8 && kind != reflect.Int16 && kind != reflect.Int32 && kind != reflect.Int64 {
				t.Fatalf("relationship id %s.%s is not scalar: %s", table, name, kind)
			}
		}
	}
}

func TestSchemaUsesPortableJSONAndBinaryTypes(t *testing.T) {
	for _, table := range []string{"app_metadata", "setting_versions", "iam_data_scopes", "task_definitions"} {
		requireFields(t, table, map[string]string{
			"app_metadata":     "metadata_value",
			"setting_versions": "value",
			"iam_data_scopes":  "ids",
			"task_definitions": "payload_schema",
		}[table])
	}
	requireFields(t, "smtp_accounts", "password_ciphertext")
}

func TestSchemaSeedTablesArePresent(t *testing.T) {
	for _, table := range []string{"app_metadata", "tenants", "organizations", "dictionary_types", "dictionary_items", "dictionary_cache_versions"} {
		if _, ok := parsedSchemas(t)[table]; !ok {
			t.Fatalf("seed table %s missing", table)
		}
	}
}

// Keep the migration API exercised by contract tests without connecting to a
// database. A nil handle returns the documented initialization error.
func TestCreateSchemaRejectsUninitializedDatabase(t *testing.T) {
	if err := migrations.CreateSchema(nil); err == nil {
		t.Fatal("CreateSchema(nil) returned nil error")
	}
	if err := migrations.DropSchema(nil); err == nil {
		t.Fatal("DropSchema(nil) returned nil error")
	}
}
