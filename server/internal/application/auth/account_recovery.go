package auth

import (
	"context"
	"errors"
	"strings"

	"example.com/gin-vben-admin/server/internal/domain/authdomain"
)

// AccountProvisioner owns writes to the users credential record. It is kept
// separate from UserRepository so read-only authentication fakes and adapters
// do not acquire account-management responsibilities accidentally.
type AccountProvisioner interface {
	CreateUser(context.Context, authdomain.User) error
	UpdatePassword(context.Context, string, string) error
}

// PasswordResetProvider owns one-time token delivery and consumption. A
// transport must never return the issued token; delivery is provider-owned.
type PasswordResetProvider interface {
	Request(context.Context, string) error
	Consume(context.Context, string) (string, error)
}

// SetAccountProvisioner enables registration and password replacement.
func (s *Service) SetAccountProvisioner(provisioner AccountProvisioner) {
	if s != nil {
		s.accounts = provisioner
	}
}

// SetPasswordResetProvider enables one-time password reset requests.
func (s *Service) SetPasswordResetProvider(provider PasswordResetProvider) {
	if s != nil {
		s.reset = provider
	}
}

// Register creates an active account after hashing its password. The
// plaintext password never crosses the AccountProvisioner boundary.
func (s *Service) Register(ctx context.Context, identifier, password string) error {
	identifier = strings.TrimSpace(identifier)
	if s == nil || s.accounts == nil {
		return authdomain.ErrDependencyUnavailable
	}
	if err := validateAccountInput(identifier, password); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return authdomain.ErrDependencyUnavailable
	}
	err = s.accounts.CreateUser(ctx, authdomain.User{Identifier: identifier, PasswordHash: hash, Active: true})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, authdomain.ErrUserAlreadyExists):
		return authdomain.ErrUserAlreadyExists
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return authdomain.ErrDependencyUnavailable
	}
}

// RequestPasswordReset deliberately returns success for an unknown account,
// preventing the endpoint from becoming an account-enumeration oracle.
func (s *Service) RequestPasswordReset(ctx context.Context, identifier string) error {
	identifier = strings.TrimSpace(identifier)
	if s == nil || s.reset == nil {
		return authdomain.ErrDependencyUnavailable
	}
	if identifier == "" {
		return authdomain.ErrInvalidAccount
	}
	user, err := s.users.FindByIdentifier(ctx, identifier)
	if err != nil {
		if errors.Is(err, authdomain.ErrInvalidCredentials) {
			return nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return authdomain.ErrDependencyUnavailable
	}
	if user.ID == "" || !user.Active {
		return nil
	}
	if err := s.reset.Request(ctx, user.Identifier); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return authdomain.ErrDependencyUnavailable
	}
	return nil
}

// ResetPassword consumes a provider token exactly once and stores a new hash.
func (s *Service) ResetPassword(ctx context.Context, token, password string) error {
	if s == nil || s.reset == nil || s.accounts == nil {
		return authdomain.ErrDependencyUnavailable
	}
	if strings.TrimSpace(token) == "" {
		return authdomain.ErrPasswordResetInvalid
	}
	if err := validatePassword(password); err != nil {
		return err
	}
	identifier, err := s.reset.Consume(ctx, token)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return authdomain.ErrPasswordResetInvalid
	}
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return authdomain.ErrPasswordResetInvalid
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return authdomain.ErrDependencyUnavailable
	}
	if err := s.accounts.UpdatePassword(ctx, identifier, hash); err != nil {
		if errors.Is(err, authdomain.ErrInvalidCredentials) {
			return authdomain.ErrPasswordResetInvalid
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return authdomain.ErrDependencyUnavailable
	}
	return nil
}

func validateAccountInput(identifier, password string) error {
	if identifier == "" {
		return authdomain.ErrInvalidAccount
	}
	return validatePassword(password)
}

func validatePassword(password string) error {
	if len([]byte(password)) < 8 || len([]byte(password)) > 128 {
		return authdomain.ErrInvalidAccount
	}
	return nil
}
