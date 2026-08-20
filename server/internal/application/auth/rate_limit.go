package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidRateLimitKey    = errors.New("rate limit key is required")
	ErrInvalidRateLimitPolicy = errors.New("rate limit policy is invalid")
)

// RateLimiter is the application seam for account/IP attempt limits. A false
// result is a policy rejection, while a non-nil error is returned so callers
// can fail closed.
type RateLimiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}

type memoryRateBucket struct {
	started time.Time
	count   int
}

// MemoryRateLimiter is deterministic for tests and single-process local
// development. Production deployments should use a shared implementation.
type MemoryRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]memoryRateBucket
	now     func() time.Time
}

func NewMemoryRateLimiter() *MemoryRateLimiter {
	return &MemoryRateLimiter{buckets: make(map[string]memoryRateBucket), now: time.Now}
}

func (m *MemoryRateLimiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if m == nil {
		return false, errors.New("rate limiter is not initialized")
	}
	if strings.TrimSpace(key) == "" {
		return false, ErrInvalidRateLimitKey
	}
	if limit <= 0 || window <= 0 {
		return false, ErrInvalidRateLimitPolicy
	}
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	for bucketKey, bucket := range m.buckets {
		if !now.Before(bucket.started.Add(window)) {
			delete(m.buckets, bucketKey)
		}
	}
	bucket, ok := m.buckets[key]
	if !ok || !now.Before(bucket.started.Add(window)) {
		m.buckets[key] = memoryRateBucket{started: now, count: 1}
		return true, nil
	}
	if bucket.count >= limit {
		return false, nil
	}
	bucket.count++
	m.buckets[key] = bucket
	return true, nil
}
