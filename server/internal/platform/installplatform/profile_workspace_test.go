package installplatform

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
)

func writeWorkspaceProfileTemplates(t *testing.T, root string, uis ...string) {
	t.Helper()
	for _, ui := range uis {
		app := filepath.Join(root, "admin", "apps", "web-"+ui)
		if err := os.MkdirAll(app, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(app, "package.json"), []byte(`{"name":"@vben/web-`+ui+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFileProfileProviderReadsIgnoredWorkspaceProfileAndKeepsAllTemplates(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceProfileTemplates(t, root, "antd", "ele", "naive")
	if err := os.WriteFile(filepath.Join(root, "admin", "pnpm-workspace.yaml"), []byte("packages:\n  - apps/*\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(root, "admin", ".ui-profile.local.json")
	if err := os.WriteFile(local, []byte(`{"schema":1,"selectedUi":"ele","packageName":"@vben/web-ele","appDirectory":"apps/web-ele"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := NewFileProfileProvider(root)
	if err != nil {
		t.Fatal(err)
	}
	profile, exists, err := provider.Profile(context.Background())
	if err != nil || !exists || profile.SelectedUI != installstate.UIEle || profile.Installing {
		t.Fatalf("Profile() = (%#v, %t, %v), want local ele profile", profile, exists, err)
	}
}

func TestWorkspaceJournalMustAgreeWithDurableProfile(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		profile string
	}{
		{name: "different UI", profile: `{"schema":1,"selectedUi":"naive","packageName":"@vben/web-naive","appDirectory":"apps/web-naive"}`},
		{name: "malformed", profile: `{"selectedUi":"ele"}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			writeWorkspaceProfileTemplates(t, root, "antd", "ele", "naive")
			if err := os.WriteFile(filepath.Join(root, "admin", ".ui-profile.local.json"), []byte(testCase.profile), 0o600); err != nil {
				t.Fatal(err)
			}
			state := filepath.Join(root, ".runtime", "install")
			if err := os.MkdirAll(state, 0o700); err != nil {
				t.Fatal(err)
			}
			journal := `{"schema":1,"owner":"admin-init-workspace","id":"12345678-1234-1234-1234-123456789abc","selectedUi":"ele","phase":"dependencies_pending","moves":[]}`
			if err := os.WriteFile(filepath.Join(state, "workspace-transaction.json"), []byte(journal), 0o600); err != nil {
				t.Fatal(err)
			}
			provider, err := NewFileProfileProvider(root)
			if err != nil {
				t.Fatal(err)
			}
			profile, exists, err := provider.Profile(context.Background())
			if err != nil || !exists || profile != (installer.InstallationProfile{}) {
				t.Fatalf("Profile() = (%#v, %t, %v), want inconsistent", profile, exists, err)
			}
		})
	}
}

func TestWorkspaceSwitchJournalSupportsSelectorLastRecovery(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		profileUI string
	}{
		{name: "before selector commit", profileUI: "antd"},
		{name: "after selector commit", profileUI: "naive"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			writeWorkspaceProfileTemplates(t, root, "antd", "ele", "naive")
			adminRoot := filepath.Join(root, "admin")
			if err := os.WriteFile(filepath.Join(adminRoot, "pnpm-workspace.yaml"), []byte("packages:\n  - apps/*\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			profile := `{"schema":1,"selectedUi":"` + testCase.profileUI + `","packageName":"@vben/web-` + testCase.profileUI + `","appDirectory":"apps/web-` + testCase.profileUI + `"}`
			if err := os.WriteFile(filepath.Join(adminRoot, ".ui-profile.local.json"), []byte(profile), 0o600); err != nil {
				t.Fatal(err)
			}
			state := filepath.Join(root, ".runtime", "install")
			if err := os.MkdirAll(state, 0o700); err != nil {
				t.Fatal(err)
			}
			journal := `{"schema":1,"owner":"admin-init-workspace","id":"12345678-1234-1234-1234-123456789abc","selectedUi":"naive","phase":"switching_ui","moves":[]}`
			if err := os.WriteFile(filepath.Join(state, "workspace-transaction.json"), []byte(journal), 0o600); err != nil {
				t.Fatal(err)
			}
			provider, err := NewFileProfileProvider(root)
			if err != nil {
				t.Fatal(err)
			}
			resolved, exists, err := provider.Profile(context.Background())
			if err != nil || !exists || resolved.SelectedUI != installstate.UINaive || !resolved.Installing || !resolved.PreparingUI || !resolved.IndependentUISelection {
				t.Fatalf("Profile() = (%#v, %t, %v), want recoverable naive switch", resolved, exists, err)
			}
			status, err := installer.NewStatusServiceWithProfile(
				NewFileMarkerStore(filepath.Join(state, ".installed")),
				provider,
			).Status(context.Background())
			if err != nil || status.State != installer.StateInstalling || status.SelectedUI != installstate.UINaive || status.Phase != installer.InstallationPhaseUIPrepare {
				t.Fatalf("Status() = (%#v, %v), want recoverable UI switch", status, err)
			}
		})
	}
}

func TestWorkspaceManifestMustContainAppsGlob(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceProfileTemplates(t, root, "antd", "ele", "naive")
	adminRoot := filepath.Join(root, "admin")
	if err := os.WriteFile(filepath.Join(adminRoot, "pnpm-workspace.yaml"), []byte("packages:\n  - packages/*\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminRoot, ".ui-profile.local.json"), []byte(`{"schema":1,"selectedUi":"ele","packageName":"@vben/web-ele","appDirectory":"apps/web-ele"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := NewFileProfileProvider(root)
	if err != nil {
		t.Fatal(err)
	}
	profile, exists, err := provider.Profile(context.Background())
	if err != nil || !exists || profile != (installer.InstallationProfile{}) {
		t.Fatalf("Profile() = (%#v, %t, %v), want invalid workspace manifest", profile, exists, err)
	}
}

func TestWorkspaceJournalRequiresAllTemplatesEvenWithoutManifest(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceProfileTemplates(t, root, "antd", "ele")
	state := filepath.Join(root, ".runtime", "install")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	journal := `{"schema":1,"owner":"admin-init-workspace","id":"12345678-1234-1234-1234-123456789abc","selectedUi":"ele","phase":"dependencies_pending","moves":[]}`
	if err := os.WriteFile(filepath.Join(state, "workspace-transaction.json"), []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := NewFileProfileProvider(root)
	if err != nil {
		t.Fatal(err)
	}
	profile, exists, err := provider.Profile(context.Background())
	if err != nil || !exists || profile != (installer.InstallationProfile{}) {
		t.Fatalf("Profile() = (%#v, %t, %v), want inconsistent layout", profile, exists, err)
	}
}

func TestWorkspaceSelectionKeepsBackendInstalledAfterUISwitch(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceProfileTemplates(t, root, "antd", "ele", "naive")
	if err := os.WriteFile(filepath.Join(root, "admin", "pnpm-workspace.yaml"), []byte("packages:\n  - apps/*\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "admin", ".ui-profile.local.json"), []byte(`{"schema":1,"selectedUi":"antd","packageName":"@vben/web-antd","appDirectory":"apps/web-antd"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := NewFileProfileProvider(root)
	if err != nil {
		t.Fatal(err)
	}
	markerStore := NewFileMarkerStore(filepath.Join(root, ".runtime", "install", ".installed"))
	marker := installstate.Marker{
		SchemaVersion: installstate.CurrentSchemaVersion, InstallerVersion: "test",
		InstalledAt: time.Date(2026, time.August, 28, 0, 0, 0, 0, time.UTC),
		SelectedUI:  installstate.UIEle, Mode: installstate.ModeDev,
		ArtifactHash: strings.Repeat("a", 64), ManifestHash: strings.Repeat("b", 64),
	}
	if err := markerStore.Create(context.Background(), marker); err != nil {
		t.Fatal(err)
	}
	status, err := installer.NewStatusServiceWithProfile(markerStore, provider).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != installer.StateInstalled || !status.Installed || status.SelectedUI != installstate.UIAntd {
		t.Fatalf("Status() = %#v, want installed with active antd selection", status)
	}
}

func TestWorkspaceSwitchJournalKeepsPublishedBackendInstalled(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceProfileTemplates(t, root, "antd", "ele", "naive")
	adminRoot := filepath.Join(root, "admin")
	if err := os.WriteFile(filepath.Join(adminRoot, "pnpm-workspace.yaml"), []byte("packages:\n  - apps/*\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminRoot, ".ui-profile.local.json"), []byte(`{"schema":1,"selectedUi":"antd","packageName":"@vben/web-antd","appDirectory":"apps/web-antd"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, ".runtime", "install")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	journal := `{"schema":1,"owner":"admin-init-workspace","id":"12345678-1234-1234-1234-123456789abc","selectedUi":"naive","phase":"switching_ui","moves":[]}`
	if err := os.WriteFile(filepath.Join(state, "workspace-transaction.json"), []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}
	markerStore := NewFileMarkerStore(filepath.Join(state, ".installed"))
	marker := installstate.Marker{
		SchemaVersion: installstate.CurrentSchemaVersion, InstallerVersion: "test",
		InstalledAt: time.Date(2026, time.August, 28, 0, 0, 0, 0, time.UTC),
		SelectedUI:  installstate.UIAntd, Mode: installstate.ModeDev,
		ArtifactHash: strings.Repeat("a", 64), ManifestHash: strings.Repeat("b", 64),
	}
	if err := markerStore.Create(context.Background(), marker); err != nil {
		t.Fatal(err)
	}
	provider, err := NewFileProfileProvider(root)
	if err != nil {
		t.Fatal(err)
	}
	status, err := installer.NewStatusServiceWithProfile(markerStore, provider).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.State != installer.StateInstalled || !status.Installed || status.SelectedUI != installstate.UINaive || status.Phase != installer.InstallationPhaseUIPrepare || status.UIAction != installer.UIPreparationActionPrepare {
		t.Fatalf("Status() = %#v, want installed backend with a visible pending naive UI switch", status)
	}
}

func TestFileProfileProviderTreatsWorkspaceManifestAsAllTemplateContract(t *testing.T) {
	root := t.TempDir()
	adminRoot := filepath.Join(root, "admin")
	if err := os.MkdirAll(adminRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminRoot, "pnpm-workspace.yaml"), []byte("packages:\n  - apps/*\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, ui := range []string{"antd", "ele", "naive"} {
		app := filepath.Join(adminRoot, "apps", "web-"+ui)
		if err := os.MkdirAll(app, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(app, "package.json"), []byte(`{"name":"@vben/web-`+ui+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(adminRoot, ".ui-profile.json"), []byte(`{"schema":1,"selectedUi":"ele","packageName":"@vben/web-ele","appDirectory":"apps/web-ele"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := NewFileProfileProvider(root)
	if err != nil {
		t.Fatal(err)
	}
	profile, exists, err := provider.Profile(context.Background())
	if err != nil || !exists || profile.SelectedUI != installstate.UIEle || profile.Installing {
		t.Fatalf("Profile() = (%#v, %t, %v), want tracked ele profile in complete workspace", profile, exists, err)
	}
}

func TestTrackedSingleTemplateProfileRemainsLegacyWithWorkspaceManifest(t *testing.T) {
	root := t.TempDir()
	writeWorkspaceProfileTemplates(t, root, "ele")
	adminRoot := filepath.Join(root, "admin")
	if err := os.WriteFile(filepath.Join(adminRoot, "pnpm-workspace.yaml"), []byte("packages:\n  - apps/*\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(adminRoot, ".ui-profile.json"), []byte(`{"schema":1,"selectedUi":"ele","packageName":"@vben/web-ele","appDirectory":"apps/web-ele"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := NewFileProfileProvider(root)
	if err != nil {
		t.Fatal(err)
	}
	profile, exists, err := provider.Profile(context.Background())
	if err != nil || !exists || profile.SelectedUI != installstate.UIEle || profile.IndependentUISelection {
		t.Fatalf("Profile() = (%#v, %t, %v), want strict legacy ele profile", profile, exists, err)
	}
}

func TestFileProfileProviderReportsZeroMoveWorkspaceJournalAsUIPreparation(t *testing.T) {
	root := t.TempDir()
	for _, ui := range []string{"antd", "ele", "naive"} {
		app := filepath.Join(root, "admin", "apps", "web-"+ui)
		if err := os.MkdirAll(app, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(app, "package.json"), []byte(`{"name":"@vben/web-`+ui+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	state := filepath.Join(root, ".runtime", "install")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	journal := `{"schema":1,"owner":"admin-init-workspace","id":"12345678-1234-1234-1234-123456789abc","selectedUi":"naive","phase":"dependencies_pending","moves":[]}`
	if err := os.WriteFile(filepath.Join(state, "workspace-transaction.json"), []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err := NewFileProfileProvider(root)
	if err != nil {
		t.Fatal(err)
	}
	profile, exists, err := provider.Profile(context.Background())
	if err != nil || !exists || profile.SelectedUI != installstate.UINaive || !profile.Installing || !profile.PreparingUI || profile.UIAction != installer.UIPreparationActionPrepare {
		t.Fatalf("Profile() = (%#v, %t, %v), want zero-move preparation", profile, exists, err)
	}
}
