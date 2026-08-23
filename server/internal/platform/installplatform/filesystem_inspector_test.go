package installplatform

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileSystemInspectorReportsAllowlistedPathCapabilities(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "admin", "apps", "install"))
	mustMkdirAll(t, filepath.Join(root, "admin", "apps", "web-antd"))
	mustMkdirAll(t, filepath.Join(root, "admin", "apps"))

	inspector, err := NewFileSystemInspector(root)
	if err != nil {
		t.Fatalf("NewFileSystemInspector() error = %v", err)
	}
	got, err := inspector.Inspect(context.Background(), "admin/apps/web-antd")
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if !got.CanRead || !got.CanWrite || !got.CanCreate || !got.CanRename || !got.CanDelete {
		t.Fatalf("permissions = %#v, want all capabilities", got)
	}
	if strings.Contains(strings.Join(got.Reasons, " "), root) {
		t.Fatalf("reasons leak absolute root: %#v", got.Reasons)
	}
}

func TestFileSystemInspectorUsesOnlyTheAdminInstallerWorkspace(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "admin", "apps", "install"))
	inspector, err := NewFileSystemInspector(root)
	if err != nil {
		t.Fatalf("NewFileSystemInspector() error = %v", err)
	}
	if _, err := inspector.Inspect(context.Background(), "admin/apps/install"); err != nil {
		t.Fatalf("Inspect(admin/apps/install) error = %v", err)
	}
	for _, legacy := range []string{"install", "admin/apps/web"} {
		if _, err := inspector.Inspect(context.Background(), legacy); err == nil {
			t.Fatalf("Inspect(%q) error = nil, want legacy path rejection", legacy)
		}
	}
}

func TestFileSystemInspectorRejectsTraversalAndUnallowlistedPaths(t *testing.T) {
	inspector, err := NewFileSystemInspector(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileSystemInspector() error = %v", err)
	}
	for _, path := range []string{"../outside", ".env/../outside", "server/config.yaml", "/tmp/outside"} {
		if _, err := inspector.Inspect(context.Background(), path); err == nil {
			t.Fatalf("Inspect(%q) error = nil, want rejection", path)
		}
	}
}

func TestFileSystemInspectorRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "admin", "apps"))
	target := t.TempDir()
	link := filepath.Join(root, "admin", "apps", "web-ele")
	if err := os.Symlink(target, link); err != nil {
		if os.IsPermission(err) {
			t.Skipf("symlink creation unavailable: %v", err)
		}
		t.Fatalf("Symlink() error = %v", err)
	}
	inspector, err := NewFileSystemInspector(root)
	if err != nil {
		t.Fatalf("NewFileSystemInspector() error = %v", err)
	}
	if _, err := inspector.Inspect(context.Background(), "admin/apps/web-ele"); err == nil {
		t.Fatal("Inspect(symlink) error = nil, want rejection")
	}
}

func TestFileSystemInspectorHonorsCancellationBeforeProbe(t *testing.T) {
	inspector, err := NewFileSystemInspector(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileSystemInspector() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := inspector.Inspect(ctx, "admin/apps/install"); err == nil {
		t.Fatal("Inspect(canceled) error = nil, want context cancellation")
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
}
