package installplatform

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	installstate "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/installstate"
)

func TestFileTransactionJournalCreatePublishesOnlyAfterCompleteTempFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "install", "transaction.json")
	publishFailure := errors.New("simulated crash before no-clobber publish")
	journal := newFileTransactionJournal(path, root, func(temporary, final string) error {
		if final != path {
			t.Fatalf("publish final = %q, want %q", final, path)
		}
		contents, err := os.ReadFile(temporary)
		if err != nil || len(contents) == 0 || contents[len(contents)-1] != '\n' {
			t.Fatalf("temporary journal is incomplete: %q error=%v", contents, err)
		}
		return publishFailure
	})
	transaction := installer.ApplyTransaction{
		Schema: installer.ApplyTransactionSchema, Owner: installer.ApplyTransactionOwner,
		ID: "install-0123456789abcdef0123456789abcdef", SelectedUI: installstate.UIAntd,
		Mode: installstate.ModeDev, DatabaseTarget: strings.Repeat("d", 64), Phase: installer.TransactionApplying, CurrentStep: "schema",
		CompletedSteps: []string{"plan", "database", "redis"},
		UpdatedAt:      time.Date(2026, time.August, 24, 9, 0, 0, 0, time.UTC),
	}
	if err := journal.Create(context.Background(), transaction); !errors.Is(err, publishFailure) {
		t.Fatalf("Create() error = %v, want injected publish failure", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("failed publish exposed final journal: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".transaction.json.tmp-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary journal remains: %v error=%v", matches, err)
	}
}

func TestFileTransactionJournalApplyCollisionReportsPersistentOwnedReleaseFailure(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "install")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "admin-init.lock"), []byte("active admin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal := NewFileTransactionJournal(filepath.Join(stateDir, "transaction.json"))
	releaseCalls := 0
	journal.acquireApplyLease = func(string, string) (func() error, error) {
		return func() error {
			releaseCalls++
			return errors.New("persistent owned remove failure")
		}, nil
	}

	if release, err := journal.AcquireApply(context.Background()); err == nil || errors.Is(err, installer.ErrApplyBusy) {
		if release != nil {
			release()
		}
		t.Fatalf("AcquireApply() error = %v, want observable release failure", err)
	} else if !strings.Contains(err.Error(), "persistent owned remove failure") {
		t.Fatalf("AcquireApply() error = %v", err)
	}
	if releaseCalls != 1 {
		t.Fatalf("release calls = %d, want 1 bounded release operation", releaseCalls)
	}
}

func TestFileTransactionJournalOperationReportsPersistentOwnedReleaseFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install", "transaction.json")
	journal := NewFileTransactionJournal(path)
	transaction := installer.ApplyTransaction{
		Schema: installer.ApplyTransactionSchema, Owner: installer.ApplyTransactionOwner,
		ID: "install-0123456789abcdef0123456789abcdef", SelectedUI: installstate.UIAntd,
		Mode: installstate.ModeDev, DatabaseTarget: strings.Repeat("d", 64), Phase: installer.TransactionApplying, CurrentStep: "schema",
		CompletedSteps: []string{"plan", "database", "redis"},
		UpdatedAt:      time.Date(2026, time.August, 24, 9, 0, 0, 0, time.UTC),
	}
	if err := journal.Create(context.Background(), transaction); err != nil {
		t.Fatal(err)
	}
	journal.acquireJournalLease = func(string) (func() error, error) {
		return func() error { return errors.New("persistent journal lease remove failure") }, nil
	}
	transaction.CurrentStep = "identity"
	transaction.CompletedSteps = append(transaction.CompletedSteps, "schema")
	transaction.UpdatedAt = transaction.UpdatedAt.Add(time.Second)

	if err := journal.Update(context.Background(), transaction); err == nil || !strings.Contains(err.Error(), "persistent journal lease remove failure") {
		t.Fatalf("Update() error = %v, want observable release failure", err)
	}
}
