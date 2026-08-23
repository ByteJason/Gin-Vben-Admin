package installplatform_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/installplatform"
)

func TestAtomicEnvStoreWritesDeterministicPrivateConfiguration(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, ".env")
	store := installplatform.NewAtomicEnvStore(path)
	values := map[string]string{
		"REDIS_ADDR":      "127.0.0.1:6379",
		"DATABASE_DSN":    "root:p@ss@tcp(127.0.0.1:3306)/app?parseTime=true&charset=utf8mb4",
		"DATABASE_DRIVER": "mysql",
		"AUTH_ENABLED":    "true",
		"AUTH_JWT_SECRET": "secret # kept inside quotes",
	}

	receipt, err := store.Write(context.Background(), values)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	want := "AUTH_ENABLED=\"true\"\n" +
		"AUTH_JWT_SECRET=\"secret # kept inside quotes\"\n" +
		"DATABASE_DRIVER=\"mysql\"\n" +
		"DATABASE_DSN=\"root:p@ss@tcp(127.0.0.1:3306)/app?parseTime=true&charset=utf8mb4\"\n" +
		"REDIS_ADDR=\"127.0.0.1:6379\"\n"
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(contents); got != want {
		t.Fatalf(".env contents = %q, want %q", got, want)
	}
	digest := sha256.Sum256([]byte(want))
	if got, wantDigest := receipt.Digest, hex.EncodeToString(digest[:]); got != wantDigest {
		t.Fatalf("receipt digest = %q, want %q", got, wantDigest)
	}
	if receipt.Replaced {
		t.Fatal("receipt Replaced = true for a newly created file")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf(".env mode = %o, want 600", info.Mode().Perm())
	}
	matches, err := filepath.Glob(filepath.Join(root, ".env.tmp-*"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain after write: %v", matches)
	}
}

func TestAtomicEnvStoreBacksUpReplacementAndRollsBack(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, ".env")
	backupDir := filepath.Join(root, "install", ".install-backup", "transaction-1")
	original := []byte("SERVER_ADDR=\"127.0.0.1:8080\"\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("WriteFile(original) error = %v", err)
	}

	store := installplatform.NewAtomicEnvStore(path, backupDir)
	receipt, err := store.Write(context.Background(), map[string]string{
		"SERVER_ADDR": "0.0.0.0:8080",
	})
	if err != nil {
		t.Fatalf("Write() replacement error = %v", err)
	}
	if !receipt.Replaced {
		t.Fatal("receipt Replaced = false, want true")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) == string(original) {
		t.Fatalf("replacement was not published: contents=%q error=%v", got, err)
	}

	if err := store.Rollback(context.Background(), receipt); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(restored) error = %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("restored contents = %q, want %q", got, original)
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("ReadDir(backup) error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("backup artifacts remain after rollback: %v", entries)
	}
}

func TestAtomicEnvStoreRejectsUnapprovedKeysAndLineInjection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values map[string]string
	}{
		{
			name: "unapproved key",
			values: map[string]string{
				"SHELL": "/bin/sh",
			},
		},
		{
			name: "line injection",
			values: map[string]string{
				"DATABASE_DSN": "valid\nAUTH_ENABLED=\"false\"",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), ".env")
			store := installplatform.NewAtomicEnvStore(path)
			if _, err := store.Write(context.Background(), tt.values); err == nil {
				t.Fatal("Write() error = nil, want validation error")
			}
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Fatalf("invalid input created target: %v", err)
			}
		})
	}
}
