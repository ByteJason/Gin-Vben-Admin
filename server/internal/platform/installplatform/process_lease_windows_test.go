//go:build windows

package installplatform_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/installplatform"
)

func TestWindowsApplyLeaseCanBeAcquiredAgainAfterRelease(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "install")
	applyLease := filepath.Join(stateDir, "apply.lock")
	journal := installplatform.NewFileTransactionJournal(filepath.Join(stateDir, "transaction.json"))

	release, err := journal.AcquireApply(context.Background())
	if err != nil {
		t.Fatalf("first AcquireApply() error = %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("first release error = %v", err)
	}
	if _, err := os.Lstat(applyLease); !os.IsNotExist(err) {
		t.Fatalf("released Windows apply lease remains: %v", err)
	}

	release, err = journal.AcquireApply(context.Background())
	if err != nil {
		t.Fatalf("second AcquireApply() error = %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("second release error = %v", err)
	}
}
