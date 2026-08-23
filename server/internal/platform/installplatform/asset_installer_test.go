package installplatform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
)

func TestAssetInstallerBuildsTheSelectedProfileWithoutRenamingItsWorkspace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	selected := filepath.Join(root, "admin", "apps", "web-ele")
	if err := os.MkdirAll(selected, 0o755); err != nil {
		t.Fatalf("MkdirAll(selected) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(selected, "fixture.txt"), []byte("ele"), 0o644); err != nil {
		t.Fatalf("WriteFile(selected) error = %v", err)
	}
	artifact := sha256.Sum256([]byte("selected-ui-dist"))
	builder := &assetBuilderStub{root: root, digest: hex.EncodeToString(artifact[:])}
	backupRoot := filepath.Join(root, "admin", "apps", "install", ".install-backup")
	service := NewAssetInstaller(
		root,
		backupRoot,
		builder,
		bytes.NewReader(bytes.Repeat([]byte{0x4d}, 64)),
	)
	plan := installer.Plan{
		SelectedUI: installstate.UIEle, Mode: installstate.ModeEmbedded,
		CanCleanup: true, CanBuild: true, CanWriteEnv: true,
	}
	receipt, err := service.Prepare(context.Background(), plan)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if !builder.calledWithSelectedOnly {
		t.Fatal("builder did not receive the retained selected workspace")
	}
	if receipt.ArtifactHash != builder.digest || len(receipt.ManifestHash) != 64 || receipt.Reference == "" {
		t.Fatalf("receipt = %#v", receipt)
	}
	if got, err := os.ReadFile(filepath.Join(selected, "fixture.txt")); err != nil || string(got) != "ele" {
		t.Fatalf("selected app = %q, error=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(backupRoot, receipt.Reference, "transaction.json")); err != nil {
		t.Fatalf("transaction receipt missing: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "admin", "apps", "web")); !os.IsNotExist(err) {
		t.Fatalf("canonical rename unexpectedly exists: %v", err)
	}
	if err := service.Rollback(context.Background(), receipt); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(backupRoot, receipt.Reference)); !os.IsNotExist(err) {
		t.Fatalf("transaction receipt remains after rollback: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(selected, "fixture.txt")); err != nil || string(got) != "ele" {
		t.Fatalf("selected app after rollback = %q, error=%v", got, err)
	}
}

func TestAssetInstallerRollbackKeepsReceiptWhenTransactionDirectoryHasUnexpectedFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	selected := filepath.Join(root, "admin", "apps", "web-ele")
	if err := os.MkdirAll(selected, 0o755); err != nil {
		t.Fatalf("MkdirAll(selected) error = %v", err)
	}
	artifact := sha256.Sum256([]byte("selected-ui-dist"))
	service := NewAssetInstaller(
		root,
		filepath.Join(root, "admin", "apps", "install", ".install-backup"),
		&assetBuilderStub{root: root, digest: hex.EncodeToString(artifact[:])},
		bytes.NewReader(bytes.Repeat([]byte{0x5e}, 64)),
	)
	receipt, err := service.Prepare(context.Background(), installer.Plan{
		SelectedUI:  installstate.UIEle,
		Mode:        installstate.ModeEmbedded,
		CanCleanup:  true,
		CanBuild:    true,
		CanWriteEnv: true,
	})
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	transactionDir := filepath.Join(root, "admin", "apps", "install", ".install-backup", receipt.Reference)
	manifestPath := filepath.Join(transactionDir, "transaction.json")
	if err := os.WriteFile(filepath.Join(transactionDir, "unexpected.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatalf("WriteFile(unexpected) error = %v", err)
	}

	if err := service.Rollback(context.Background(), receipt); !errors.Is(err, ErrAssetInstallation) {
		t.Fatalf("Rollback() error = %v, want ErrAssetInstallation", err)
	}
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("transaction receipt should remain retryable: %v", err)
	}
}

type assetBuilderStub struct {
	root                   string
	digest                 string
	calledWithSelectedOnly bool
}

func (s *assetBuilderStub) Build(_ context.Context, ui installstate.UI, mode installstate.Mode) (string, error) {
	selectedOnly := true
	for _, candidate := range []string{"antd", "ele", "naive"} {
		info, err := os.Stat(filepath.Join(s.root, "admin", "apps", "web-"+candidate))
		if candidate == "ele" {
			selectedOnly = selectedOnly && err == nil && info.IsDir()
		} else {
			selectedOnly = selectedOnly && os.IsNotExist(err)
		}
	}
	s.calledWithSelectedOnly = selectedOnly && ui == installstate.UIEle && mode == installstate.ModeEmbedded
	return s.digest, nil
}
