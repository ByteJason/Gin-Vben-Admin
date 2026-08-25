package installplatform

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"strings"
	"sync"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
)

var ErrIdentityInstallation = errors.New("initial administrator installation failed")

type InitialPasswordHasher interface {
	Hash(string) (string, error)
}

type IdentityStore interface {
	Initialize(context.Context, string, string, string) error
	Rollback(context.Context, string) error
	Close() error
}

type IdentityStoreFactory func(installer.DatabaseConnection) (IdentityStore, error)

type IdentityInstaller struct {
	open    IdentityStoreFactory
	hasher  InitialPasswordHasher
	random  io.Reader
	mutex   sync.Mutex
	pending map[string]installer.DatabaseConnection
}

func NewIdentityInstaller(factory IdentityStoreFactory, hasher InitialPasswordHasher, randomSource io.Reader) *IdentityInstaller {
	if randomSource == nil {
		randomSource = rand.Reader
	}
	return &IdentityInstaller{open: factory, hasher: hasher, random: randomSource, pending: make(map[string]installer.DatabaseConnection)}
}

func (s *IdentityInstaller) Initialize(ctx context.Context, database installer.DatabaseConnection, account installer.AdminAccount) (installer.IdentityReceipt, error) {
	if s == nil || s.open == nil || s.hasher == nil || s.random == nil {
		return installer.IdentityReceipt{}, ErrIdentityInstallation
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return installer.IdentityReceipt{}, err
	}
	s.mutex.Lock()
	reference, err := randomHex(s.random, 16)
	if err == nil {
		if _, collision := s.pending[reference]; collision {
			err = errors.New("identity reference collision")
		}
	}
	s.mutex.Unlock()
	if err != nil {
		return installer.IdentityReceipt{}, ErrIdentityInstallation
	}
	return s.initializeWithReference(ctx, database, account, reference, false)
}

// InitializeWithReference commits the identity under a recovery reference
// that ApplyService has already fsynced to its credential-free journal.
func (s *IdentityInstaller) InitializeWithReference(ctx context.Context, database installer.DatabaseConnection, account installer.AdminAccount, reference string) (installer.IdentityReceipt, error) {
	if s == nil || s.open == nil || s.hasher == nil || strings.TrimSpace(reference) == "" || len(reference) > 64 || strings.ContainsAny(reference, "\x00\r\n") {
		return installer.IdentityReceipt{}, ErrIdentityInstallation
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return installer.IdentityReceipt{}, err
	}
	s.mutex.Lock()
	_, collision := s.pending[reference]
	s.mutex.Unlock()
	if collision {
		return installer.IdentityReceipt{}, ErrIdentityInstallation
	}
	return s.initializeWithReference(ctx, database, account, reference, true)
}

func (s *IdentityInstaller) initializeWithReference(ctx context.Context, database installer.DatabaseConnection, account installer.AdminAccount, reference string, retainReceiptOnFailure bool) (installer.IdentityReceipt, error) {
	username := strings.TrimSpace(account.Username)
	passwordLength := len([]byte(account.Password))
	if len(username) < 3 || len(username) > 64 || strings.ContainsAny(username, "\x00\r\n") || passwordLength < 12 || passwordLength > 128 || strings.ContainsAny(account.Password, "\x00\r\n") {
		return installer.IdentityReceipt{}, ErrIdentityInstallation
	}
	passwordHash, err := s.hasher.Hash(account.Password)
	if err != nil || strings.TrimSpace(passwordHash) == "" {
		return installer.IdentityReceipt{}, ErrIdentityInstallation
	}
	receipt := installer.IdentityReceipt{Reference: reference}
	store, err := s.open(database)
	if err != nil {
		if retainReceiptOnFailure {
			return receipt, ErrIdentityInstallation
		}
		return installer.IdentityReceipt{}, ErrIdentityInstallation
	}
	if err := store.Initialize(ctx, reference, username, passwordHash); err != nil {
		_ = store.Close()
		var diagnostic installer.FailureDiagnosticProvider
		if errors.As(err, &diagnostic) {
			if retainReceiptOnFailure {
				return receipt, err
			}
			return installer.IdentityReceipt{}, err
		}
		if retainReceiptOnFailure {
			return receipt, ErrIdentityInstallation
		}
		return installer.IdentityReceipt{}, ErrIdentityInstallation
	}
	s.mutex.Lock()
	s.pending[reference] = database
	s.mutex.Unlock()
	if err := store.Close(); err != nil {
		return receipt, ErrIdentityInstallation
	}
	return receipt, nil
}

func (s *IdentityInstaller) Rollback(ctx context.Context, receipt installer.IdentityReceipt) error {
	if s == nil || s.open == nil || strings.TrimSpace(receipt.Reference) == "" {
		return ErrIdentityInstallation
	}
	s.mutex.Lock()
	database, ok := s.pending[receipt.Reference]
	s.mutex.Unlock()
	if !ok {
		// Prepared references are unique to one ApplyService transaction. Once
		// removed from pending, repeating compensation in the same process is an
		// idempotent success; restart recovery uses RecoverRollback with freshly
		// supplied database credentials instead.
		return nil
	}
	err := s.rollbackWithDatabase(ctx, database, receipt)
	if err != nil && !errors.Is(err, installer.ErrIdentityNotOwned) {
		return err
	}
	s.mutex.Lock()
	delete(s.pending, receipt.Reference)
	s.mutex.Unlock()
	return err
}

// RecoverRollback reconnects with freshly re-entered credentials and uses
// only the journal's opaque installation reference. Database connection
// material remains in memory and is never written to transaction.json.
func (s *IdentityInstaller) RecoverRollback(ctx context.Context, database installer.DatabaseConnection, receipt installer.IdentityReceipt) error {
	if s == nil || s.open == nil || strings.TrimSpace(receipt.Reference) == "" {
		return ErrIdentityInstallation
	}
	err := s.rollbackWithDatabase(ctx, database, receipt)
	if err != nil && !errors.Is(err, installer.ErrIdentityNotOwned) {
		return err
	}
	s.mutex.Lock()
	delete(s.pending, receipt.Reference)
	s.mutex.Unlock()
	return err
}

// Finalize forgets the database connection material retained only for a
// possible same-process rollback. The administrator row is already committed;
// repeating this call after journal recovery is an idempotent success.
func (s *IdentityInstaller) Finalize(ctx context.Context, receipt installer.IdentityReceipt) error {
	if s == nil || strings.TrimSpace(receipt.Reference) == "" {
		return ErrIdentityInstallation
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mutex.Lock()
	delete(s.pending, receipt.Reference)
	s.mutex.Unlock()
	return nil
}

func (s *IdentityInstaller) rollbackWithDatabase(ctx context.Context, database installer.DatabaseConnection, receipt installer.IdentityReceipt) error {
	store, err := s.open(database)
	if err != nil {
		return ErrIdentityInstallation
	}
	rollbackErr := store.Rollback(ctx, receipt.Reference)
	closeErr := store.Close()
	if rollbackErr != nil {
		if errors.Is(rollbackErr, installer.ErrIdentityNotOwned) && closeErr == nil {
			return installer.ErrIdentityNotOwned
		}
		return ErrIdentityInstallation
	}
	if closeErr != nil {
		return ErrIdentityInstallation
	}
	return nil
}
