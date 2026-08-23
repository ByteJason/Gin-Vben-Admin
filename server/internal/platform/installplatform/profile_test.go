package installplatform

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
