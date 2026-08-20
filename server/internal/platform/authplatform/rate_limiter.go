package authplatform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	rediscache "example.com/gin-vben-admin/server/internal/platform/cache/redis"
)

var ErrRateLimiterUnavailable = errors.New("redis rate limiter is not initialized")

// RedisRateLimiter provides a process-independent fixed-window counter. The
// caller supplies a logical key (for example account or client IP); it is
// hashed before entering Redis so identifiers cannot alter key structure.
type RedisRateLimiter struct {
	cache *rediscache.Client
}

func NewRedisRateLimiter(cache *rediscache.Client) *RedisRateLimiter {
	return &RedisRateLimiter{cache: cache}
}

func (r *RedisRateLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	if r == nil || r.cache == nil {
		return false, ErrRateLimiterUnavailable
	}
	if limit <= 0 || window <= 0 {
		return false, errors.New("invalid rate limiter policy")
	}
	physical, err := rateKey(r.cache, key)
	if err != nil {
		return false, err
	}
	count, err := r.cache.Increment(ctx, physical, window)
	if err != nil {
		return false, err
	}
	return count <= int64(limit), nil
}

func rateKey(cache *rediscache.Client, key string) (string, error) {
	digest := sha256.Sum256([]byte(key))
	return cache.Key("auth-rate", hex.EncodeToString(digest[:]))
}
