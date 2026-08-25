package installplatform

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
)

func TestFileProfileProviderReadsTheTrackedUIProfile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteProfileFixture(t, root, `{
  "schema": 1,
  "selectedUi": "naive",
  "packageName": "@vben/web-naive",
  "appDirectory": "apps/web-naive"
}`)
	if err := os.MkdirAll(filepath.Join(root, "admin", "apps", "web-naive"), 0o755); err != nil {
		t.Fatal(err)
	}

	provider, err := NewFileProfileProvider(root)
	if err != nil {
		t.Fatalf("NewFileProfileProvider() error = %v", err)
	}
	profile, exists, err := provider.Profile(context.Background())
	if err != nil {
		t.Fatalf("Profile() error = %v", err)
	}
	if !exists || profile.SelectedUI != installstate.UINaive || profile.Installing {
		t.Fatalf("Profile() = (%#v, %t), want naive prepared profile", profile, exists)
	}
}

func TestFileProfileProviderDoesNotReportReadyWhileAdminInitTransactionIsPending(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteProfileFixture(t, root, `{"schema":1,"selectedUi":"ele","packageName":"@vben/web-ele","appDirectory":"apps/web-ele"}`)
	if err := os.MkdirAll(filepath.Join(root, "admin", "apps", "web-ele"), 0o755); err != nil {
		t.Fatal(err)
	}
	transactionPath := filepath.Join(root, ".runtime", "install", "transaction.json")
	if err := os.MkdirAll(filepath.Dir(transactionPath), 0o700); err != nil {
		t.Fatal(err)
	}
	pending := `{"schema":1,"owner":"admin-init","id":"12345678-1234-1234-1234-123456789abc","selectedUi":"ele","phase":"dependencies_pending","moves":[{"source":"apps/web-antd","backup":"apps/web-antd"},{"source":"apps/web-naive","backup":"apps/web-naive"}]}` + "\n"
	if err := os.WriteFile(transactionPath, []byte(pending), 0o600); err != nil {
		t.Fatal(err)
	}

	provider, err := NewFileProfileProvider(root)
	if err != nil {
		t.Fatalf("NewFileProfileProvider() error = %v", err)
	}
	profile, exists, err := provider.Profile(context.Background())
	if err != nil || !exists || profile.SelectedUI != installstate.UIEle || !profile.Installing || !profile.PreparingUI || profile.UIAction != installer.UIPreparationActionPrepare {
		t.Fatalf("Profile() with pending admin transaction = (%#v, %t, %v), want active ele preparation", profile, exists, err)
	}
	statusService := installer.NewStatusServiceWithProfile(
		NewFileMarkerStore(filepath.Join(root, ".runtime", "install", ".installed")),
		provider,
	)
	status, err := statusService.Status(context.Background())
	if err != nil || status.State != installer.StateInstalling || status.Phase != installer.InstallationPhaseUIPrepare || status.UIAction != installer.UIPreparationActionPrepare {
		t.Fatalf("Status() with pending admin transaction = (%#v, %v), want preparing install status", status, err)
	}
}

func TestFileProfileProviderUsesConfiguredInstallStateDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteProfileFixture(t, root, `{"schema":1,"selectedUi":"antd","packageName":"@vben/web-antd","appDirectory":"apps/web-antd"}`)
	if err := os.MkdirAll(filepath.Join(root, "admin", "apps", "web-antd"), 0o755); err != nil {
		t.Fatal(err)
	}
	stateDirectory := t.TempDir()
	pending := `{"schema":1,"owner":"admin-init","id":"12345678-1234-1234-1234-123456789abc","selectedUi":"antd","phase":"dependencies_pending","moves":[{"source":"apps/web-ele","backup":"apps/web-ele"},{"source":"apps/web-naive","backup":"apps/web-naive"}]}` + "\n"
	if err := os.WriteFile(filepath.Join(stateDirectory, "transaction.json"), []byte(pending), 0o600); err != nil {
		t.Fatal(err)
	}

	provider, err := NewFileProfileProviderWithStateDirectory(root, stateDirectory)
	if err != nil {
		t.Fatalf("NewFileProfileProviderWithStateDirectory() error = %v", err)
	}
	profile, exists, err := provider.Profile(context.Background())
	if err != nil || !exists || profile.SelectedUI != installstate.UIAntd || !profile.Installing || !profile.PreparingUI || profile.UIAction != installer.UIPreparationActionPrepare {
		t.Fatalf("Profile() with configured pending transaction = (%#v, %t, %v), want active antd preparation", profile, exists, err)
	}
}

func TestFileProfileProviderRecognizesStrictAdminInitTransactionPhases(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		phase       string
		withProfile bool
		wantAction  installer.UIPreparationAction
	}{
		{phase: "moving_ui", wantAction: installer.UIPreparationActionPrepare},
		{phase: "dependencies_pending", withProfile: true, wantAction: installer.UIPreparationActionPrepare},
		{phase: "resetting_ui", withProfile: true, wantAction: installer.UIPreparationActionReset},
	} {
		t.Run(testCase.phase, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "admin", "apps", "web-ele"), 0o755); err != nil {
				t.Fatal(err)
			}
			if testCase.withProfile {
				mustWriteProfileFixture(t, root, `{"schema":1,"selectedUi":"ele","packageName":"@vben/web-ele","appDirectory":"apps/web-ele"}`)
			}
			mustWriteAdminInitTransactionFixture(t, filepath.Join(root, ".runtime", "install"), `{"schema":1,"owner":"admin-init","id":"12345678-1234-1234-1234-123456789abc","selectedUi":"ele","phase":"`+testCase.phase+`","moves":[{"source":"apps/web-antd","backup":"apps/web-antd"},{"source":"apps/web-naive","backup":"apps/web-naive"}]}`)

			provider, err := NewFileProfileProvider(root)
			if err != nil {
				t.Fatal(err)
			}
			profile, exists, err := provider.Profile(context.Background())
			if err != nil || !exists || profile.SelectedUI != installstate.UIEle || !profile.Installing || !profile.PreparingUI || profile.UIAction != testCase.wantAction {
				t.Fatalf("Profile() = (%#v, %t, %v), want active %s", profile, exists, err, testCase.wantAction)
			}
			status, err := installer.NewStatusServiceWithProfile(
				NewFileMarkerStore(filepath.Join(root, ".runtime", "install", ".installed")),
				provider,
			).Status(context.Background())
			if err != nil || status.State != installer.StateInstalling || status.Phase != installer.InstallationPhaseUIPrepare || status.UIAction != testCase.wantAction {
				t.Fatalf("Status() = (%#v, %v), want ui_prepare/%s", status, err, testCase.wantAction)
			}
		})
	}
}

func TestFileProfileProviderRejectsMalformedAdminInitTransactions(t *testing.T) {
	t.Parallel()

	validMoves := `[{"source":"apps/web-antd","backup":"apps/web-antd"},{"source":"apps/web-naive","backup":"apps/web-naive"}]`
	for _, testCase := range []struct {
		name        string
		transaction string
	}{
		{name: "unknown field", transaction: `{"schema":1,"owner":"admin-init","id":"12345678-1234-1234-1234-123456789abc","selectedUi":"ele","phase":"moving_ui","moves":` + validMoves + `,"command":"private"}`},
		{name: "invalid id", transaction: `{"schema":1,"owner":"admin-init","id":"fixture","selectedUi":"ele","phase":"moving_ui","moves":` + validMoves + `}`},
		{name: "invalid ui", transaction: `{"schema":1,"owner":"admin-init","id":"12345678-1234-1234-1234-123456789abc","selectedUi":"other","phase":"moving_ui","moves":` + validMoves + `}`},
		{name: "invalid phase", transaction: `{"schema":1,"owner":"admin-init","id":"12345678-1234-1234-1234-123456789abc","selectedUi":"ele","phase":"complete","moves":` + validMoves + `}`},
		{name: "wrong moves", transaction: `{"schema":1,"owner":"admin-init","id":"12345678-1234-1234-1234-123456789abc","selectedUi":"ele","phase":"moving_ui","moves":[]}`},
		{name: "duplicate trailing value", transaction: `{"schema":1,"owner":"admin-init","id":"12345678-1234-1234-1234-123456789abc","selectedUi":"ele","phase":"moving_ui","moves":` + validMoves + `} {}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			mustWriteProfileFixture(t, root, `{"schema":1,"selectedUi":"ele","packageName":"@vben/web-ele","appDirectory":"apps/web-ele"}`)
			if err := os.MkdirAll(filepath.Join(root, "admin", "apps", "web-ele"), 0o755); err != nil {
				t.Fatal(err)
			}
			mustWriteAdminInitTransactionFixture(t, filepath.Join(root, ".runtime", "install"), testCase.transaction)
			provider, err := NewFileProfileProvider(root)
			if err != nil {
				t.Fatal(err)
			}
			profile, exists, err := provider.Profile(context.Background())
			if err != nil || !exists || profile != (installer.InstallationProfile{}) {
				t.Fatalf("Profile() = (%#v, %t, %v), want inconsistent existing profile", profile, exists, err)
			}
		})
	}
}

func TestFileProfileProviderRejectsServerTransactionForDifferentUI(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteProfileFixture(t, root, `{"schema":1,"selectedUi":"ele","packageName":"@vben/web-ele","appDirectory":"apps/web-ele"}`)
	if err := os.MkdirAll(filepath.Join(root, "admin", "apps", "web-ele"), 0o755); err != nil {
		t.Fatal(err)
	}
	transaction := installer.ApplyTransaction{
		Schema: installer.ApplyTransactionSchema, Owner: installer.ApplyTransactionOwner,
		ID: "install-0123456789abcdef0123456789abcdef", SelectedUI: installstate.UINaive,
		Mode: installstate.ModeDev, DatabaseTarget: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		Phase: installer.TransactionApplying, CurrentStep: "schema", CompletedSteps: []string{"plan", "database", "redis"},
		UpdatedAt: time.Date(2026, time.August, 24, 10, 0, 0, 0, time.UTC),
	}
	encoded, err := json.Marshal(transaction)
	if err != nil {
		t.Fatal(err)
	}
	transactionPath := filepath.Join(root, ".runtime", "install", "transaction.json")
	if err := os.MkdirAll(filepath.Dir(transactionPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transactionPath, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	provider, err := NewFileProfileProvider(root)
	if err != nil {
		t.Fatal(err)
	}
	profile, exists, err := provider.Profile(context.Background())
	if err != nil || !exists || profile.SelectedUI != "" {
		t.Fatalf("Profile() with mismatched server transaction = (%#v, %t, %v), want invalid existing profile", profile, exists, err)
	}
}

func TestFileProfileProviderDistinguishesPristineFromInconsistent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	provider, err := NewFileProfileProvider(root)
	if err != nil {
		t.Fatalf("NewFileProfileProvider() error = %v", err)
	}
	if profile, exists, err := provider.Profile(context.Background()); err != nil || exists || profile.SelectedUI != "" {
		t.Fatalf("pristine Profile() = (%#v, %t, %v)", profile, exists, err)
	}

	mustWriteProfileFixture(t, root, `{"schema":1,"selectedUi":"antd"}`)
	if profile, exists, err := provider.Profile(context.Background()); err != nil || !exists || profile.SelectedUI != "" {
		t.Fatalf("damaged Profile() = (%#v, %t, %v), want invalid existing profile", profile, exists, err)
	}
}

func TestFileProfileProviderRejectsExtraTemplatesAndSymlinks(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{
			name: "extra template",
			setup: func(t *testing.T, root string) {
				mustWriteProfileFixture(t, root, `{"schema":1,"selectedUi":"antd","packageName":"@vben/web-antd","appDirectory":"apps/web-antd"}`)
				for _, ui := range []string{"antd", "ele"} {
					if err := os.MkdirAll(filepath.Join(root, "admin", "apps", "web-"+ui), 0o755); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.WriteFile(filepath.Join(root, "admin", "apps", "web-ele", "package.json"), []byte(`{"name":"@vben/web-ele"}`), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "selected template symlink",
			setup: func(t *testing.T, root string) {
				mustWriteProfileFixture(t, root, `{"schema":1,"selectedUi":"ele","packageName":"@vben/web-ele","appDirectory":"apps/web-ele"}`)
				target := t.TempDir()
				if err := os.MkdirAll(filepath.Join(root, "admin", "apps"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(root, "admin", "apps", "web-ele")); err != nil {
					if os.IsPermission(err) {
						t.Skipf("symlinks unavailable: %v", err)
					}
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			testCase.setup(t, root)
			provider, err := NewFileProfileProvider(root)
			if err != nil {
				t.Fatalf("NewFileProfileProvider() error = %v", err)
			}
			profile, exists, err := provider.Profile(context.Background())
			if err != nil || !exists || profile.SelectedUI != "" {
				t.Fatalf("Profile() = (%#v, %t, %v), want invalid existing profile", profile, exists, err)
			}
		})
	}
}

func TestFileProfileProviderAllowsPulledFragmentsWithoutAnUnselectedPackageManifest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteProfileFixture(t, root, `{"schema":1,"selectedUi":"ele","packageName":"@vben/web-ele","appDirectory":"apps/web-ele"}`)
	if err := os.MkdirAll(filepath.Join(root, "admin", "apps", "web-ele"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, ui := range []string{"antd", "naive"} {
		directory := filepath.Join(root, "admin", "apps", "web-"+ui, "src", "api", "core")
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "pulled_update.go.fixture"), []byte("tracked update\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	provider, err := NewFileProfileProvider(root)
	if err != nil {
		t.Fatalf("NewFileProfileProvider() error = %v", err)
	}
	profile, exists, err := provider.Profile(context.Background())
	if err != nil || !exists || profile.SelectedUI != installstate.UIEle || profile.Installing {
		t.Fatalf("Profile() = (%#v, %t, %v), want ele profile with harmless pulled fragments", profile, exists, err)
	}

	markerStore := NewFileMarkerStore(filepath.Join(root, ".runtime", "install", ".installed"))
	marker := installstate.Marker{
		SchemaVersion:    installstate.CurrentSchemaVersion,
		InstallerVersion: installer.CurrentInstallerVersion,
		InstalledAt:      time.Date(2026, time.August, 25, 0, 0, 0, 0, time.UTC),
		SelectedUI:       installstate.UIEle,
		Mode:             installstate.ModeDev,
		ArtifactHash:     strings.Repeat("a", 64),
		ManifestHash:     strings.Repeat("b", 64),
	}
	if err := markerStore.Create(context.Background(), marker); err != nil {
		t.Fatalf("Create(marker) error = %v", err)
	}
	status, err := installer.NewStatusServiceWithProfile(markerStore, provider).Status(context.Background())
	if err != nil || !status.Installed || status.State != installer.StateInstalled || status.SelectedUI != installstate.UIEle {
		t.Fatalf("Status() = (%#v, %v), want installed ele after pull", status, err)
	}
}

func mustWriteProfileFixture(t *testing.T, root, contents string) {
	t.Helper()
	path := filepath.Join(root, "admin", ".ui-profile.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustWriteAdminInitTransactionFixture(t *testing.T, stateDirectory, contents string) {
	t.Helper()
	if err := os.MkdirAll(stateDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDirectory, "transaction.json"), []byte(contents+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
