package backup

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appbackup "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/backup"
)

type roundTripRunner struct {
	dumpPayload string
	restored    string
}

func (r *roundTripRunner) Run(_ context.Context, command Command, stdin io.Reader, stdout, _ io.Writer) error {
	if command.Program == "mysqldump" || command.Program == "pg_dump" {
		_, err := io.WriteString(stdout, r.dumpPayload)
		return err
	}
	b, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}
	r.restored = string(b)
	return nil
}

func TestLocalBackupServiceRoundTripWithCommandAdapter(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0).UTC()
	runner := &roundTripRunner{dumpPayload: "CREATE TABLE b8_roundtrip (id INT);\n"}
	commands := NewDatabaseCommands(runner)
	store := NewLocalArtifactStore(func() time.Time { return clock })
	service := appbackup.NewService(commands, commands, store, appbackup.Config{
		Clock: func() time.Time { return clock }, DefaultRPO: 15 * time.Minute, DefaultRTO: 30 * time.Minute,
	})
	path := filepath.Join(t.TempDir(), "roundtrip.gva")
	artifact, err := service.Backup(context.Background(), appbackup.BackupRequest{
		Source:      appbackup.Source{Driver: appbackup.DriverMySQL, DSN: "app:secret@tcp(db:3306)/gin"},
		Destination: path, EncryptionKey: []byte("operator-key"),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Restore(context.Background(), appbackup.RestoreRequest{
		Source:       appbackup.Source{Driver: appbackup.DriverMySQL, DSN: "app:secret@tcp(db:3306)/gin"},
		ArtifactPath: artifact.Path, EncryptionKey: []byte("operator-key"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runner.restored, "b8_roundtrip") || !result.WithinRPO || !result.WithinRTO {
		t.Fatalf("round trip restored=%q result=%#v", runner.restored, result)
	}
}
