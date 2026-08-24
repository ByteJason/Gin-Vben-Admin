package installplatform_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/installplatform"
)

func TestFileTransactionJournalCleansOnlyTheUniqueValidatedAdminInitBackup(t *testing.T) {
	root := t.TempDir()
	writeSelectedProfileFixture(t, root, "ele")
	stateDir := filepath.Join(root, ".runtime", "install")
	backupRoot := filepath.Join(stateDir, "ui-backup")
	transactionID := "12345678-1234-1234-1234-123456789abc"
	transactionDir := filepath.Join(backupRoot, transactionID)
	for _, app := range []string{"web-antd", "web-naive"} {
		path := filepath.Join(transactionDir, "apps", app)
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "fixture.txt"), []byte(app), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	receipt := map[string]any{
		"schema": 1, "owner": "admin-init", "transactionId": transactionID,
		"selectedUi": "ele", "dependenciesReady": true,
		"moves": []map[string]string{
			{"source": "apps/web-antd", "backup": "apps/web-antd"},
			{"source": "apps/web-naive", "backup": "apps/web-naive"},
		},
	}
	writeJSONFixture(t, filepath.Join(transactionDir, "receipt.json"), receipt)
	unknown := filepath.Join(backupRoot, "keep-unknown")
	if err := os.MkdirAll(unknown, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unknown, "keep.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	journal := installplatform.NewFileTransactionJournal(filepath.Join(stateDir, "transaction.json"), root)
	if err := journal.CleanupCompleted(context.Background(), cleanupMarker("ele")); err != nil {
		t.Fatalf("CleanupCompleted() error = %v", err)
	}
	if _, err := os.Lstat(transactionDir); !os.IsNotExist(err) {
		t.Fatalf("validated transaction remains: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(unknown, "keep.txt")); err != nil || string(got) != "keep" {
		t.Fatalf("unknown backup changed: %q error=%v", got, err)
	}
	if info, err := os.Lstat(backupRoot); err != nil || !info.IsDir() {
		t.Fatalf("ui-backup root was removed: info=%v error=%v", info, err)
	}
}

func TestFileTransactionJournalPreservesBackupWithUnexpectedTopLevelEntry(t *testing.T) {
	root := t.TempDir()
	writeSelectedProfileFixture(t, root, "ele")
	stateDir := filepath.Join(root, ".runtime", "install")
	transactionID := "12345678-1234-1234-1234-123456789abc"
	transactionDir := filepath.Join(stateDir, "ui-backup", transactionID)
	for _, app := range []string{"web-antd", "web-naive"} {
		if err := os.MkdirAll(filepath.Join(transactionDir, "apps", app), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeJSONFixture(t, filepath.Join(transactionDir, "receipt.json"), map[string]any{
		"schema": 1, "owner": "admin-init", "transactionId": transactionID,
		"selectedUi": "ele", "dependenciesReady": true,
		"moves": []map[string]string{
			{"source": "apps/web-antd", "backup": "apps/web-antd"},
			{"source": "apps/web-naive", "backup": "apps/web-naive"},
		},
	})
	if err := os.WriteFile(filepath.Join(transactionDir, "unexpected.txt"), []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal := installplatform.NewFileTransactionJournal(filepath.Join(stateDir, "transaction.json"), root)
	if err := journal.CleanupCompleted(context.Background(), cleanupMarker("ele")); err != nil {
		t.Fatalf("CleanupCompleted() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(transactionDir, "unexpected.txt")); err != nil {
		t.Fatalf("invalid transaction was removed: %v", err)
	}
}

func TestFileTransactionJournalPreservesBackupWithNonCanonicalTransactionID(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeSelectedProfileFixture(t, root, "ele")
	stateDir := filepath.Join(root, ".runtime", "install")
	transactionID := "12345678-1234-1234-1234-123456789ABC"
	transactionDir := filepath.Join(stateDir, "ui-backup", transactionID)
	for _, app := range []string{"web-antd", "web-naive"} {
		if err := os.MkdirAll(filepath.Join(transactionDir, "apps", app), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeJSONFixture(t, filepath.Join(transactionDir, "receipt.json"), map[string]any{
		"schema": 1, "owner": "admin-init", "transactionId": transactionID,
		"selectedUi": "ele", "dependenciesReady": true,
		"moves": []map[string]string{
			{"source": "apps/web-antd", "backup": "apps/web-antd"},
			{"source": "apps/web-naive", "backup": "apps/web-naive"},
		},
	})

	journal := installplatform.NewFileTransactionJournal(filepath.Join(stateDir, "transaction.json"), root)
	if err := journal.CleanupCompleted(context.Background(), cleanupMarker("ele")); err != nil {
		t.Fatalf("CleanupCompleted() error = %v", err)
	}
	if _, err := os.Stat(transactionDir); err != nil {
		t.Fatalf("non-canonical transaction backup was removed: %v", err)
	}
}

func TestFileTransactionJournalResumesInterruptedTombstoneCleanup(t *testing.T) {
	root := t.TempDir()
	writeSelectedProfileFixture(t, root, "ele")
	stateDir := filepath.Join(root, ".runtime", "install")
	backupRoot := filepath.Join(stateDir, "ui-backup")
	transactionID := "12345678-1234-1234-1234-123456789abc"
	tombstone := filepath.Join(backupRoot, ".cleanup-ele-"+transactionID)
	// A prior process already deleted receipt.json and one app before stopping.
	remaining := filepath.Join(tombstone, "apps", "web-naive")
	if err := os.MkdirAll(remaining, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remaining, "fixture.txt"), []byte("remaining"), 0o600); err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(backupRoot, "keep-unknown")
	if err := os.MkdirAll(unknown, 0o700); err != nil {
		t.Fatal(err)
	}

	journal := installplatform.NewFileTransactionJournal(filepath.Join(stateDir, "transaction.json"), root)
	if err := journal.CleanupCompleted(context.Background(), cleanupMarker("ele")); err != nil {
		t.Fatalf("CleanupCompleted() error = %v", err)
	}
	if _, err := os.Lstat(tombstone); !os.IsNotExist(err) {
		t.Fatalf("interrupted cleanup tombstone remains: %v", err)
	}
	if info, err := os.Lstat(unknown); err != nil || !info.IsDir() {
		t.Fatalf("unknown backup changed: info=%v err=%v", info, err)
	}
}

func TestFileTransactionJournalPreservesInvalidCleanupTombstone(t *testing.T) {
	root := t.TempDir()
	writeSelectedProfileFixture(t, root, "ele")
	stateDir := filepath.Join(root, ".runtime", "install")
	tombstone := filepath.Join(stateDir, "ui-backup", ".cleanup-ele-12345678-1234-1234-1234-123456789abc")
	if err := os.MkdirAll(tombstone, 0o700); err != nil {
		t.Fatal(err)
	}
	unexpected := filepath.Join(tombstone, "unexpected.txt")
	if err := os.WriteFile(unexpected, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}

	journal := installplatform.NewFileTransactionJournal(filepath.Join(stateDir, "transaction.json"), root)
	if err := journal.CleanupCompleted(context.Background(), cleanupMarker("ele")); err != nil {
		t.Fatalf("CleanupCompleted() error = %v", err)
	}
	if got, err := os.ReadFile(unexpected); err != nil || string(got) != "preserve" {
		t.Fatalf("invalid cleanup tombstone changed: got=%q err=%v", got, err)
	}
}

func writeSelectedProfileFixture(t *testing.T, root, selected string) {
	t.Helper()
	definitions := map[string][2]string{
		"antd":  {"@vben/web-antd", "apps/web-antd"},
		"ele":   {"@vben/web-ele", "apps/web-ele"},
		"naive": {"@vben/web-naive", "apps/web-naive"},
	}
	definition := definitions[selected]
	if err := os.MkdirAll(filepath.Join(root, "admin", filepath.FromSlash(definition[1])), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONFixture(t, filepath.Join(root, "admin", ".ui-profile.json"), map[string]any{
		"schema": 1, "selectedUi": selected, "packageName": definition[0], "appDirectory": definition[1],
	})
}

func writeJSONFixture(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func cleanupMarker(ui installstate.UI) installstate.Marker {
	return installstate.Marker{
		SchemaVersion: installstate.CurrentSchemaVersion, InstallerVersion: "0.4.0-dev",
		InstalledAt: time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC),
		SelectedUI:  ui, Mode: installstate.ModeDev,
		ArtifactHash: strings.Repeat("a", 64), ManifestHash: strings.Repeat("b", 64),
	}
}
