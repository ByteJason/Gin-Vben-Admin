package authplatform

import (
	"context"
	"sync"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
)

type MemorySessionStore struct {
	mu       sync.Mutex
	sessions map[string]authdomain.Session
}

func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{sessions: make(map[string]authdomain.Session)}
}
func (*MemorySessionStore) checkContext(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
func (s *MemorySessionStore) Create(ctx context.Context, session authdomain.Session) error {
	if err := s.checkContext(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
	return nil
}
func (s *MemorySessionStore) Get(ctx context.Context, id string) (authdomain.Session, error) {
	if err := s.checkContext(ctx); err != nil {
		return authdomain.Session{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessions[id]
	if !ok {
		return authdomain.Session{}, authdomain.ErrSessionNotFound
	}
	return v, nil
}
func (s *MemorySessionStore) Rotate(ctx context.Context, id, expectedJTI, nextJTI string, expiresAt time.Time) error {
	if err := s.checkContext(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessions[id]
	if !ok {
		return authdomain.ErrSessionNotFound
	}
	if v.Revoked {
		return authdomain.ErrSessionRevoked
	}
	if v.RefreshJTI != expectedJTI {
		return authdomain.ErrRefreshReplay
	}
	v.RefreshJTI = nextJTI
	v.ExpiresAt = expiresAt
	s.sessions[id] = v
	return nil
}
func (s *MemorySessionStore) Revoke(ctx context.Context, id string) error {
	if err := s.checkContext(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessions[id]
	if !ok {
		return authdomain.ErrSessionNotFound
	}
	v.Revoked = true
	s.sessions[id] = v
	return nil
}
