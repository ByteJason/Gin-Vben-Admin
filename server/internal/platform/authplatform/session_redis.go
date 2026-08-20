package authplatform

import (
	"context"
	"errors"
	"time"

	"example.com/gin-vben-admin/server/internal/domain/authdomain"
	rediscache "example.com/gin-vben-admin/server/internal/platform/cache/redis"
)

// RedisSessionStore persists refresh-session state using the existing namespaced
// cache client. Rotation is guarded by a short distributed lock per session.
type RedisSessionStore struct {
	cache   *rediscache.Client
	lockTTL time.Duration
}

func NewRedisSessionStore(cache *rediscache.Client) *RedisSessionStore {
	return &RedisSessionStore{cache: cache, lockTTL: 3 * time.Second}
}
func (s *RedisSessionStore) key(id string) (string, error) { return s.cache.Key("auth-session", id) }
func (s *RedisSessionStore) Create(ctx context.Context, session authdomain.Session) error {
	key, err := s.key(session.ID)
	if err != nil {
		return err
	}
	ttl := time.Until(session.ExpiresAt)
	if ttl <= 0 {
		return authdomain.ErrSessionRevoked
	}
	return s.cache.SetJSON(ctx, key, session, ttl)
}
func (s *RedisSessionStore) Get(ctx context.Context, id string) (authdomain.Session, error) {
	key, err := s.key(id)
	if err != nil {
		return authdomain.Session{}, err
	}
	var v authdomain.Session
	if err := s.cache.GetJSON(ctx, key, &v); err != nil {
		if errors.Is(err, rediscache.ErrCacheMiss) {
			return v, authdomain.ErrSessionNotFound
		}
		return v, err
	}
	return v, nil
}
func (s *RedisSessionStore) Rotate(ctx context.Context, id, expectedJTI, nextJTI string, expiresAt time.Time) error {
	lock, err := s.cache.AcquireLock(ctx, "auth-rotate-"+id, s.lockTTL)
	if err != nil {
		return err
	}
	defer lock.Release(ctx)
	v, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if v.Revoked {
		return authdomain.ErrSessionRevoked
	}
	if v.RefreshJTI != expectedJTI {
		return authdomain.ErrRefreshReplay
	}
	v.RefreshJTI = nextJTI
	v.ExpiresAt = expiresAt
	return s.Create(ctx, v)
}
func (s *RedisSessionStore) Revoke(ctx context.Context, id string) error {
	lock, err := s.cache.AcquireLock(ctx, "auth-revoke-"+id, s.lockTTL)
	if err != nil {
		return err
	}
	defer lock.Release(ctx)
	v, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	v.Revoked = true
	ttl := time.Until(v.ExpiresAt)
	if ttl <= 0 {
		return authdomain.ErrSessionRevoked
	}
	key, _ := s.key(id)
	return s.cache.SetJSON(ctx, key, v, ttl)
}
