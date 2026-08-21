package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type fakeDumper struct {
	called bool
	err    error
}

func (d *fakeDumper) Dump(_ context.Context, _ Source, dst io.Writer) error {
	d.called = true
	if d.err != nil {
		return d.err
	}
	_, err := io.WriteString(dst, "CREATE TABLE b8_probe (id INT);\n")
	return err
}

type fakeRestorer struct {
	called bool
	data   string
	err    error
}

func (r *fakeRestorer) Restore(_ context.Context, _ Source, src io.Reader) error {
	r.called = true
	b, err := io.ReadAll(src)
	if err != nil {
		return err
	}
	r.data = string(b)
	return r.err
}

type fakeSink struct {
	bytes.Buffer
	artifact Artifact
	aborted  bool
	commits  int
}

func (s *fakeSink) Commit(context.Context) (Artifact, error) {
	s.commits++
	return s.artifact, nil
}

func (s *fakeSink) Abort() error {
	s.aborted = true
	return nil
}

type fakeReader struct {
	io.Reader
	artifact Artifact
}

func (r *fakeReader) Close() error       { return nil }
func (r *fakeReader) Artifact() Artifact { return r.artifact }

type fakeArtifacts struct {
	sink   *fakeSink
	reader *fakeReader
	path   string
}

func (a *fakeArtifacts) Create(context.Context, ArtifactRequest) (ArtifactSink, error) {
	return a.sink, nil
}

func (a *fakeArtifacts) Open(_ context.Context, path string, _ []byte) (ArtifactReader, error) {
	if path != a.path {
		return nil, errors.New("unexpected artifact path")
	}
	return a.reader, nil
}

func TestServiceBackupUsesDumperAndRecordsRPOAndRTOMetadata(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	dumper := &fakeDumper{}
	artifacts := &fakeArtifacts{sink: &fakeSink{artifact: Artifact{
		ID: "b8-1", Driver: DriverMySQL, Path: "/tmp/b8-1.gva", CreatedAt: now,
		CompletedAt: now.Add(time.Second), PlaintextBytes: 33, CiphertextBytes: 77,
		SHA256: "digest", Encryption: EncryptionAES256GCM,
	}}}
	svc := NewService(dumper, nil, artifacts, Config{Clock: func() time.Time { return now }, DefaultRPO: 15 * time.Minute, DefaultRTO: 30 * time.Minute})

	got, err := svc.Backup(context.Background(), BackupRequest{
		Source:      Source{Driver: DriverMySQL, DSN: "user:secret@tcp(db:3306)/app"},
		Destination: "/tmp/b8-1.gva", EncryptionKey: []byte("local-test-key"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !dumper.called || artifacts.sink.commits != 1 || artifacts.sink.aborted {
		t.Fatalf("backup lifecycle = called:%v commits:%d aborted:%v", dumper.called, artifacts.sink.commits, artifacts.sink.aborted)
	}
	if got.Driver != DriverMySQL || got.TargetRPO != 15*time.Minute || got.TargetRTO != 30*time.Minute {
		t.Fatalf("metadata = %#v", got)
	}
}

func TestServiceBackupAbortsArtifactWhenDumpFails(t *testing.T) {
	dumper := &fakeDumper{err: errors.New("dump failed")}
	sink := &fakeSink{}
	artifacts := &fakeArtifacts{sink: sink}
	svc := NewService(dumper, nil, artifacts, Config{Clock: time.Now})
	_, err := svc.Backup(context.Background(), BackupRequest{
		Source:      Source{Driver: DriverPostgres, DSN: "postgres://u:p@db/app"},
		Destination: "/tmp/b8-2.gva", EncryptionKey: []byte("key"),
	})
	if !errors.Is(err, ErrBackupFailed) || !sink.aborted {
		t.Fatalf("error = %v, aborted = %v", err, sink.aborted)
	}
}

func TestServiceRestoreRejectsCrossDriverAndReportsRPOAndRTO(t *testing.T) {
	now := time.Unix(1_700_000_100, 0).UTC()
	restorer := &fakeRestorer{}
	artifact := Artifact{
		ID: "b8-3", Driver: DriverPostgres, Path: "/tmp/b8-3.gva", CreatedAt: now.Add(-2 * time.Minute),
		TargetRPO: 15 * time.Minute, TargetRTO: 30 * time.Minute, Encryption: EncryptionAES256GCM,
	}
	artifacts := &fakeArtifacts{path: artifact.Path, reader: &fakeReader{Reader: strings.NewReader("dump"), artifact: artifact}}
	svc := NewService(nil, restorer, artifacts, Config{Clock: func() time.Time { return now.Add(3 * time.Second) }})

	if _, err := svc.Restore(context.Background(), RestoreRequest{
		Source: Source{Driver: DriverMySQL, DSN: "user:secret@tcp(db:3306)/app"}, ArtifactPath: artifact.Path, EncryptionKey: []byte("key"),
	}); !errors.Is(err, ErrArtifactDriverMismatch) || restorer.called {
		t.Fatalf("cross-driver restore error = %v, called = %v", err, restorer.called)
	}

	result, err := svc.Restore(context.Background(), RestoreRequest{
		Source: Source{Driver: DriverPostgres, DSN: "postgres://u:p@db/app"}, ArtifactPath: artifact.Path, EncryptionKey: []byte("key"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !restorer.called || restorer.data != "dump" || result.ObservedRPO != 2*time.Minute+3*time.Second || !result.WithinRPO || !result.WithinRTO {
		t.Fatalf("restore result = %#v, restorer = %#v", result, restorer)
	}
}

func TestServiceRestoreFillsDefaultTargetsInResult(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	restorer := &fakeRestorer{}
	artifact := Artifact{ID: "b8-defaults", Driver: DriverMySQL, Path: "/tmp/b8-defaults.gva", CreatedAt: now}
	artifacts := &fakeArtifacts{path: artifact.Path, reader: &fakeReader{Reader: strings.NewReader("dump"), artifact: artifact}}
	svc := NewService(nil, restorer, artifacts, Config{Clock: func() time.Time { return now }, DefaultRPO: time.Minute, DefaultRTO: 2 * time.Minute})
	result, err := svc.Restore(context.Background(), RestoreRequest{Source: Source{Driver: DriverMySQL, DSN: "dsn"}, ArtifactPath: artifact.Path, EncryptionKey: []byte("key")})
	if err != nil {
		t.Fatal(err)
	}
	if result.Artifact.TargetRPO != time.Minute || result.Artifact.TargetRTO != 2*time.Minute {
		t.Fatalf("default targets = %v/%v", result.Artifact.TargetRPO, result.Artifact.TargetRTO)
	}
}

func TestServiceValidatesSourceAndEncryptionKey(t *testing.T) {
	svc := NewService(&fakeDumper{}, nil, &fakeArtifacts{sink: &fakeSink{}}, Config{Clock: time.Now})
	cases := []BackupRequest{
		{Source: Source{Driver: "sqlite", DSN: "x"}, Destination: "/tmp/a", EncryptionKey: []byte("key")},
		{Source: Source{Driver: DriverMySQL}, Destination: "/tmp/a", EncryptionKey: []byte("key")},
		{Source: Source{Driver: DriverMySQL, DSN: "x"}, Destination: "/tmp/a"},
	}
	for _, input := range cases {
		if _, err := svc.Backup(context.Background(), input); !errors.Is(err, ErrInvalidBackupRequest) {
			t.Errorf("request %#v error = %v", input, err)
		}
	}
}
