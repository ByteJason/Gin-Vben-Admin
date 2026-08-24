package webassets

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallerSourceExposesOnlyTheInstallerSubtree(t *testing.T) {
	workspaceRoot := installerWorkspaceFixture(t, map[string]string{
		"index.html": "<h1>installer</h1>",
		"app.js":     "console.log('installer')",
		"styles.css": "body { color: green; }",
		"secret.txt": "not public",
	})
	if err := os.WriteFile(filepath.Join(workspaceRoot, "secret.txt"), []byte("not public"), 0o644); err != nil {
		t.Fatal(err)
	}

	assets, err := InstallerSource(workspaceRoot)
	if err != nil {
		t.Fatalf("InstallerSource() error = %v", err)
	}
	contents, err := fs.ReadFile(assets, "install/index.html")
	if err != nil || !strings.Contains(string(contents), "installer") {
		t.Fatalf("ReadFile(install/index.html) = %q, %v", contents, err)
	}
	for _, name := range []string{"install", "install/assets", "secret.txt", "install/secret.txt", "install/../secret.txt", "admin/apps/install/src/index.html"} {
		if contents, err := fs.ReadFile(assets, name); err == nil {
			t.Fatalf("ReadFile(%q) = %q, want unavailable", name, contents)
		}
	}
}

func TestInstallerSourceRequiresTheCompletePublicEntrySet(t *testing.T) {
	for _, missing := range []string{"index.html", "app.js", "styles.css"} {
		t.Run(missing, func(t *testing.T) {
			files := map[string]string{
				"index.html": "<h1>installer</h1>",
				"app.js":     "console.log('installer')",
				"styles.css": "body { color: green; }",
			}
			delete(files, missing)
			workspaceRoot := installerWorkspaceFixture(t, files)
			if assets, err := InstallerSource(workspaceRoot); err == nil || assets != nil {
				t.Fatalf("InstallerSource() = (%T, %v), want missing %s rejected", assets, err, missing)
			}
		})
	}
}

func TestInstallerSourceRejectsSymlinkedPathComponent(t *testing.T) {
	workspaceRoot := t.TempDir()
	outside := t.TempDir()
	source := filepath.Join(outside, "src")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "index.html"), []byte("outside installer"), 0o644); err != nil {
		t.Fatal(err)
	}
	apps := filepath.Join(workspaceRoot, "admin", "apps")
	if err := os.MkdirAll(apps, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(apps, "install")); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}

	if assets, err := InstallerSource(workspaceRoot); err == nil || assets != nil {
		t.Fatalf("InstallerSource() = (%T, %v), want rejected symlink", assets, err)
	}
}

func TestInstallerSourceRejectsSymlinkedEntry(t *testing.T) {
	workspaceRoot := installerWorkspaceFixture(t, map[string]string{
		"index.html": "<h1>installer</h1>",
		"styles.css": "body { color: green; }",
	})
	target := filepath.Join(t.TempDir(), "app.js")
	if err := os.WriteFile(target, []byte("outside script"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(workspaceRoot, filepath.FromSlash(installerSourceDirectory), "app.js")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}

	if assets, err := InstallerSource(workspaceRoot); err == nil || assets != nil {
		t.Fatalf("InstallerSource() = (%T, %v), want rejected symlink", assets, err)
	}
}

func installerWorkspaceFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	workspaceRoot := t.TempDir()
	source := filepath.Join(workspaceRoot, filepath.FromSlash(installerSourceDirectory))
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(source, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return workspaceRoot
}
