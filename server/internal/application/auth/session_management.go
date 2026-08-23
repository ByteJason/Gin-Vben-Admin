package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
)

// SessionQuery owns durable device-session reads and user-scoped revocation.
// Implementations must enforce the user boundary in the database as well as
// in the application layer.
type SessionQuery interface {
	ListByUser(context.Context, string) ([]authdomain.Session, error)
	RevokeOwned(context.Context, string, string) error
}

// SessionManagementService is the transport-facing seam for device sessions.
type SessionManagementService interface {
	ListSessions(context.Context, string) ([]authdomain.Session, error)
	RevokeSession(context.Context, string, string) error
}

func (s *Service) SetSessionQuery(query SessionQuery) {
	if s != nil {
		s.query = query
	}
}

var _ SessionManagementService = (*Service)(nil)

func (s *Service) ListSessions(ctx context.Context, userID string) ([]authdomain.Session, error) {
	if s == nil || s.query == nil || strings.TrimSpace(userID) == "" {
		return nil, authdomain.ErrDependencyUnavailable
	}
	sessions, err := s.query.ListByUser(ctx, strings.TrimSpace(userID))
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, authdomain.ErrDependencyUnavailable
	}
	filtered := make([]authdomain.Session, 0, len(sessions))
	for _, session := range sessions {
		if session.UserID == userID {
			filtered = append(filtered, session)
		}
	}
	return filtered, nil
}

func (s *Service) RevokeSession(ctx context.Context, userID, sessionID string) error {
	if s == nil || s.query == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(sessionID) == "" {
		return authdomain.ErrDependencyUnavailable
	}
	userID = strings.TrimSpace(userID)
	sessionID = strings.TrimSpace(sessionID)
	owned, err := s.query.ListByUser(ctx, userID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return authdomain.ErrDependencyUnavailable
	}
	found := false
	for _, session := range owned {
		if session.ID == sessionID && session.UserID == userID {
			found = true
			break
		}
	}
	if !found {
		return authdomain.ErrSessionNotFound
	}
	// Validate ownership before touching the runtime store. A missing Redis
	// key is tolerated because the durable row still needs to be revoked.
	if s.sess != nil {
		if runtimeErr := s.sess.Revoke(ctx, sessionID); runtimeErr != nil &&
			!errors.Is(runtimeErr, authdomain.ErrSessionNotFound) &&
			!errors.Is(runtimeErr, authdomain.ErrSessionRevoked) {
			if errors.Is(runtimeErr, context.Canceled) || errors.Is(runtimeErr, context.DeadlineExceeded) {
				return runtimeErr
			}
			return authdomain.ErrDependencyUnavailable
		}
	}
	err = s.query.RevokeOwned(ctx, userID, sessionID)
	if err == nil {
		return nil
	}
	if errors.Is(err, authdomain.ErrSessionNotFound) || errors.Is(err, authdomain.ErrSessionRevoked) {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return authdomain.ErrDependencyUnavailable
}
