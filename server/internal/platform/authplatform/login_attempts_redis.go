package authplatform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	appauth "example.com/gin-vben-admin/server/internal/application/auth"
	rediscache "example.com/gin-vben-admin/server/internal/platform/cache/redis"
)

var ErrLoginAttemptStoreUnavailable = errors.New("redis login attempt store is not initialized")

// RedisLoginAttemptStore keeps failed-login counters and lock markers in the
// configured Redis namespace. Counter increments are atomic and lock markers
// expire automatically, so multiple API processes share the same policy.
type RedisLoginAttemptStore struct {
	cache     *rediscache.Client
	threshold int
	duration  time.Duration
}

var _ appauth.LoginAttemptStore = (*RedisLoginAttemptStore)(nil)

func NewRedisLoginAttemptStore(cache *rediscache.Client, threshold int, duration time.Duration) *RedisLoginAttemptStore {
	return &RedisLoginAttemptStore{cache: cache, threshold: threshold, duration: duration}
}

func (s *RedisLoginAttemptStore) IsLocked(ctx context.Context, identifier string) (bool, error) {
	if err := s.validate(ctx, identifier); err != nil {
		return false, err
	}
	key, err := s.key("auth-lock", identifier)
	if err != nil {
		return false, err
	}
	var marker bool
	if err := s.cache.GetJSON(ctx, key, &marker); err != nil {
		if errors.Is(err, rediscache.ErrCacheMiss) {
			return false, nil
		}
		return false, err
	}
	return marker, nil
}

func (s *RedisLoginAttemptStore) RecordFailure(ctx context.Context, identifier string) (bool, error) {
	if err := s.validate(ctx, identifier); err != nil {
		return false, err
	}
	locked, err := s.IsLocked(ctx, identifier)
	if err != nil || locked {
		return locked, err
	}
	counter, err := s.key("auth-fail", identifier)
	if err != nil {
		return false, err
	}
	count, err := s.cache.Increment(ctx, counter, s.duration)
	if err != nil {
		return false, err
	}
	if count < int64(s.threshold) {
		return false, nil
	}
	marker, err := s.key("auth-lock", identifier)
	if err != nil {
		return false, err
	}
	if err := s.cache.SetJSON(ctx, marker, true, s.duration); err != nil {
		return false, err
	}
	return true, nil
}

func (s *RedisLoginAttemptStore) Reset(ctx context.Context, identifier string) error {
	if err := s.validate(ctx, identifier); err != nil {
		return err
	}
	for _, prefix := range []string{"auth-fail", "auth-lock"} {
		key, err := s.key(prefix, identifier)
		if err != nil {
			return err
		}
		if err := s.cache.Delete(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

func (s *RedisLoginAttemptStore) key(prefix, identifier string) (string, error) {
	digest := sha256.Sum256([]byte(strings.TrimSpace(identifier)))
	return s.cache.Key(prefix, hex.EncodeToString(digest[:]))
}

func (s *RedisLoginAttemptStore) validate(ctx context.Context, identifier string) error {
	if s == nil || s.cache == nil {
		return ErrLoginAttemptStoreUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(identifier) == "" {
		return appauth.ErrInvalidLoginAttemptKey
	}
	if s.threshold <= 0 || s.duration <= 0 {
		return appauth.ErrInvalidLoginAttemptPolicy
	}
	return nil
}
