package admin

import (
	"gorm.io/gorm/schema"
	"strings"
	"sync"
	"testing"
)

func TestV005VersionAndMetadataColumnsMatchModels(t *testing.T) {
	if V005Version != "v005_task_run_execution_metadata" {
		t.Fatalf("version=%q", V005Version)
	}
	for _, item := range taskRunMetadataColumns {
		p, e := schema.Parse(item.model, &sync.Map{}, schema.NamingStrategy{})
		if e != nil {
			t.Fatal(e)
		}
		for _, c := range item.columns {
			f := p.FieldsByDBName[c]
			if f == nil {
				t.Fatalf("table %s missing migration column %s", p.Table, c)
			}
			if f.NotNull && !f.HasDefaultValue && f.DefaultValue == "" && f.DefaultValueInterface == nil {
				t.Fatalf("table %s column %s lacks default", p.Table, c)
			}
		}
	}
}
func TestV005TargetsOnlyTaskTables(t *testing.T) {
	want := map[string]bool{"gvba_task_definitions": true, "gvba_task_runs": true, "gvba_task_run_logs": true}
	for _, item := range taskRunMetadataColumns {
		p, e := schema.Parse(item.model, &sync.Map{}, schema.NamingStrategy{})
		if e != nil {
			t.Fatal(e)
		}
		if !want[p.Table] {
			t.Fatalf("unexpected target %q", p.Table)
		}
		delete(want, p.Table)
	}
	if len(want) > 0 {
		t.Fatalf("missing %v", want)
	}
}
func TestV005MigrationRejectsNilDatabase(t *testing.T) {
	if e := UpV005(nil); e == nil || !strings.Contains(e.Error(), "database") {
		t.Fatalf("UpV005=%v", e)
	}
	if e := DownV005(nil); e == nil || !strings.Contains(e.Error(), "database") {
		t.Fatalf("DownV005=%v", e)
	}
}
