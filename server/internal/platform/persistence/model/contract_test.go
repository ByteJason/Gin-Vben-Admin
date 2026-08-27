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
	if len(definitions) != 27 {
		t.Fatalf("model definition count = %d, want 27", len(definitions))
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
	if got := len(ModelsFor(ModuleAdmin)); got != 21 {
		t.Fatalf("admin model count = %d, want 21", got)
	}
	for _, value := range All() {
		if ModuleFor(value) == "" {
			t.Fatalf("model %T has no module", value)
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
