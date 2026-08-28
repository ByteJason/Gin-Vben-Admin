package installer

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrInvalidInitialAdminPassword = errors.New("initial administrator password is invalid")
	ErrInitialAdminPasswordReset   = errors.New("initial administrator password reset failed")
)

// InitialAdminPasswordStore resolves and changes only the administrator
// recorded in the completed installation receipt.
type InitialAdminPasswordStore interface {
	InitialAdminIdentifier(context.Context) (string, error)
	ResetInitialAdminPassword(context.Context, string, string) error
}

type InitialAdminPasswordHasher interface {
	Hash(string) (string, error)
}

type InitialAdminLoginAttemptResetter interface {
	Reset(context.Context, string) error
}

// InitialAdminPasswordResetService is the application boundary used by the
// local recovery command. It is deliberately not exposed through HTTP.
type InitialAdminPasswordResetService struct {
	store    InitialAdminPasswordStore
	hasher   InitialAdminPasswordHasher
	attempts InitialAdminLoginAttemptResetter
}

func NewInitialAdminPasswordResetService(store InitialAdminPasswordStore, hasher InitialAdminPasswordHasher, attempts InitialAdminLoginAttemptResetter) *InitialAdminPasswordResetService {
	return &InitialAdminPasswordResetService{store: store, hasher: hasher, attempts: attempts}
}

func (s *InitialAdminPasswordResetService) Reset(ctx context.Context, password string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.store == nil || s.hasher == nil || s.attempts == nil {
		return ErrInitialAdminPasswordReset
	}
	if !IsValidInitialAdminPassword(password) {
		return ErrInvalidInitialAdminPassword
	}
	passwordHash, err := s.hasher.Hash(password)
	if err != nil || strings.TrimSpace(passwordHash) == "" {
		return ErrInitialAdminPasswordReset
	}
	identifier, err := s.store.InitialAdminIdentifier(ctx)
	if err != nil || strings.TrimSpace(identifier) == "" {
		return ErrInitialAdminPasswordReset
	}
	// Clear shared lockout state before committing the password change. A
	// Redis failure therefore leaves the existing credential intact; clearing
	// a counter followed by a database failure is harmless and retryable.
	if err := s.attempts.Reset(ctx, identifier); err != nil {
		return ErrInitialAdminPasswordReset
	}
	if err := s.store.ResetInitialAdminPassword(ctx, identifier, passwordHash); err != nil {
		return ErrInitialAdminPasswordReset
	}
	return nil
}
