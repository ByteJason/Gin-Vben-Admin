package installplatform

import (
	"bytes"
	"context"
	"fmt"
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
	receipt, err := service.Publish(context.Background(), request, installer.Plan{SelectedUI: "naive", Mode: "embedded"})
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
	if _, err := service.Publish(context.Background(), request, installer.Plan{SelectedUI: "antd", Mode: "standalone"}); err != nil {
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

func TestEnvironmentInstallerRollbackSurvivesProcessRestartWithoutPersistingSecrets(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".env")
	backup := filepath.Join(root, "install", ".env-backup")
	store := NewAtomicEnvStore(path, backup)
	request := installer.ApplyRequest{
		Mode: "dev",
		Database: installer.DatabaseConnection{Driver: "mysql", Mode: "single", Host: "127.0.0.1", Port: 3306,
			Database: "app", Username: "app", Password: "restart-database-secret"},
		Redis: installer.RedisConnection{Mode: "single", Addr: "127.0.0.1:6379", Password: "restart-redis-secret"},
		Admin: installer.AdminAccount{Username: "admin", Password: "restart-admin-secret"},
	}
	first := NewEnvironmentInstaller(store, filepath.Join(root, "install"), bytes.NewReader(bytes.Repeat([]byte{0x3b}, 96)))
	receipt, err := first.Publish(context.Background(), request, installer.Plan{SelectedUI: "antd", Mode: "dev"})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	encoded := fmt.Sprintf("%+v", receipt)
	for _, secret := range []string{request.Database.Password, request.Redis.Password, request.Admin.Password} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("receipt contains secret %q: %s", secret, encoded)
		}
	}

	restarted := NewEnvironmentInstaller(store, filepath.Join(root, "install"), bytes.NewReader(bytes.Repeat([]byte{0x4c}, 96)))
	if err := restarted.Rollback(context.Background(), receipt); err != nil {
		t.Fatalf("Rollback() after restart error = %v; receipt=%#v", err, receipt)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("recovered rollback retained generated .env: %v", err)
	}
}

func TestEnvironmentInstallerRecoversPreparedIntentWithoutReceiptAfterRestart(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, ".env")
	backup := filepath.Join(root, "install", ".env-backup")
	reference := "install-0123456789abcdef0123456789abcdef"
	request := installer.ApplyRequest{
		Mode: "dev",
		Database: installer.DatabaseConnection{Driver: "mysql", Mode: "single", Host: "127.0.0.1", Port: 3306,
			Database: "app", Username: "app", Password: "prepared-database-secret"},
		Redis: installer.RedisConnection{Mode: "single", Addr: "127.0.0.1:6379", Password: "prepared-redis-secret"},
		Admin: installer.AdminAccount{Username: "admin", Password: "prepared-admin-secret"},
	}
	first := NewEnvironmentInstaller(NewAtomicEnvStore(path, backup), filepath.Join(root, "install"), bytes.NewReader(bytes.Repeat([]byte{0x6d}, 64)))
	if _, err := first.PublishWithReference(context.Background(), request, installer.Plan{SelectedUI: "ele", Mode: "dev"}, reference); err != nil {
		t.Fatalf("PublishWithReference() error = %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(.env) error = %v", err)
	}
	if !strings.Contains(string(contents), `INSTALL_TRANSACTION_ID="`+reference+`"`) {
		t.Fatalf("prepared .env does not identify its transaction: %q", contents)
	}

	restarted := NewEnvironmentInstaller(NewAtomicEnvStore(path, backup), filepath.Join(root, "install"), bytes.NewReader(bytes.Repeat([]byte{0x7e}, 64)))
	if err := restarted.RecoverPrepared(context.Background(), reference); err != nil {
		t.Fatalf("RecoverPrepared() after restart error = %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("prepared recovery retained generated .env: %v", err)
	}
}

func TestEnvironmentInstallerRecoversPreparedReplacementAfterRestart(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, ".env")
	backup := filepath.Join(root, "install", ".env-backup")
	original := []byte("SERVER_ADDR=\"127.0.0.1:8080\"\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("WriteFile(original) error = %v", err)
	}
	reference := "install-fedcba9876543210fedcba9876543210"
	request := installer.ApplyRequest{
		Mode: "dev",
		Database: installer.DatabaseConnection{Driver: "mysql", Mode: "single", Host: "127.0.0.1", Port: 3306,
			Database: "app", Username: "app", Password: "prepared-database-secret"},
		Redis: installer.RedisConnection{Mode: "single", Addr: "127.0.0.1:6379"},
		Admin: installer.AdminAccount{Username: "admin", Password: "prepared-admin-secret"},
	}
	first := NewEnvironmentInstaller(NewAtomicEnvStore(path, backup), filepath.Join(root, "install"), bytes.NewReader(bytes.Repeat([]byte{0x4a}, 64)))
	if _, err := first.PublishWithReference(context.Background(), request, installer.Plan{SelectedUI: "antd", Mode: "dev"}, reference); err != nil {
		t.Fatalf("PublishWithReference() error = %v", err)
	}

	restarted := NewEnvironmentInstaller(NewAtomicEnvStore(path, backup), filepath.Join(root, "install"), bytes.NewReader(bytes.Repeat([]byte{0x5b}, 64)))
	if err := restarted.RecoverPrepared(context.Background(), reference); err != nil {
		t.Fatalf("RecoverPrepared() replacement error = %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(restored .env) error = %v", err)
	}
	if string(contents) != string(original) {
		t.Fatalf("restored .env = %q, want %q", contents, original)
	}
	entries, err := os.ReadDir(backup)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("ReadDir(backup) error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("prepared backup remains after recovery: %v", entries)
	}
}

func TestEnvironmentInstallerFinalizeRemovesOnlyTransactionOwnedBackupAfterRestart(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, ".env")
	backupDir := filepath.Join(root, "install", "environment-backup")
	original := []byte("DATABASE_PASSWORD=old-secret\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	reference := "install-11111111111111111111111111111111"
	request := installer.ApplyRequest{
		Mode: "dev",
		Database: installer.DatabaseConnection{Driver: "mysql", Mode: "single", Host: "127.0.0.1", Port: 3306,
			Database: "app", Username: "app", Password: "new-database-secret"},
		Redis: installer.RedisConnection{Mode: "single", Addr: "127.0.0.1:6379"},
		Admin: installer.AdminAccount{Username: "admin", Password: "new-admin-secret"},
	}
	first := NewEnvironmentInstaller(NewAtomicEnvStore(path, backupDir), filepath.Join(root, "install"), bytes.NewReader(bytes.Repeat([]byte{0x3c}, 64)))
	receipt, err := first.PublishWithReference(context.Background(), request, installer.Plan{SelectedUI: "antd", Mode: "dev"}, reference)
	if err != nil {
		t.Fatalf("PublishWithReference() error = %v", err)
	}
	unrelated := filepath.Join(backupDir, ".env.previous-install-22222222222222222222222222222222")
	if err := os.WriteFile(unrelated, []byte("unrelated-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	restarted := NewEnvironmentInstaller(NewAtomicEnvStore(path, backupDir), filepath.Join(root, "install"), bytes.NewReader(bytes.Repeat([]byte{0x4d}, 64)))
	if err := restarted.Finalize(context.Background(), receipt); err != nil {
		t.Fatalf("Finalize() after restart error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(backupDir, receipt.BackupName)); !os.IsNotExist(err) {
		t.Fatalf("transaction backup remains after finalize: %v", err)
	}
	if contents, err := os.ReadFile(unrelated); err != nil || string(contents) != "unrelated-secret\n" {
		t.Fatalf("unrelated backup changed: contents=%q err=%v", contents, err)
	}
	installed, err := os.ReadFile(path)
	if err != nil || string(installed) == string(original) {
		t.Fatalf("installed environment was not preserved: contents=%q err=%v", installed, err)
	}
	// Finalization is idempotent across the crash window before journal removal.
	if err := restarted.Finalize(context.Background(), receipt); err != nil {
		t.Fatalf("second Finalize() error = %v", err)
	}
}
