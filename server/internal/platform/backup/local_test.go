package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appbackup "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/backup"
)

func TestLocalArtifactStoreEncryptsAndRestoresArtifact(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daily.gva")
	store := NewLocalArtifactStore(func() time.Time { return time.Unix(1_700_000_000, 0).UTC() })
	sink, err := store.Create(context.Background(), appbackup.ArtifactRequest{
		Destination: path, Source: appbackup.Source{Driver: appbackup.DriverMySQL},
		EncryptionKey: []byte("operator-key"), TargetRPO: 15 * time.Minute, TargetRTO: 30 * time.Minute,
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("CREATE TABLE backup_probe (id INT);\n")
	if _, err := sink.Write(plain); err != nil {
		t.Fatal(err)
	}
	artifact, err := sink.Commit(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Encryption != appbackup.EncryptionAES256GCM || artifact.PlaintextBytes != int64(len(plain)) || artifact.CiphertextBytes <= artifact.PlaintextBytes {
		t.Fatalf("artifact metadata = %#v", artifact)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, plain) {
		t.Fatal("plaintext is present in encrypted artifact")
	}
	metadata, err := os.ReadFile(path + ".meta.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"operator-key", "secret", "password"} {
		if bytes.Contains(metadata, []byte(secret)) {
			t.Fatalf("secret %q is present in artifact metadata", secret)
		}
	}
	reader, err := store.Open(context.Background(), path, []byte("operator-key"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	if closeErr := reader.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("restored bytes = %q, err = %v", got, err)
	}
}

func TestLocalArtifactStoreRejectsWrongKeyAndOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daily.gva")
	store := NewLocalArtifactStore()
	request := appbackup.ArtifactRequest{Destination: path, Source: appbackup.Source{Driver: appbackup.DriverPostgres}, EncryptionKey: []byte("key"), TargetRPO: time.Minute, TargetRTO: time.Minute, CreatedAt: time.Now()}
	sink, err := store.Create(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(sink, "dump")
	if _, err = sink.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Create(context.Background(), request); !errors.Is(err, ErrArtifactExists) {
		t.Fatalf("overwrite error = %v", err)
	}
	if _, err = store.Open(context.Background(), path, []byte("wrong")); err == nil {
		t.Fatal("wrong key unexpectedly opened artifact")
	}
}

func TestLocalArtifactStoreAbortRemovesStagingFiles(t *testing.T) {
	dir := t.TempDir()
	store := NewLocalArtifactStore()
	sink, err := store.Create(context.Background(), appbackup.ArtifactRequest{
		Destination: filepath.Join(dir, "aborted.gva"), Source: appbackup.Source{Driver: appbackup.DriverMySQL},
		EncryptionKey: []byte("key"), TargetRPO: time.Minute, TargetRTO: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(sink, "partial"); err != nil {
		t.Fatal(err)
	}
	if err := sink.Abort(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("staging files remain after abort: %#v", entries)
	}
}

func TestLocalArtifactStoreDetectsTamperedEnvelope(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tampered.gva")
	store := NewLocalArtifactStore()
	sink, err := store.Create(context.Background(), appbackup.ArtifactRequest{
		Destination: path, Source: appbackup.Source{Driver: appbackup.DriverMySQL},
		EncryptionKey: []byte("key"), TargetRPO: time.Minute, TargetRTO: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(sink, "payload")
	if _, err := sink.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xff
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(context.Background(), path, []byte("key")); !errors.Is(err, ErrArtifactCorrupt) && !errors.Is(err, ErrArtifactKey) {
		t.Fatalf("tampered artifact error = %v", err)
	}
}

func TestLocalArtifactStoreRoundTripsMultipleAuthenticatedChunks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chunks.gva")
	store := NewLocalArtifactStore()
	store.chunkSize = 32
	sink, err := store.Create(context.Background(), appbackup.ArtifactRequest{
		Destination: path, Source: appbackup.Source{Driver: appbackup.DriverPostgres},
		EncryptionKey: []byte("chunk-key"), TargetRPO: time.Minute, TargetRTO: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	plain := bytes.Repeat([]byte("0123456789abcdef"), 17)
	if _, err := sink.Write(plain); err != nil {
		t.Fatal(err)
	}
	if _, err := sink.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	reader, err := store.Open(context.Background(), path, []byte("chunk-key"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	closeErr := reader.Close()
	if err != nil || closeErr != nil || !bytes.Equal(got, plain) {
		t.Fatalf("multi-chunk restore bytes=%d err=%v close=%v", len(got), err, closeErr)
	}
}

type fakeRunner struct {
	commands []Command
	output   string
	err      error
}

func (r *fakeRunner) Run(_ context.Context, command Command, _ io.Reader, stdout, _ io.Writer) error {
	r.commands = append(r.commands, command)
	if r.output != "" {
		_, _ = io.WriteString(stdout, r.output)
	}
	return r.err
}

func TestDatabaseCommandsUseDriverToolsWithoutPasswordInArguments(t *testing.T) {
	runner := &fakeRunner{output: "dump"}
	commands := NewDatabaseCommands(runner)
	var out bytes.Buffer
	if err := commands.Dump(context.Background(), appbackup.Source{Driver: appbackup.DriverMySQL, DSN: "app:secret@tcp(db:3307)/gin"}, &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "dump" || len(runner.commands) != 1 {
		t.Fatalf("dump output=%q commands=%#v", out.String(), runner.commands)
	}
	cmd := runner.commands[0]
	if cmd.Program != "mysqldump" || strings.Contains(strings.Join(cmd.Args, " "), "secret") || !containsEnv(cmd.Env, "MYSQL_PWD=secret") {
		t.Fatalf("mysql command leaks password or has wrong shape: %#v", cmd)
	}

	runner = &fakeRunner{}
	commands = NewDatabaseCommands(runner)
	if err := commands.Restore(context.Background(), appbackup.Source{Driver: appbackup.DriverPostgres, DSN: "postgres://app:secret@db:5433/gin"}, strings.NewReader("dump")); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 1 || runner.commands[0].Program != "psql" || strings.Contains(strings.Join(runner.commands[0].Args, " "), "secret") || !containsEnv(runner.commands[0].Env, "PGPASSWORD=secret") {
		t.Fatalf("postgres command leaks password or has wrong shape: %#v", runner.commands)
	}
}

func containsEnv(env []string, want string) bool {
	for _, item := range env {
		if item == want {
			return true
		}
	}
	return false
}
