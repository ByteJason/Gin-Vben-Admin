package migrations_test

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	persistencemodel "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/model"
	"github.com/ByteJason/Gin-Vben-Admin/server/migrations"
	"gorm.io/gorm/schema"
)

var expectedSchemaTables = []string{
	"app_metadata", "tenants", "organizations", "users", "roles", "user_roles",
	"menus", "permissions", "iam_policies", "iam_data_scopes", "auth_sessions",
	"auth_audit_events", "setting_versions", "file_objects", "smtp_accounts",
	"email_messages", "email_recipients", "email_delivery_attempts", "dictionary_types",
	"dictionary_items", "dictionary_cache_versions", "task_definitions", "task_runs",
	"task_run_logs", "import_export_jobs", "import_export_errors", "import_export_artifacts",
}

func parsedSchemas(t *testing.T) map[string]*schema.Schema {
	t.Helper()
	result := make(map[string]*schema.Schema, len(migrations.Models()))
	for _, model := range migrations.Models() {
		parsed, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse %T: %v", model, err)
		}
		if _, exists := result[parsed.Table]; exists {
			t.Fatalf("duplicate schema table %q", parsed.Table)
		}
		result[parsed.Table] = parsed
	}
	return result
}

func fieldsFor(t *testing.T, table string) map[string]*schema.Field {
	t.Helper()
	parsed := parsedSchemas(t)[table]
	if parsed == nil {
		t.Fatalf("schema table %q is missing", table)
	}
	fields := make(map[string]*schema.Field, len(parsed.Fields))
	for _, field := range parsed.Fields {
		fields[field.DBName] = field
	}
	return fields
}

func requireFields(t *testing.T, table string, names ...string) {
	t.Helper()
	fields := fieldsFor(t, table)
	for _, name := range names {
		if fields[name] == nil {
			t.Fatalf("table %s is missing field %s", table, name)
		}
	}
}

func hasTagToken(tag reflect.StructTag, token string) bool {
	gormTag := tag.Get("gorm")
	for _, part := range strings.Split(gormTag, ";") {
		if strings.EqualFold(strings.TrimSpace(part), token) || strings.HasPrefix(strings.ToLower(strings.TrimSpace(part)), strings.ToLower(token)+":") {
			return true
		}
	}
	return false
}

func TestMigrationRegistryUsesSharedPersistenceModels(t *testing.T) {
	definitions := migrations.ModelDefinitions()
	if len(definitions) != len(migrations.Models()) {
		t.Fatalf("definition/model counts = %d/%d", len(definitions), len(migrations.Models()))
	}
	for index, definition := range definitions {
		migrationModel := migrations.Models()[index]
		persistenceModel := definition.New()
		if reflect.TypeOf(migrationModel) != reflect.TypeOf(persistenceModel) {
			t.Fatalf("registry model %d type = %T, want persistence model %T", index, migrationModel, persistenceModel)
		}
		if persistencemodel.ModuleFor(persistenceModel) != definition.Module {
			t.Fatalf("registry model %d module = %q, want %q", index, persistencemodel.ModuleFor(persistenceModel), definition.Module)
		}
	}
}
