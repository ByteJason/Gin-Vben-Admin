// Package auth coordinates authentication use cases behind injectable ports.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"example.com/gin-vben-admin/server/internal/domain/authdomain"
)

type Service struct {
	users    authdomain.UserRepository
	hasher   authdomain.PasswordHasher
	tokens   authdomain.TokenService
	sess     authdomain.SessionStore
	journal  SessionJournal
	attempts LoginAttemptStore
	accounts AccountProvisioner
	reset    PasswordResetProvider
	query    SessionQuery
	audit    AuditSink
}

// SessionJournal records durable session lifecycle changes independently from
// the runtime session store. This lets Redis remain the fast revocation store
// while SQL retains an auditable auth_sessions history.
type SessionJournal interface {
	Create(context.Context, authdomain.Session) error
	Rotate(context.Context, string, string, string, time.Time) error
	Revoke(context.Context, string) error
}

// AuditSink persists authentication outcomes without coupling the application
// service to SQL, Redis, or a particular logging backend.
type AuditSink interface {
	Record(context.Context, authdomain.AuditEvent) error
}

// AuthService is the transport-facing seam. HTTP handlers should depend on
// this interface so tests can inject a deterministic fake.
type AuthService interface {
	Login(ctx context.Context, identifier, password string) (authdomain.TokenPair, error)
	Refresh(ctx context.Context, refreshToken string) (authdomain.TokenPair, error)
	Logout(ctx context.Context, sessionID string) error
	VerifyAccess(token string) (authdomain.Claims, error)
}

// RefreshLogoutService extends AuthService for handlers that receive only a
// refresh token in their logout payload.
type RefreshLogoutService interface {
	AuthService
	LogoutWithRefreshToken(ctx context.Context, refreshToken string) error
}

var _ AuthService = (*Service)(nil)
var _ RefreshLogoutService = (*Service)(nil)

func NewService(users authdomain.UserRepository, hasher authdomain.PasswordHasher, tokens authdomain.TokenService, sessions authdomain.SessionStore, attemptStores ...LoginAttemptStore) *Service {
	var attempts LoginAttemptStore
	if len(attemptStores) > 0 {
		attempts = attemptStores[0]
	}
	return &Service{users: users, hasher: hasher, tokens: tokens, sess: sessions, attempts: attempts}
}

// SetSessionJournal enables durable lifecycle recording without changing the
// runtime session store used for token validation and rotation.
func (s *Service) SetSessionJournal(journal SessionJournal) {
	if s != nil {
		s.journal = journal
	}
}

func (s *Service) SetAuditSink(sink AuditSink) {
	if s != nil {
		s.audit = sink
	}
}

func (s *Service) recordAudit(ctx context.Context, event authdomain.AuditEvent) error {
	if s == nil || s.audit == nil {
		return nil
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	return s.audit.Record(ctx, event)
}

func (s *Service) Login(ctx context.Context, identifier, password string) (authdomain.TokenPair, error) {
	if s.attempts != nil {
		locked, err := s.attempts.IsLocked(ctx, identifier)
		if err != nil {
			return authdomain.TokenPair{}, authdomain.ErrDependencyUnavailable
		}
		if locked {
			return authdomain.TokenPair{}, authdomain.ErrAccountLocked
		}
	}
	user, err := s.users.FindByIdentifier(ctx, identifier)
	if err != nil {
		if errors.Is(err, authdomain.ErrInvalidCredentials) {
			return s.failedLogin(ctx, identifier)
		}
		return authdomain.TokenPair{}, authdomain.ErrDependencyUnavailable
	}
	if !user.Active || s.hasher.Compare(user.PasswordHash, password) != nil {
		return s.failedLogin(ctx, identifier)
	}
	if s.attempts != nil {
		// Clear the failure state before issuing credentials so a reset error
		// cannot leave an apparently failed request with a live session.
		if err := s.attempts.Reset(ctx, identifier); err != nil {
			return authdomain.TokenPair{}, authdomain.ErrDependencyUnavailable
		}
	}
	sessionID, err := randomID()
	if err != nil {
		return authdomain.TokenPair{}, err
	}
	pair, err := s.tokens.Issue(user.ID, sessionID)
	if err != nil {
		return authdomain.TokenPair{}, err
	}
	claims, err := s.tokens.Parse(pair.RefreshToken)
	if err != nil {
		return authdomain.TokenPair{}, err
	}
	if claims.Type != authdomain.RefreshToken {
		return authdomain.TokenPair{}, authdomain.ErrInvalidToken
	}
	session := authdomain.Session{ID: sessionID, UserID: user.ID, RefreshJTI: claims.TokenID, ExpiresAt: claims.ExpiresAt}
	if err := s.sess.Create(ctx, session); err != nil {
		return authdomain.TokenPair{}, authdomain.ErrDependencyUnavailable
	}
	if s.journal != nil {
		if err := s.journal.Create(ctx, session); err != nil {
			_ = s.sess.Revoke(ctx, sessionID)
			return authdomain.TokenPair{}, authdomain.ErrDependencyUnavailable
		}
	}
	if err := s.recordAudit(ctx, authdomain.AuditEvent{
		UserID: user.ID, SessionID: session.ID, EventType: authdomain.AuditLogin,
		Outcome: authdomain.AuditOutcomeSuccess,
	}); err != nil {
		_ = s.sess.Revoke(ctx, session.ID)
		return authdomain.TokenPair{}, authdomain.ErrDependencyUnavailable
	}
	return pair, nil
}

func (s *Service) failedLogin(ctx context.Context, identifier string) (authdomain.TokenPair, error) {
	if s.attempts == nil {
		return authdomain.TokenPair{}, authdomain.ErrInvalidCredentials
	}
	locked, err := s.attempts.RecordFailure(ctx, identifier)
	if err != nil {
		return authdomain.TokenPair{}, authdomain.ErrDependencyUnavailable
	}
	if locked {
		return authdomain.TokenPair{}, authdomain.ErrAccountLocked
	}
	return authdomain.TokenPair{}, authdomain.ErrInvalidCredentials
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (authdomain.TokenPair, error) {
	claims, err := s.tokens.Parse(refreshToken)
	if err != nil || claims.Type != authdomain.RefreshToken {
		return authdomain.TokenPair{}, authdomain.ErrInvalidToken
	}
	session, err := s.sess.Get(ctx, claims.SessionID)
	if err != nil {
		if errors.Is(err, authdomain.ErrSessionNotFound) || errors.Is(err, authdomain.ErrSessionRevoked) {
			return authdomain.TokenPair{}, err
		}
		return authdomain.TokenPair{}, authdomain.ErrDependencyUnavailable
	}
	if session.UserID != claims.Subject {
		return authdomain.TokenPair{}, authdomain.ErrInvalidToken
	}
	if !session.MatchesRefreshJTI(claims.TokenID) {
		return authdomain.TokenPair{}, authdomain.ErrRefreshReplay
	}
	if session.Revoked || !session.ExpiresAt.After(time.Now()) {
		return authdomain.TokenPair{}, authdomain.ErrSessionRevoked
	}
	pair, err := s.tokens.Issue(claims.Subject, claims.SessionID)
	if err != nil {
		return authdomain.TokenPair{}, err
	}
	next, err := s.tokens.Parse(pair.RefreshToken)
	if err != nil {
		return authdomain.TokenPair{}, err
	}
	if err := s.sess.Rotate(ctx, claims.SessionID, claims.TokenID, next.TokenID, next.ExpiresAt); err != nil {
		if errors.Is(err, authdomain.ErrRefreshReplay) || errors.Is(err, authdomain.ErrSessionRevoked) || errors.Is(err, authdomain.ErrSessionNotFound) {
			return authdomain.TokenPair{}, err
		}
		return authdomain.TokenPair{}, authdomain.ErrDependencyUnavailable
	}
	if s.journal != nil {
		if err := s.journal.Rotate(ctx, claims.SessionID, claims.TokenID, next.TokenID, next.ExpiresAt); err != nil {
			_ = s.sess.Revoke(ctx, claims.SessionID)
			return authdomain.TokenPair{}, authdomain.ErrDependencyUnavailable
		}
	}
	if err := s.recordAudit(ctx, authdomain.AuditEvent{
		UserID: claims.Subject, SessionID: claims.SessionID, EventType: authdomain.AuditRefresh,
		Outcome: authdomain.AuditOutcomeSuccess,
	}); err != nil {
		_ = s.sess.Revoke(ctx, claims.SessionID)
		return authdomain.TokenPair{}, authdomain.ErrDependencyUnavailable
	}
	return pair, nil
}

func (s *Service) Logout(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return authdomain.ErrSessionNotFound
	}
	if err := s.sess.Revoke(ctx, sessionID); err != nil {
		if errors.Is(err, authdomain.ErrSessionNotFound) || errors.Is(err, authdomain.ErrSessionRevoked) {
			return err
		}
		return authdomain.ErrDependencyUnavailable
	}
	if s.journal != nil {
		if err := s.journal.Revoke(ctx, sessionID); err != nil {
			return authdomain.ErrDependencyUnavailable
		}
	}
	if err := s.recordAudit(ctx, authdomain.AuditEvent{
		SessionID: sessionID, EventType: authdomain.AuditLogout,
		Outcome: authdomain.AuditOutcomeSuccess,
	}); err != nil {
		return authdomain.ErrDependencyUnavailable
	}
	return nil
}

func (s *Service) LogoutWithRefreshToken(ctx context.Context, refreshToken string) error {
	claims, err := s.tokens.Parse(refreshToken)
	if err != nil || claims.Type != authdomain.RefreshToken {
		return authdomain.ErrInvalidToken
	}
	return s.Logout(ctx, claims.SessionID)
}

func (s *Service) VerifyAccess(token string) (authdomain.Claims, error) {
	claims, err := s.tokens.Parse(token)
	if err != nil || claims.Type != authdomain.AccessToken {
		return authdomain.Claims{}, authdomain.ErrInvalidToken
	}
	return claims, nil
}

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
