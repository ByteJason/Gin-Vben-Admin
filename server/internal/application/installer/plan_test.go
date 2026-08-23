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
		"admin/apps/install":  {CanRead: true, CanWrite: true, CanCreate: true, CanRename: true, CanDelete: true},
		"admin/apps/web-antd": {CanRead: true, CanWrite: true, CanCreate: true, CanRename: true, CanDelete: true},
		".env":                {CanRead: false, CanWrite: true, CanCreate: true, CanRename: true, CanDelete: false},
	}}
	service := NewPlanServiceWithProfile(&inspector, profileProviderStub{profile: InstallationProfile{SelectedUI: installstate.UIAntd}, exists: true})

	got, err := service.Plan(context.Background(), PlanRequest{Mode: "embedded"})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if got.SelectedUI != installstate.UIAntd || got.Mode != installstate.ModeEmbedded || !got.CanCleanup || !got.CanBuild || !got.CanWriteEnv || !got.RequiresRestart {
		t.Fatalf("plan summary = %#v", got)
	}
	wantPaths := []string{"admin/apps/install", "admin/apps/web-antd", ".env"}
	var paths []string
	for _, entry := range got.Entries {
		paths = append(paths, entry.Path)
	}
	if !reflect.DeepEqual(paths, wantPaths) {
		t.Fatalf("plan paths = %#v, want %#v", paths, wantPaths)
	}
	if got.Entries[0].Action != ActionKeep || got.Entries[1].Action != ActionKeep || got.Entries[2].Action != ActionWrite {
		t.Fatalf("plan actions = %#v", got.Entries)
	}
	if !reflect.DeepEqual(inspector.seen, wantPaths) {
		t.Fatalf("inspected paths = %#v, want %#v", inspector.seen, wantPaths)
	}
}

func TestPlanServiceBlocksCleanupWhenPermissionIsMissing(t *testing.T) {
	service := NewPlanService(&inspectorStub{permissions: map[string]PathPermission{
		"admin/apps/install":  {CanRead: true, CanWrite: true, CanCreate: true, CanRename: true, CanDelete: true},
		"admin/apps/web-antd": {CanRead: true, CanWrite: true, CanCreate: true, CanRename: true, CanDelete: true},
		".env":                {CanRead: false, CanWrite: true, CanCreate: true, CanRename: true, CanDelete: false},
	}})
	service.profiles = profileProviderStub{profile: InstallationProfile{SelectedUI: installstate.UIAntd}, exists: true}

	got, err := service.Plan(context.Background(), PlanRequest{Mode: "standalone"})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if !got.CanCleanup {
		t.Fatal("CanCleanup = false, want true because CLI already staged unselected templates")
	}
}

func TestPlanServiceRejectsUnknownUIOrModeBeforeInspectingPaths(t *testing.T) {
	inspector := inspectorStub{}
	service := NewPlanServiceWithProfile(&inspector, profileProviderStub{profile: InstallationProfile{SelectedUI: installstate.UIAntd}, exists: true})
	for _, request := range []PlanRequest{{Mode: "shell"}} {
		if _, err := service.Plan(context.Background(), request); err == nil {
			t.Fatalf("Plan(%#v) error = nil, want validation error", request)
		}
	}
	if len(inspector.seen) != 0 {
		t.Fatalf("invalid request inspected paths: %#v", inspector.seen)
	}
}

func TestPlanServiceUsesReadOnlyProfileSelection(t *testing.T) {
	inspector := &inspectorStub{permissions: map[string]PathPermission{
		"admin/apps/install":  {CanRead: true},
		"admin/apps/web-antd": {CanRead: true, CanWrite: true, CanCreate: true, CanRename: true, CanDelete: true},
		"admin/apps/web-ele":  {CanRead: true, CanWrite: true, CanCreate: true, CanRename: true, CanDelete: true},
		".env":                {CanWrite: true, CanCreate: true, CanRename: true},
	}}
	service := NewPlanServiceWithProfile(inspector, profileProviderStub{profile: InstallationProfile{SelectedUI: installstate.UIEle}, exists: true})

	got, err := service.Plan(context.Background(), PlanRequest{Mode: "embedded"})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if got.SelectedUI != installstate.UIEle {
		t.Fatalf("Plan().SelectedUI = %q, want profile selection", got.SelectedUI)
	}
	paths := []string{got.Entries[0].Path, got.Entries[1].Path, got.Entries[2].Path}
	if want := []string{"admin/apps/install", "admin/apps/web-ele", ".env"}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("Plan() paths = %#v, want %#v", paths, want)
	}
}

func TestPlanServiceRequiresTheAdminInstallerWorkspaceToBeReadable(t *testing.T) {
	inspector := &inspectorStub{permissions: map[string]PathPermission{
		"admin/apps/install":  {CanRead: false},
		"admin/apps/web-antd": {CanRead: true},
		".env":                {CanWrite: true, CanCreate: true, CanRename: true},
	}}
	service := NewPlanServiceWithProfile(inspector, profileProviderStub{profile: InstallationProfile{SelectedUI: installstate.UIAntd}, exists: true})

	got, err := service.Plan(context.Background(), PlanRequest{Mode: "embedded"})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if got.CanBuild {
		t.Fatalf("Plan().CanBuild = true with unreadable admin/apps/install: %#v", got)
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
