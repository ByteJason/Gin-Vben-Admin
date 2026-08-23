package authplatform

import (
	"errors"
	"sync"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
)

var (
	ErrJWTKeyID    = errors.New("jwt key id must not be empty")
	ErrJWTKey      = errors.New("jwt signing key must not be empty")
	ErrJWTGrace    = errors.New("jwt rotation grace period must not be negative")
	ErrJWTKeyReuse = errors.New("jwt key id is already active")
)

type retiredJWTKey struct {
	service   *JWTService
	retiredAt time.Time
}

// RotatingJWTService keeps one active signing key and accepts retired keys only
// for the configured grace window. Existing JWTService instances remain
// immutable, so readers can parse tokens while a rotation is in progress.
type RotatingJWTService struct {
	mu sync.RWMutex

	activeID string
	active   *JWTService
	previous []retiredJWTKey
	grace    time.Duration
}

func NewRotatingJWTService(keyID string, secret []byte, accessTTL, refreshTTL time.Duration, issuer, audience string, grace time.Duration) (*RotatingJWTService, error) {
	if keyID == "" {
		return nil, ErrJWTKeyID
	}
	if len(secret) == 0 {
		return nil, ErrJWTKey
	}
	if grace < 0 {
		return nil, ErrJWTGrace
	}
	return &RotatingJWTService{
		activeID: keyID,
		active:   NewJWTServiceWithOptions(secret, accessTTL, refreshTTL, issuer, audience),
		grace:    grace,
	}, nil
}

func (s *RotatingJWTService) Issue(userID, sessionID string) (authdomain.TokenPair, error) {
	if s == nil {
		return authdomain.TokenPair{}, ErrJWTSecret
	}
	s.mu.RLock()
	active := s.active
	s.mu.RUnlock()
	return active.Issue(userID, sessionID)
}

func (s *RotatingJWTService) Parse(token string) (authdomain.Claims, error) {
	if s == nil {
		return authdomain.Claims{}, authdomain.ErrInvalidToken
	}
	now := time.Now()
	s.mu.RLock()
	active := s.active
	previous := append([]retiredJWTKey(nil), s.previous...)
	grace := s.grace
	s.mu.RUnlock()

	if claims, err := active.Parse(token); err == nil {
		return claims, nil
	}
	for _, key := range previous {
		if now.After(key.retiredAt.Add(grace)) {
			continue
		}
		if claims, err := key.service.Parse(token); err == nil {
			return claims, nil
		}
	}
	return authdomain.Claims{}, authdomain.ErrInvalidToken
}

func (s *RotatingJWTService) Rotate(keyID string, secret []byte) error {
	if s == nil {
		return ErrJWTKeyID
	}
	if keyID == "" {
		return ErrJWTKeyID
	}
	if len(secret) == 0 {
		return ErrJWTKey
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if keyID == s.activeID {
		return ErrJWTKeyReuse
	}
	if s.active == nil {
		return ErrJWTSecret
	}
	s.previous = append([]retiredJWTKey{{service: s.active, retiredAt: time.Now()}}, s.previous...)
	// JWTService carries the issuer/audience and TTL policy. A new service is
	// built with those values rather than sharing mutable signing state.
	s.active = NewJWTServiceWithOptions(secret, s.active.accessTTL, s.active.refreshTTL, s.active.Issuer, s.active.Audience)
	s.activeID = keyID
	s.pruneLocked(time.Now())
	return nil
}

func (s *RotatingJWTService) pruneLocked(now time.Time) {
	cutoff := now.Add(-s.grace)
	keep := s.previous[:0]
	for _, key := range s.previous {
		if !key.retiredAt.Before(cutoff) {
			keep = append(keep, key)
		}
	}
	s.previous = keep
}

var _ authdomain.TokenService = (*RotatingJWTService)(nil)
