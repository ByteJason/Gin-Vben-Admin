package installplatform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
)

const maxApplyTransactionBytes = 64 << 10

// FileTransactionJournal owns only server-installer envelopes. The admin init
// CLI intentionally shares the path with owner=admin-init; that owner is
// treated as busy and is never decoded, updated, removed, or replaced here.
type FileTransactionJournal struct {
	path                string
	workspaceRoot       string
	publish             func(string, string) error
	acquireApplyLease   func(string, string) (func() error, error)
	acquireJournalLease func(string) (func() error, error)
}

func NewFileTransactionJournal(path string, workspaceRoot ...string) *FileTransactionJournal {
	root := ""
	if len(workspaceRoot) > 0 {
		root = filepath.Clean(workspaceRoot[0])
	} else {
		root = filepath.Clean(filepath.Join(filepath.Dir(path), "..", ".."))
	}
	return newFileTransactionJournal(path, root, os.Link)
}

func newFileTransactionJournal(path, workspaceRoot string, publish func(string, string) error) *FileTransactionJournal {
	if publish == nil {
		publish = os.Link
	}
	return &FileTransactionJournal{
		path:                filepath.Clean(path),
		workspaceRoot:       filepath.Clean(workspaceRoot),
		publish:             publish,
		acquireApplyLease:   acquireProcessLeaseWithGuard,
		acquireJournalLease: acquireProcessLease,
	}
}

func (s *FileTransactionJournal) Load(ctx context.Context) (installer.ApplyTransaction, bool, error) {
	if err := contextError(ctx); err != nil {
		return installer.ApplyTransaction{}, false, err
	}
	if s == nil || s.path == "" || s.path == "." {
		return installer.ApplyTransaction{}, false, errors.New("installation transaction journal path is required")
	}
	contents, exists, err := readRegularFile(s.path, maxApplyTransactionBytes)
	if !exists && err == nil {
		return installer.ApplyTransaction{}, false, nil
	}
	if err != nil {
		return installer.ApplyTransaction{}, exists, fmt.Errorf("read installation transaction journal: %w", err)
	}
	var envelope struct {
		Owner string `json:"owner"`
	}
	if err := json.Unmarshal(contents, &envelope); err != nil {
		return installer.ApplyTransaction{}, true, errors.New("decode installation transaction journal owner")
	}
	if envelope.Owner != installer.ApplyTransactionOwner {
		if envelope.Owner == "admin-init" {
			return installer.ApplyTransaction{}, true, installer.ErrApplyBusy
		}
		return installer.ApplyTransaction{}, true, errors.New("installation transaction journal owner is invalid")
	}

	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var transaction installer.ApplyTransaction
	if err := decoder.Decode(&transaction); err != nil {
		return installer.ApplyTransaction{}, true, errors.New("decode installation transaction journal")
	}
	if err := ensureJournalJSONEnd(decoder); err != nil {
		return installer.ApplyTransaction{}, true, err
	}
	if err := transaction.Validate(); err != nil {
		return installer.ApplyTransaction{}, true, fmt.Errorf("validate installation transaction journal: %w", err)
	}
	return transaction, true, nil
}

func (s *FileTransactionJournal) Create(ctx context.Context, transaction installer.ApplyTransaction) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.path == "" || s.path == "." {
		return errors.New("installation transaction journal path is required")
	}
	if err := transaction.Validate(); err != nil {
		return fmt.Errorf("validate installation transaction journal: %w", err)
	}
	encoded, err := encodeApplyTransaction(transaction)
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create installation transaction directory: %w", err)
	}
	temporary, err := os.CreateTemp(dir, ".transaction.json.tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary installation transaction journal: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := writeAndSync(temporary, encoded); err != nil {
		return fmt.Errorf("write installation transaction journal: %w", err)
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := s.publish(temporaryPath, s.path); errors.Is(err, os.ErrExist) {
		return installer.ErrApplyBusy
	} else if err != nil {
		return fmt.Errorf("publish installation transaction journal: %w", err)
	}
	if err := os.Remove(temporaryPath); err != nil {
		return fmt.Errorf("remove temporary installation transaction journal: %w", err)
	}
	if err := syncDirectory(dir); err != nil {
		return fmt.Errorf("sync installation transaction directory: %w", err)
	}
	return nil
}

func (s *FileTransactionJournal) Update(ctx context.Context, transaction installer.ApplyTransaction) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := transaction.Validate(); err != nil {
		return fmt.Errorf("validate installation transaction journal: %w", err)
	}
	return s.withLock(ctx, func() error {
		current, exists, err := s.Load(ctx)
		if err != nil {
			return err
		}
		if !exists || current.ID != transaction.ID {
			return installer.ErrTransactionChanged
		}
		encoded, err := encodeApplyTransaction(transaction)
		if err != nil {
			return err
		}
		if err := publishPrivateFile(s.path, encoded); err != nil {
			return fmt.Errorf("publish installation transaction journal: %w", err)
		}
		return nil
	})
}

func (s *FileTransactionJournal) Remove(ctx context.Context, id string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if !validJournalTransactionID(id) {
		return installer.ErrTransactionChanged
	}
	return s.withLock(ctx, func() error {
		current, exists, err := s.Load(ctx)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		if current.ID != id {
			return installer.ErrTransactionChanged
		}
		if err := os.Remove(s.path); err != nil {
			return fmt.Errorf("remove installation transaction journal: %w", err)
		}
		if err := syncDirectory(filepath.Dir(s.path)); err != nil {
			return fmt.Errorf("sync installation transaction directory: %w", err)
		}
		return nil
	})
}

// AcquireApply owns a process lease for the entire apply/recovery workflow.
// Per-operation journal locks alone are insufficient because another server
// process could otherwise compensate a transaction that is still active.
func (s *FileTransactionJournal) AcquireApply(ctx context.Context) (func() error, error) {
	if s == nil || s.path == "" || s.path == "." {
		return nil, errors.New("installation transaction journal path is required")
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	stateDir := filepath.Dir(s.path)
	acquire := s.acquireApplyLease
	if acquire == nil {
		acquire = acquireProcessLeaseWithGuard
	}
	release, err := acquire(
		filepath.Join(stateDir, "apply.lock"),
		filepath.Join(stateDir, "process.guard"),
	)
	if errors.Is(err, errProcessLeaseBusy) {
		return nil, installer.ErrApplyBusy
	}
	if err != nil {
		return nil, fmt.Errorf("acquire installation apply lease: %w", err)
	}
	// The dependency-free Node initializer owns admin-init.lock and only checks
	// apply.lock after publishing that lease. Checking its lease while we own
	// apply.lock closes both acquisition orders without asking Node to emulate
	// this package's flock/LockFileEx process.guard protocol.
	if _, statErr := os.Lstat(filepath.Join(stateDir, "admin-init.lock")); statErr == nil || !errors.Is(statErr, os.ErrNotExist) {
		if releaseErr := release(); releaseErr != nil {
			return nil, fmt.Errorf("release installation apply lease after admin init collision: %w", releaseErr)
		}
		return nil, installer.ErrApplyBusy
	}
	return release, nil
}

// ReconcileApplyLease reclaims a dead server apply owner during startup, then
// immediately releases the temporary ownership acquired for reconciliation.
func (s *FileTransactionJournal) ReconcileApplyLease(ctx context.Context) error {
	if s == nil || s.path == "" || s.path == "." {
		return errors.New("installation transaction journal path is required")
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	stateDir := filepath.Dir(s.path)
	err := reconcileStaleProcessLeaseWithGuard(
		filepath.Join(stateDir, "apply.lock"),
		filepath.Join(stateDir, "process.guard"),
	)
	if errors.Is(err, errProcessLeaseBusy) {
		return installer.ErrApplyBusy
	}
	if err != nil {
		return fmt.Errorf("reconcile installation apply lease: %w", err)
	}
	return nil
}

func (s *FileTransactionJournal) withLock(ctx context.Context, operation func() error) error {
	if s == nil || s.path == "" || s.path == "." {
		return errors.New("installation transaction journal path is required")
	}
	if err := contextError(ctx); err != nil {
		return err
	}
	lockPath := s.path + ".lock"
	acquire := s.acquireJournalLease
	if acquire == nil {
		acquire = acquireProcessLease
	}
	release, err := acquire(lockPath)
	if errors.Is(err, errProcessLeaseBusy) {
		return installer.ErrApplyBusy
	}
	if err != nil {
		return fmt.Errorf("acquire installation transaction lock: %w", err)
	}
	operationErr := operation()
	releaseErr := release()
	if releaseErr != nil {
		releaseErr = fmt.Errorf("release installation transaction lock: %w", releaseErr)
	}
	return errors.Join(operationErr, releaseErr)
}

func encodeApplyTransaction(transaction installer.ApplyTransaction) ([]byte, error) {
	encoded, err := json.MarshalIndent(transaction, "", "  ")
	if err != nil {
		return nil, errors.New("encode installation transaction journal")
	}
	return append(encoded, '\n'), nil
}

func ensureJournalJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	}
	return errors.New("installation transaction journal contains trailing data")
}

func validJournalTransactionID(id string) bool {
	if !strings.HasPrefix(id, "install-") || len(id) != len("install-")+32 {
		return false
	}
	for _, character := range strings.TrimPrefix(id, "install-") {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
