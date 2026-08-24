package installplatform_test

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
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/installplatform"
)

func TestFileTransactionJournalRoundTripsAndRemovesOnlyTheExpectedTransaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install", "transaction.json")
	journal := installplatform.NewFileTransactionJournal(path)
	transaction := validApplyTransaction()

	if err := journal.Create(context.Background(), transaction); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	transaction.CurrentStep = "identity"
	transaction.CompletedSteps = append(transaction.CompletedSteps, "schema")
	transaction.UpdatedAt = transaction.UpdatedAt.Add(time.Second)
	if err := journal.Update(context.Background(), transaction); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	got, exists, err := journal.Load(context.Background())
	if err != nil || !exists {
		t.Fatalf("Load() = (%#v, %t, %v)", got, exists, err)
	}
	if got.ID != transaction.ID || got.CurrentStep != "identity" || len(got.CompletedSteps) != 4 {
		t.Fatalf("Load() transaction = %#v", got)
	}
	if err := journal.Remove(context.Background(), "install-ffffffffffffffffffffffffffffffff"); !errors.Is(err, installer.ErrTransactionChanged) {
		t.Fatalf("Remove(different) error = %v, want ErrTransactionChanged", err)
	}
	if err := journal.Remove(context.Background(), transaction.ID); err != nil {
		t.Fatalf("Remove(expected) error = %v", err)
	}
	if _, exists, err := journal.Load(context.Background()); err != nil || exists {
		t.Fatalf("Load() after remove = exists:%t error:%v", exists, err)
	}
}

func TestFileTransactionJournalNeverOverwritesAdminInitOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transaction.json")
	cli := `{"schema":1,"owner":"admin-init","id":"fixture","selectedUi":"antd","phase":"dependencies_pending","moves":[]}` + "\n"
	if err := os.WriteFile(path, []byte(cli), 0o600); err != nil {
		t.Fatal(err)
	}
	journal := installplatform.NewFileTransactionJournal(path)
	if _, _, err := journal.Load(context.Background()); !errors.Is(err, installer.ErrApplyBusy) {
		t.Fatalf("Load(CLI owner) error = %v, want ErrApplyBusy", err)
	}
	if err := journal.Create(context.Background(), validApplyTransaction()); !errors.Is(err, installer.ErrApplyBusy) {
		t.Fatalf("Create(over CLI owner) error = %v, want ErrApplyBusy", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != cli {
		t.Fatalf("CLI journal changed: %q, error=%v", got, err)
	}
}

func TestFileTransactionJournalRejectsUnknownCredentialFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transaction.json")
	transaction := validApplyTransaction()
	encoded := `{"schema":1,"owner":"server-installer","id":"` + transaction.ID + `","selectedUi":"antd","mode":"dev","databaseTarget":"` + strings.Repeat("d", 64) + `","phase":"applying","currentStep":"schema","completedSteps":["plan","database","redis"],"updatedAt":"2026-08-24T09:00:00Z","password":"database-secret"}`
	if err := os.WriteFile(path, []byte(encoded), 0o600); err != nil {
		t.Fatal(err)
	}
	journal := installplatform.NewFileTransactionJournal(path)
	if _, _, err := journal.Load(context.Background()); err == nil {
		t.Fatal("Load() error = nil for credential-bearing unknown field")
	}
	contents, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(contents), "database-secret") {
		t.Fatalf("invalid journal should be preserved for inspection: %q error=%v", contents, err)
	}
}

func TestFileTransactionJournalReclaimsADeadProcessLockAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install", "transaction.json")
	journal := installplatform.NewFileTransactionJournal(path)
	transaction := validApplyTransaction()
	if err := journal.Create(context.Background(), transaction); err != nil {
		t.Fatal(err)
	}
	stale := `{"schema":1,"pid":99999999,"createdAt":"2026-08-23T00:00:00Z"}` + "\n"
	if err := os.WriteFile(path+".lock", []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	transaction.CurrentStep = "identity"
	transaction.CompletedSteps = append(transaction.CompletedSteps, "schema")
	transaction.UpdatedAt = transaction.UpdatedAt.Add(time.Second)
	if err := journal.Update(context.Background(), transaction); err != nil {
		t.Fatalf("Update() with dead-process lock error = %v", err)
	}
	if _, err := os.Lstat(path + ".lock"); !os.IsNotExist(err) {
		t.Fatalf("stale lock remains after update: %v", err)
	}
}

func TestFileTransactionJournalApplyOwnershipIsExclusiveAcrossInstances(t *testing.T) {
	path := filepath.Join(t.TempDir(), "install", "transaction.json")
	first := installplatform.NewFileTransactionJournal(path)
	second := installplatform.NewFileTransactionJournal(path)

	releaseFirst, err := first.AcquireApply(context.Background())
	if err != nil {
		t.Fatalf("first AcquireApply() error = %v", err)
	}
	if release, err := second.AcquireApply(context.Background()); !errors.Is(err, installer.ErrApplyBusy) {
		if release != nil {
			release()
		}
		t.Fatalf("second AcquireApply() error = %v, want ErrApplyBusy", err)
	}
	releaseFirst()
	releaseSecond, err := second.AcquireApply(context.Background())
	if err != nil {
		t.Fatalf("AcquireApply() after release error = %v", err)
	}
	releaseSecond()
}

func TestFileTransactionJournalApplyReleasePreservesSameBytesReplacement(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "install")
	applyLease := filepath.Join(stateDir, "apply.lock")
	journal := installplatform.NewFileTransactionJournal(filepath.Join(stateDir, "transaction.json"))
	release, err := journal.AcquireApply(context.Background())
	if err != nil {
		t.Fatalf("AcquireApply() error = %v", err)
	}
	originalInfo, err := os.Lstat(applyLease)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(applyLease)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(applyLease); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(applyLease, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	replacementInfo, err := os.Lstat(applyLease)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(originalInfo, replacementInfo) {
		t.Fatal("replacement unexpectedly reused the acquired lease inode")
	}

	release()

	if got, err := os.ReadFile(applyLease); err != nil || string(got) != string(contents) {
		t.Fatalf("same-bytes replacement removed by stale release: %q error=%v", got, err)
	}
}

func TestFileTransactionJournalApplyOwnershipRespectsAdminInitLease(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "install")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	adminLease := filepath.Join(stateDir, "admin-init.lock")
	adminBytes := []byte("admin init owns workspace mutation\n")
	if err := os.WriteFile(adminLease, adminBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	journal := installplatform.NewFileTransactionJournal(filepath.Join(stateDir, "transaction.json"))

	if release, err := journal.AcquireApply(context.Background()); !errors.Is(err, installer.ErrApplyBusy) {
		if release != nil {
			release()
		}
		t.Fatalf("AcquireApply() error = %v, want ErrApplyBusy", err)
	}
	if got, err := os.ReadFile(adminLease); err != nil || string(got) != string(adminBytes) {
		t.Fatalf("admin init lease changed: %q error=%v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(stateDir, "apply.lock")); !os.IsNotExist(err) {
		t.Fatalf("apply lease remains after admin-init contention: %v", err)
	}
}

func TestApplyJobDoesNotFailInstallationRunningAfterAdminInitProcessExited(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "install")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	adminLease := filepath.Join(stateDir, "admin-init.lock")
	staleAdminLease := []byte(`{"schema":2,"owner":"admin-init","id":"12345678-1234-1234-1234-123456789abc","pid":99999999,"pidStartToken":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","createdAt":"2026-08-24T00:00:00.000Z"}` + "\n")
	if err := os.WriteFile(adminLease, staleAdminLease, 0o600); err != nil {
		t.Fatal(err)
	}

	journal := installplatform.NewFileTransactionJournal(filepath.Join(stateDir, "transaction.json"))
	jobs := installer.NewApplyJobService(&applyOwnershipRunner{journal: journal})
	t.Cleanup(func() { _ = jobs.Close() })
	request := installer.ApplyRequest{
		Mode: "dev",
		Database: installer.DatabaseConnection{
			Driver: "postgres", Mode: "single", Host: "localhost", Port: 15432, Database: "gin_vben_admin", Username: "root",
		},
		Redis: installer.RedisConnection{Mode: "single", Addr: "localhost:6379"},
		Admin: installer.AdminAccount{Username: "admin", Password: "TestAdminPassword123!"},
	}
	job, err := jobs.Start(context.Background(), request)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		progress, progressErr := jobs.Progress(context.Background(), job.ID)
		if progressErr != nil {
			t.Fatalf("Progress() error = %v", progressErr)
		}
		if progress.State == installer.JobCompleted {
			if _, statErr := os.Lstat(adminLease); !os.IsNotExist(statErr) {
				t.Fatalf("exited admin init lease remains after apply: %v", statErr)
			}
			if _, statErr := os.Lstat(adminLease + ".reclaim"); !os.IsNotExist(statErr) {
				t.Fatalf("admin init reclaim tombstone remains after apply: %v", statErr)
			}
			return
		}
		if progress.State == installer.JobFailed {
			t.Fatalf("job changed queued -> failed: errorCode=%d errorKey=%q, want completed", progress.ErrorCode, progress.ErrorKey)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("job %s did not complete", job.ID)
}

func TestFileTransactionJournalStartupReconcileRemovesDeadApplyLeaseWithoutTouchingActiveAdminInit(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "install")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	applyLease := filepath.Join(stateDir, "apply.lock")
	staleBytes := []byte(`{"schema":1,"pid":99999999,"createdAt":"2026-08-23T00:00:00Z"}` + "\n")
	if err := os.WriteFile(applyLease, staleBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	adminLease := filepath.Join(stateDir, "admin-init.lock")
	adminBytes := []byte("admin init owns workspace mutation\n")
	if err := os.WriteFile(adminLease, adminBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	journal := installplatform.NewFileTransactionJournal(filepath.Join(stateDir, "transaction.json"))

	if err := journal.ReconcileApplyLease(context.Background()); err != nil {
		t.Fatalf("ReconcileApplyLease() error = %v", err)
	}
	if _, err := os.Lstat(applyLease); !os.IsNotExist(err) {
		t.Fatalf("dead apply lease remains while admin init is active: %v", err)
	}
	if got, err := os.ReadFile(adminLease); err != nil || string(got) != string(adminBytes) {
		t.Fatalf("admin init lease changed: %q error=%v", got, err)
	}
}

func TestFileTransactionJournalStartupReconcileNeverPublishesOrRemovesUnconfirmedApplyLease(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		stateDir := filepath.Join(t.TempDir(), "install")
		applyLease := filepath.Join(stateDir, "apply.lock")
		journal := installplatform.NewFileTransactionJournal(filepath.Join(stateDir, "transaction.json"))
		if err := journal.ReconcileApplyLease(context.Background()); err != nil {
			t.Fatalf("ReconcileApplyLease() error = %v", err)
		}
		if _, err := os.Lstat(applyLease); !os.IsNotExist(err) {
			t.Fatalf("startup reconciliation published an apply lease: %v", err)
		}
	})

	t.Run("active", func(t *testing.T) {
		stateDir := filepath.Join(t.TempDir(), "install")
		applyLease := filepath.Join(stateDir, "apply.lock")
		owner := installplatform.NewFileTransactionJournal(filepath.Join(stateDir, "transaction.json"))
		release, err := owner.AcquireApply(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		before, err := os.ReadFile(applyLease)
		if err != nil {
			t.Fatal(err)
		}
		observer := installplatform.NewFileTransactionJournal(filepath.Join(stateDir, "transaction.json"))
		if err := observer.ReconcileApplyLease(context.Background()); !errors.Is(err, installer.ErrApplyBusy) {
			t.Fatalf("ReconcileApplyLease() error = %v, want ErrApplyBusy", err)
		}
		if got, err := os.ReadFile(applyLease); err != nil || string(got) != string(before) {
			t.Fatalf("active apply lease changed: %q error=%v", got, err)
		}
	})

	t.Run("invalid", func(t *testing.T) {
		stateDir := filepath.Join(t.TempDir(), "install")
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		applyLease := filepath.Join(stateDir, "apply.lock")
		invalid := []byte(`{"schema":`)
		if err := os.WriteFile(applyLease, invalid, 0o600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-time.Hour)
		if err := os.Chtimes(applyLease, old, old); err != nil {
			t.Fatal(err)
		}
		journal := installplatform.NewFileTransactionJournal(filepath.Join(stateDir, "transaction.json"))
		if err := journal.ReconcileApplyLease(context.Background()); !errors.Is(err, installer.ErrApplyBusy) {
			t.Fatalf("ReconcileApplyLease() error = %v, want ErrApplyBusy", err)
		}
		if got, err := os.ReadFile(applyLease); err != nil || string(got) != string(invalid) {
			t.Fatalf("invalid apply lease changed: %q error=%v", got, err)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		stateDir := filepath.Join(t.TempDir(), "install")
		if err := os.MkdirAll(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(stateDir, "external.lock")
		targetBytes := []byte(`{"schema":1,"pid":99999999,"createdAt":"2026-08-23T00:00:00Z"}` + "\n")
		if err := os.WriteFile(target, targetBytes, 0o600); err != nil {
			t.Fatal(err)
		}
		applyLease := filepath.Join(stateDir, "apply.lock")
		if err := os.Symlink(target, applyLease); err != nil {
			t.Fatal(err)
		}
		journal := installplatform.NewFileTransactionJournal(filepath.Join(stateDir, "transaction.json"))
		if err := journal.ReconcileApplyLease(context.Background()); !errors.Is(err, installer.ErrApplyBusy) {
			t.Fatalf("ReconcileApplyLease() error = %v, want ErrApplyBusy", err)
		}
		info, err := os.Lstat(applyLease)
		if err != nil {
			t.Fatalf("apply symlink disappeared: %v", err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("apply symlink mode = %v", info.Mode())
		}
		if got, err := os.ReadFile(target); err != nil || string(got) != string(targetBytes) {
			t.Fatalf("symlink target changed: %q error=%v", got, err)
		}
	})
}

func validApplyTransaction() installer.ApplyTransaction {
	return installer.ApplyTransaction{
		Schema: installer.ApplyTransactionSchema, Owner: installer.ApplyTransactionOwner,
		ID: "install-0123456789abcdef0123456789abcdef", SelectedUI: installstate.UIAntd,
		Mode: installstate.ModeDev, DatabaseTarget: strings.Repeat("d", 64), Phase: installer.TransactionApplying, CurrentStep: "schema",
		CompletedSteps: []string{"plan", "database", "redis"},
		UpdatedAt:      time.Date(2026, time.August, 24, 9, 0, 0, 0, time.UTC),
	}
}

type applyOwnershipRunner struct {
	journal *installplatform.FileTransactionJournal
}

func (r *applyOwnershipRunner) ApplyWithProgress(ctx context.Context, request installer.ApplyRequest, report func(string)) (installer.ApplyResult, error) {
	release, err := r.journal.AcquireApply(ctx)
	if err != nil {
		return installer.ApplyResult{}, err
	}
	defer release()
	report("plan")
	return installer.ApplyResult{
		State: installer.StateInstalled, SelectedUI: installstate.UIEle, Mode: installstate.Mode(request.Mode),
		Steps: []installer.ApplyStep{{ID: "plan", Status: installer.StepCompleted}},
	}, nil
}
