package installplatform

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
)

func TestEnvironmentInstallerPublishesAndRollsBackPrivateRootEnv(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, ".env")
	store := NewAtomicEnvStore(path, filepath.Join(root, "install", ".install-backup", "apply"))
	service := NewEnvironmentInstaller(store, "../install", bytes.NewReader(bytes.Repeat([]byte{0x5a}, 96)))
	request := installer.ApplyRequest{
		Mode: "embedded",
		Database: installer.DatabaseConnection{
			Driver: "postgres", Mode: "single", Host: "127.0.0.1", Port: 5432,
			Database: "app", Username: "app-user", Password: "database-secret", TLSMode: "disable",
		},
		Redis: installer.RedisConnection{Mode: "single", Addr: "127.0.0.1:6379", Password: "redis-secret"},
		Admin: installer.AdminAccount{Username: "admin", Password: "initial-password-123"},
	}
	receipt, err := service.Publish(context.Background(), request, installer.AssetReceipt{
		ArtifactHash: strings.Repeat("a", 64), ManifestHash: strings.Repeat("b", 64),
	}, installer.Plan{SelectedUI: "naive", Mode: "embedded"})
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
		`AUTH_ACCESS_TTL="30m"`,
		`AUTH_CAPTCHA_ENABLED="false"`,
		`AUTH_CAPTCHA_RISK_THRESHOLD="3"`,
		`AUTH_CAPTCHA_RISK_WINDOW="15m"`,
		`AUTH_CAPTCHA_CHALLENGE_TTL="2m"`,
		`AUTH_CAPTCHA_KEY_PREFIX="auth-captcha"`,
		`I18N_MODE="single"`,
		`I18N_DEFAULT_LOCALE="zh-CN"`,
		`I18N_SUPPORTED_LOCALES="zh-CN,en-US"`,
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

func TestEnvironmentInstallerPersistsExplicitLocalePolicy(t *testing.T) {
	root := t.TempDir()
	store := NewAtomicEnvStore(filepath.Join(root, ".env"), filepath.Join(root, "backup"))
	service := NewEnvironmentInstaller(store, filepath.Join(root, "install"), bytes.NewReader(bytes.Repeat([]byte{0x2a}, 96)))
	request := installer.ApplyRequest{
		Mode: "standalone", Locale: "en-US", LocaleMode: "multi",
		Database: installer.DatabaseConnection{Driver: "mysql", Mode: "single", Host: "127.0.0.1", Port: 3306, Database: "app", Username: "app", Password: "database-secret"},
		Redis:    installer.RedisConnection{Mode: "single", Addr: "127.0.0.1:6379"},
		Admin:    installer.AdminAccount{Username: "admin", Password: "initial-password-123"},
	}
	if _, err := service.Publish(context.Background(), request, installer.AssetReceipt{ArtifactHash: strings.Repeat("a", 64), ManifestHash: strings.Repeat("b", 64)}, installer.Plan{SelectedUI: "antd", Mode: "standalone"}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(root, ".env"))
	if err != nil {
		t.Fatalf("ReadFile(.env) error = %v", err)
	}
	for _, required := range []string{`I18N_MODE="multi"`, `I18N_DEFAULT_LOCALE="en-US"`, `I18N_SUPPORTED_LOCALES="zh-CN,en-US"`} {
		if !strings.Contains(string(contents), required) {
			t.Fatalf(".env missing explicit locale setting %q", required)
		}
	}
}
