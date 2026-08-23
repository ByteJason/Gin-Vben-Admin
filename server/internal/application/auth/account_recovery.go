package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
)

// AccountProvisioner owns writes to the users credential record. It is kept
// separate from UserRepository so read-only authentication fakes and adapters
// do not acquire account-management responsibilities accidentally.
type AccountProvisioner interface {
	CreateUser(context.Context, authdomain.User) error
	UpdatePassword(context.Context, string, string) error
}

// AccountRecoveryService is the transport-facing seam for account creation
// and password recovery. It is separate from AuthService so existing login
// consumers do not need to implement write operations.
type AccountRecoveryService interface {
	Register(context.Context, string, string) error
	RequestPasswordReset(context.Context, string) error
	ResetPassword(context.Context, string, string) error
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

var _ AccountRecoveryService = (*Service)(nil)

// Register creates an active account after hashing its password. The
// plaintext password never crosses the AccountProvisioner boundary.
func (s *Service) Register(ctx context.Context, identifier, password string) error {
	if s == nil || s.accounts == nil {
		return authdomain.ErrDependencyUnavailable
	}
	canonical, identifierType, err := normalizeAccountIdentifier(identifier)
	if err != nil {
		return err
	}
	if err := validateAccountInput(canonical, password); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return authdomain.ErrDependencyUnavailable
	}
	// Keep the legacy users.username NOT NULL key populated even when the
	// caller chooses an email identifier; email remains an additional alias.
	user := authdomain.User{Identifier: canonical, Username: canonical, PasswordHash: hash, Active: true}
	if identifierType == authdomain.IdentifierEmail {
		user.Email = canonical
	} else {
		user.Username = canonical
	}
	err = s.accounts.CreateUser(ctx, user)
	switch {
	case err == nil:
		_ = s.recordAudit(ctx, authdomain.AuditEvent{EventType: authdomain.AuditRegister, Outcome: authdomain.AuditOutcomeSuccess})
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
	if s == nil || s.reset == nil {
		return authdomain.ErrDependencyUnavailable
	}
	canonical, _, err := normalizeAccountIdentifier(identifier)
	if err != nil {
		return err
	}
	user, err := s.users.FindByIdentifier(ctx, canonical)
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
	var requestErr error
	if recipientProvider, ok := s.reset.(PasswordResetRecipientProvider); ok {
		// Username requests use the already-loaded profile email. If a profile
		// has no email, keep the enumeration-safe success response and leave the
		// administrator/development reset path available.
		if strings.TrimSpace(user.Email) == "" {
			return nil
		}
		requestErr = recipientProvider.RequestTo(ctx, canonical, user.Email)
	} else {
		requestErr = s.reset.Request(ctx, canonical)
	}
	if requestErr != nil {
		if errors.Is(requestErr, context.Canceled) || errors.Is(requestErr, context.DeadlineExceeded) {
			return requestErr
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
	canonical, _, normalizeErr := normalizeAccountIdentifier(identifier)
	if normalizeErr != nil {
		return authdomain.ErrPasswordResetInvalid
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return authdomain.ErrDependencyUnavailable
	}
	if err := s.accounts.UpdatePassword(ctx, canonical, hash); err != nil {
		if errors.Is(err, authdomain.ErrInvalidCredentials) {
			return authdomain.ErrPasswordResetInvalid
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return authdomain.ErrDependencyUnavailable
	}
	_ = s.recordAudit(ctx, authdomain.AuditEvent{EventType: authdomain.AuditPasswordReset, Outcome: authdomain.AuditOutcomeSuccess})
	return nil
}

func validateAccountInput(identifier, password string) error {
	if identifier == "" {
		return authdomain.ErrInvalidAccount
	}
	return validatePassword(password)
}

func normalizeAccountIdentifier(identifier string) (string, authdomain.IdentifierType, error) {
	canonical, identifierType, err := authdomain.NormalizeIdentifier(identifier)
	if err != nil {
		return "", "", authdomain.ErrInvalidAccount
	}
	return canonical, identifierType, nil
}

func validatePassword(password string) error {
	if len([]byte(password)) < 8 || len([]byte(password)) > 128 {
		return authdomain.ErrInvalidAccount
	}
	return nil
}
