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
	username := strings.TrimSpace(account.Username)
	passwordLength := len([]byte(account.Password))
	if len(username) < 3 || len(username) > 64 || strings.ContainsAny(username, "\x00\r\n") || passwordLength < 12 || passwordLength > 128 || strings.ContainsAny(account.Password, "\x00\r\n") {
		return installer.IdentityReceipt{}, ErrIdentityInstallation
	}
	passwordHash, err := s.hasher.Hash(account.Password)
	if err != nil || strings.TrimSpace(passwordHash) == "" {
		return installer.IdentityReceipt{}, ErrIdentityInstallation
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
	store, err := s.open(database)
	if err != nil {
		return installer.IdentityReceipt{}, ErrIdentityInstallation
	}
	if err := store.Initialize(ctx, reference, username, passwordHash); err != nil {
		_ = store.Close()
		return installer.IdentityReceipt{}, ErrIdentityInstallation
	}
	s.mutex.Lock()
	s.pending[reference] = database
	s.mutex.Unlock()
	receipt := installer.IdentityReceipt{Reference: reference}
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
		return ErrIdentityInstallation
	}
	store, err := s.open(database)
	if err != nil {
		return ErrIdentityInstallation
	}
	rollbackErr := store.Rollback(ctx, receipt.Reference)
	closeErr := store.Close()
	if rollbackErr != nil {
		return ErrIdentityInstallation
	}
	s.mutex.Lock()
	delete(s.pending, receipt.Reference)
	s.mutex.Unlock()
	if closeErr != nil {
		return ErrIdentityInstallation
	}
	return nil
}
