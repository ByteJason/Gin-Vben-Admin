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
	"gvba_sys_app_metadata", "gvba_sys_tenants", "gvba_sys_organizations", "gvba_iam_users", "gvba_iam_roles", "gvba_iam_user_roles",
	"gvba_iam_menus", "gvba_iam_permissions", "gvba_iam_policies", "gvba_iam_data_scopes", "gvba_auth_sessions",
	"gvba_audit_auth_events", "gvba_sys_setting_versions", "gvba_storage_file_objects", "gvba_storage_media_categories", "gvba_storage_media_usages", "gvba_notify_smtp_accounts",
	"gvba_notify_email_messages", "gvba_notify_email_recipients", "gvba_notify_email_delivery_attempts", "gvba_notify_callers", "gvba_notify_caller_accounts",
	"gvba_notify_templates", "gvba_notify_template_locales", "gvba_notify_template_versions", "gvba_verify_policies", "gvba_verify_challenges", "gvba_dict_types",
	"gvba_dict_items", "gvba_dict_cache_versions", "gvba_task_definitions", "gvba_task_runs",
	"gvba_task_run_logs", "gvba_import_jobs", "gvba_import_errors", "gvba_import_artifacts",
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
