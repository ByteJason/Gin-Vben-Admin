package installplatform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
)

func TestAssetInstallerBuildsBeforeStagingAndRestoresAllTemplates(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, ui := range []string{"antd", "ele", "naive"} {
		path := filepath.Join(root, "admin", "apps", "web-"+ui)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", ui, err)
		}
		if err := os.WriteFile(filepath.Join(path, "fixture.txt"), []byte(ui), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", ui, err)
		}
	}
	artifact := sha256.Sum256([]byte("selected-ui-dist"))
	builder := &assetBuilderStub{root: root, digest: hex.EncodeToString(artifact[:])}
	service := NewAssetInstaller(
		root,
		filepath.Join(root, "install", ".install-backup"),
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
	if !builder.calledWithAllTemplates {
		t.Fatal("builder did not run before template staging")
	}
	if receipt.ArtifactHash != builder.digest || len(receipt.ManifestHash) != 64 || receipt.Reference == "" {
		t.Fatalf("receipt = %#v", receipt)
	}
	if got, err := os.ReadFile(filepath.Join(root, "admin", "apps", "web", "fixture.txt")); err != nil || string(got) != "ele" {
		t.Fatalf("selected canonical app = %q, error=%v", got, err)
	}
	for _, removed := range []string{"web-antd", "web-ele", "web-naive"} {
		if _, err := os.Lstat(filepath.Join(root, "admin", "apps", removed)); !os.IsNotExist(err) {
			t.Fatalf("template %s remains after staging: %v", removed, err)
		}
	}
	if err := service.Rollback(context.Background(), receipt); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "admin", "apps", "web")); !os.IsNotExist(err) {
		t.Fatalf("canonical web remains after rollback: %v", err)
	}
	for _, ui := range []string{"antd", "ele", "naive"} {
		got, err := os.ReadFile(filepath.Join(root, "admin", "apps", "web-"+ui, "fixture.txt"))
		if err != nil || strings.TrimSpace(string(got)) != ui {
			t.Fatalf("restored %s fixture = %q, error=%v", ui, got, err)
		}
	}
}

type assetBuilderStub struct {
	root                   string
	digest                 string
	calledWithAllTemplates bool
}

func (s *assetBuilderStub) Build(_ context.Context, ui installstate.UI, mode installstate.Mode) (string, error) {
	allPresent := true
	for _, candidate := range []string{"antd", "ele", "naive"} {
		if info, err := os.Stat(filepath.Join(s.root, "admin", "apps", "web-"+candidate)); err != nil || !info.IsDir() {
			allPresent = false
		}
	}
	s.calledWithAllTemplates = allPresent && ui == installstate.UIEle && mode == installstate.ModeEmbedded
	return s.digest, nil
}
