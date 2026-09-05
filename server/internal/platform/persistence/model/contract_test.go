package model

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestDefinitionsExposeOneCommentedModelPerTable(t *testing.T) {
	definitions := Definitions()
	if len(definitions) != 36 {
		t.Fatalf("model definition count = %d, want 36", len(definitions))
	}
	seen := make(map[string]struct{}, len(definitions))
	comments := TableComments()
	for _, definition := range definitions {
		value := definition.New()
		if value == nil {
			t.Fatalf("%s definition returned nil", definition.Module)
		}
		parsed, err := schema.Parse(value, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse %T: %v", value, err)
		}
		if _, exists := seen[parsed.Table]; exists {
			t.Fatalf("duplicate model table %q", parsed.Table)
		}
		seen[parsed.Table] = struct{}{}
		if strings.TrimSpace(comments[parsed.Table]) == "" {
			t.Fatalf("table %s has no table comment", parsed.Table)
		}
		if len(parsed.Relationships.Relations) != 0 {
			t.Fatalf("migration model %s declares GORM relationships", parsed.Table)
		}
		for _, field := range parsed.Fields {
			if strings.TrimSpace(field.TagSettings["COMMENT"]) == "" {
				t.Fatalf("table %s field %s has no comment", parsed.Table, field.DBName)
			}
		}
	}
	if got := len(ModelsFor(ModuleClient)); got != 0 {
		t.Fatalf("reserved client model count = %d, want 0", got)
	}
}

func TestModelsForPreservesModuleBoundaries(t *testing.T) {
	if got := len(ModelsFor(ModuleShared)); got != 4 {
		t.Fatalf("shared model count = %d, want 4", got)
	}
	if got := len(ModelsFor(ModuleAuth)); got != 1 {
		t.Fatalf("auth model count = %d, want 1", got)
	}
	if got := len(ModelsFor(ModuleAudit)); got != 1 {
		t.Fatalf("audit model count = %d, want 1", got)
	}
	if got := len(ModelsFor(ModuleAdmin)); got != 30 {
		t.Fatalf("admin model count = %d, want 30", got)
	}
	for _, value := range All() {
		if ModuleFor(value) == "" {
			t.Fatalf("model %T has no module", value)
		}
	}
}

func TestScopedUniqueIndexesCarryTenantAndOrganizationDimensions(t *testing.T) {
	want := map[string]struct {
		index  string
		fields []string
	}{
		"gvba_notify_callers":           {index: "uq_gvba_notify_callers_scope_key", fields: []string{"tenant_id", "org_id", "scope_type", "caller_key"}},
		"gvba_notify_templates":         {index: "uq_gvba_notify_templates_scope_key", fields: []string{"tenant_id", "org_id", "scope_type", "template_key"}},
		"gvba_verify_policies":          {index: "uq_gvba_verify_policies_scope_key", fields: []string{"tenant_id", "org_id", "scope_type", "policy_key"}},
		"gvba_storage_media_categories": {index: "uq_gvba_storage_media_categories_scope_parent_name", fields: []string{"tenant_id", "org_id", "scope_type", "parent_id", "name"}},
	}
	for _, definition := range Definitions() {
		parsed, err := schema.Parse(definition.New(), &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse %T: %v", definition.New(), err)
		}
		expected, ok := want[parsed.Table]
		if !ok {
			continue
		}
		var found *schema.Index
		for _, index := range parsed.ParseIndexes() {
			if index.Name == expected.index {
				found = index
				break
			}
		}
		if found == nil {
			t.Fatalf("table %s is missing scoped unique index %s", parsed.Table, expected.index)
		}
		actual := make(map[string]struct{}, len(found.Fields))
		for _, field := range found.Fields {
			actual[field.DBName] = struct{}{}
		}
		for _, field := range expected.fields {
			if _, ok := actual[field]; !ok {
				t.Fatalf("table %s index %s is missing scoped field %s", parsed.Table, expected.index, field)
			}
		}
		delete(want, parsed.Table)
	}
	if len(want) != 0 {
		t.Fatalf("scoped unique index checks did not visit tables: %v", want)
	}
}

func TestMySQLIndexKeyWidthsStayWithinInnoDBLimit(t *testing.T) {
	// The schema is shared by MySQL and PostgreSQL.  A worst-case utf8mb4
	// estimate catches accidental full-value indexes on long varchar columns
	// (InnoDB's default key limit is 3072 bytes) before a fresh install fails.
	const maxInnoDBKeyBytes = 3072
	for _, definition := range Definitions() {
		parsed, err := schema.Parse(definition.New(), &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse %T: %v", definition.New(), err)
		}
		for _, index := range parsed.ParseIndexes() {
			bytes := 0
			for _, option := range index.Fields {
				length := option.Length
				if length == 0 {
					length = int(option.Field.Size)
				}
				if length > 0 && (option.Field.DataType == schema.String || option.Field.DataType == schema.Bytes) {
					bytes += length * 4
				} else {
					// Numeric/time keys are bounded well below a varchar's
					// worst-case width; eight bytes is a conservative estimate.
					bytes += 8
				}
			}
			if bytes > maxInnoDBKeyBytes {
				t.Fatalf("table %s index %s estimated key width %d exceeds %d bytes", parsed.Table, index.Name, bytes, maxInnoDBKeyBytes)
			}
		}
	}
}

func TestRelationCatalogReferencesKnownTablesAndKeepsTenantScope(t *testing.T) {
	tables := make(map[string]struct{}, len(Definitions()))
	for _, definition := range Definitions() {
		parsed, err := schema.Parse(definition.New(), &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatalf("parse %T: %v", definition.New(), err)
		}
		tables[parsed.Table] = struct{}{}
	}
	kinds := make(map[RelationKind]int)
	if len(Relations()) < 60 {
		t.Fatalf("relation catalog is unexpectedly small: %d", len(Relations()))
	}
	for _, relation := range Relations() {
		kinds[relation.Kind]++
		if _, ok := tables[relation.From]; !ok {
			t.Fatalf("relation source %q is not a schema table", relation.From)
		}
		if _, ok := tables[relation.To]; !ok {
			t.Fatalf("relation target %q is not a schema table", relation.To)
		}
		if relation.TenantKey == "" || relation.Keys == "" || relation.Description == "" {
			t.Fatalf("relation metadata is incomplete: %+v", relation)
		}
	}
	for _, kind := range []RelationKind{RelationBelongsTo, RelationHasOne, RelationHasMany} {
		if kinds[kind] == 0 {
			t.Fatalf("relation catalog has no %s entries", kind)
		}
	}
}

// compileGenericUserCRUD is intentionally not executed: its purpose is to
// keep the supported GORM generics surface checked by the compiler while
// leaving tenant scoping and transaction policy in concrete repositories.
func compileGenericUserCRUD(ctx context.Context, db *gorm.DB, user *User) error {
	if err := gorm.G[User](db).Create(ctx, user); err != nil {
		return err
	}
	_, err := gorm.G[User](db).Where("id = ?", user.ID).First(ctx)
	return err
}

func TestGenericCRUDSurfaceCompiles(t *testing.T) {
	var operation func(context.Context, *gorm.DB, *User) error = compileGenericUserCRUD
	if reflect.ValueOf(operation).IsNil() {
		t.Fatal("generic CRUD operation is nil")
	}
}
