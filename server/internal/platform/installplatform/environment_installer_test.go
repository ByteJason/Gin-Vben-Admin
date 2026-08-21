package installplatform

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	installer "example.com/gin-vben-admin/server/internal/application/installer"
)

func TestEnvironmentInstallerPublishesAndRollsBackPrivateRootEnv(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, ".env")
	store := NewAtomicEnvStore(path, filepath.Join(root, "install", ".install-backup", "apply"))
	service := NewEnvironmentInstaller(store, "../install", bytes.NewReader(bytes.Repeat([]byte{0x5a}, 96)))
	request := installer.ApplyRequest{
		SelectedUI: "naive", Mode: "embedded", ConfirmCleanup: true,
		Database: installer.DatabaseConnection{
			Driver: "postgres", Mode: "single", Host: "127.0.0.1", Port: 5432,
			Database: "app", Username: "app-user", Password: "database-secret", TLSMode: "disable",
		},
		Redis: installer.RedisConnection{Mode: "single", Addr: "127.0.0.1:6379", Password: "redis-secret"},
		Admin: installer.AdminAccount{Username: "admin", Password: "initial-password-123"},
	}
	receipt, err := service.Publish(context.Background(), request, installer.AssetReceipt{
		ArtifactHash: strings.Repeat("a", 64), ManifestHash: strings.Repeat("b", 64),
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if len(receipt.Digest) != 64 || receipt.Reference == "" || strings.Contains(receipt.Reference, "secret") {
		t.Fatalf("credential-free receipt = %#v", receipt)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(.env) error = %v", err)
	}
	for _, required := range []string{
		`APP_UI_ACTIVE="naive"`, `APP_UI_MODE="embedded"`, `DATABASE_DRIVER="postgres"`,
		`DATABASE_DSN="postgres://app-user:database-secret@127.0.0.1:5432/app?sslmode=disable"`,
		`REDIS_PASSWORD="redis-secret"`, `AUTH_ENABLED="true"`, `INSTALL_STATE_DIR="../install"`,
	} {
		if !strings.Contains(string(contents), required) {
			t.Fatalf(".env missing required setting %q", required)
		}
	}
	if strings.Contains(string(contents), request.Admin.Password) {
		t.Fatal(".env contains initial administrator password")
	}
	if err := service.Rollback(context.Background(), receipt); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("rollback retained generated .env: %v", err)
	}
}
