package installplatform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSyncDirectoryForWindowsValidatesThePathWithoutFlushingADirectoryHandle(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := syncDirectoryForPlatform(root, "windows"); err != nil {
		t.Fatalf("syncDirectoryForPlatform(directory, windows) error = %v", err)
	}
	file := filepath.Join(root, "regular-file")
	if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syncDirectoryForPlatform(file, "windows"); err == nil {
		t.Fatal("syncDirectoryForPlatform(file, windows) error = nil")
	}
	if err := syncDirectoryForPlatform(filepath.Join(root, "missing"), "windows"); err == nil {
		t.Fatal("syncDirectoryForPlatform(missing, windows) error = nil")
	}
}
