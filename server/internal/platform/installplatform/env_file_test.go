package installplatform_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"example.com/gin-vben-admin/server/internal/platform/installplatform"
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
