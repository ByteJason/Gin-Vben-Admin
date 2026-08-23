package installer

import (
	"context"
	"errors"
	"reflect"
	"testing"

	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
)

func TestPlanServiceReturnsAllowlistedActionsForSelectedUI(t *testing.T) {
	inspector := inspectorStub{permissions: map[string]PathPermission{
		"install":              {CanRead: true, CanWrite: true, CanCreate: true, CanRename: true, CanDelete: true},
		"admin/apps/web-antd":  {CanRead: true, CanWrite: true, CanCreate: true, CanRename: true, CanDelete: true},
		"admin/apps/web-ele":   {CanRead: true, CanWrite: true, CanCreate: true, CanRename: true, CanDelete: true},
		"admin/apps/web-naive": {CanRead: true, CanWrite: true, CanCreate: true, CanRename: true, CanDelete: true},
		"admin/apps/web":       {CanRead: true, CanWrite: true, CanCreate: true, CanRename: true, CanDelete: true},
		".env":                 {CanRead: false, CanWrite: true, CanCreate: true, CanRename: true, CanDelete: false},
	}}
	service := NewPlanService(&inspector)

	got, err := service.Plan(context.Background(), PlanRequest{SelectedUI: "antd", Mode: "embedded"})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if got.SelectedUI != installstate.UIAntd || got.Mode != installstate.ModeEmbedded || !got.CanCleanup || !got.CanBuild || !got.CanWriteEnv || !got.RequiresRestart {
		t.Fatalf("plan summary = %#v", got)
	}
	wantPaths := []string{"install", "admin/apps/web-antd", "admin/apps/web-ele", "admin/apps/web-naive", "admin/apps/web", ".env"}
	var paths []string
	for _, entry := range got.Entries {
		paths = append(paths, entry.Path)
	}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("plan paths = %#v, want %#v", paths, wantPaths)
	}
	if got.Entries[1].Action != ActionKeep || got.Entries[2].Action != ActionRemove || got.Entries[5].Action != ActionWrite {
		t.Fatalf("plan actions = %#v", got.Entries)
	}
	if !reflect.DeepEqual(inspector.seen, wantPaths) {
		t.Fatalf("inspected paths = %#v, want %#v", inspector.seen, wantPaths)
	}
}

func TestPlanServiceBlocksCleanupWhenPermissionIsMissing(t *testing.T) {
	service := NewPlanService(&inspectorStub{permissions: map[string]PathPermission{
		"install":              {CanRead: true, CanWrite: true, CanCreate: true, CanRename: true, CanDelete: true},
		"admin/apps/web-antd":  {CanRead: true, CanWrite: true, CanCreate: true, CanRename: true, CanDelete: true},
		"admin/apps/web-ele":   {CanRead: true, CanWrite: true, CanCreate: true, CanRename: false, CanDelete: false, Reasons: []string{"rename_or_delete_not_available"}},
		"admin/apps/web-naive": {CanRead: true, CanWrite: true, CanCreate: true, CanRename: true, CanDelete: true},
		"admin/apps/web":       {CanRead: true, CanWrite: true, CanCreate: true, CanRename: true, CanDelete: true},
		".env":                 {CanRead: false, CanWrite: true, CanCreate: true, CanRename: true, CanDelete: false},
	}})

	got, err := service.Plan(context.Background(), PlanRequest{SelectedUI: "antd", Mode: "standalone"})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if got.CanCleanup {
		t.Fatal("CanCleanup = true, want false when an unselected template cannot be removed")
	}
	if len(got.Reasons) == 0 {
		t.Fatalf("plan reasons = %#v, want actionable reason", got)
	}
}

func TestPlanServiceRejectsUnknownUIOrModeBeforeInspectingPaths(t *testing.T) {
	inspector := inspectorStub{}
	service := NewPlanService(&inspector)
	for _, request := range []PlanRequest{{SelectedUI: "web-antdv-next", Mode: "embedded"}, {SelectedUI: "antd", Mode: "shell"}} {
		if _, err := service.Plan(context.Background(), request); err == nil {
			t.Fatalf("Plan(%#v) error = nil, want validation error", request)
		}
	}
	if len(inspector.seen) != 0 {
		t.Fatalf("invalid request inspected paths: %#v", inspector.seen)
	}
}

type inspectorStub struct {
	permissions map[string]PathPermission
	seen        []string
}

func (s *inspectorStub) Inspect(_ context.Context, path string) (PathPermission, error) {
	s.seen = append(s.seen, path)
	permission, ok := s.permissions[path]
	if !ok {
		return PathPermission{}, errors.New("missing fixture")
	}
	return permission, nil
}
