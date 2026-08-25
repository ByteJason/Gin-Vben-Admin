package iam

import (
	"context"
	"errors"
	"time"

	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
)

// DecisionCacheGeneration identifies the immutable namespace observed during
// a cache lookup. An in-flight decision must be written back to that exact
// generation, never whatever generation happens to be current later.
type DecisionCacheGeneration int64

// DecisionCache stores authorization decisions under a versioned namespace.
// Implementations must invalidate the namespace atomically; callers never
// delete arbitrary keys or scan the whole Redis database.
type DecisionCache interface {
	Get(ctx context.Context, subject domain.Subject, request domain.Request) (allowed bool, found bool, generation DecisionCacheGeneration, err error)
	Set(ctx context.Context, subject domain.Subject, request domain.Request, generation DecisionCacheGeneration, allowed bool, ttl time.Duration) error
	Invalidate(ctx context.Context) error
}

type cachedAuthorizer struct {
	underlying domain.Authorizer
	cache      DecisionCache
	ttl        time.Duration
}

func NewCachedAuthorizer(underlying domain.Authorizer, cache DecisionCache, ttl time.Duration) domain.Authorizer {
	if underlying == nil || cache == nil {
		return underlying
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &cachedAuthorizer{underlying: underlying, cache: cache, ttl: ttl}
}

func (a *cachedAuthorizer) Authorize(ctx context.Context, subject domain.Subject, request domain.Request) (bool, error) {
	allowed, found, generation, err := a.cache.Get(ctx, subject, request)
	if err != nil {
		return false, err
	}
	if found {
		if !allowed {
			return false, domain.ErrAccessDenied
		}
		// The cache is deny-only. Ignore any legacy or externally injected allow
		// value and re-evaluate it against the authoritative policy store.
	}

	allowed, authErr := a.underlying.Authorize(ctx, subject, request)
	// Allow decisions are never cached. If a database mutation commits while
	// Redis invalidation is unavailable, there is therefore no durable old allow
	// left in the unchanged namespace for a later request to reuse.
	if allowed {
		return true, authErr
	}
	// Repository and context failures are not authorization decisions. Caching
	// them as false would turn a transient 5xx into a durable access denial and
	// discard the original cause on subsequent requests.
	if authErr != nil && !errors.Is(authErr, domain.ErrAccessDenied) {
		return false, authErr
	}
	// Deny decisions remain safe to cache. A cache write failure is fail-closed
	// so a dependency outage never silently broadens authorization.
	// Write only to the generation observed by Get. If a concurrent policy
	// mutation invalidated that namespace while the underlying decision was in
	// flight, this result remains unreachable from the new generation.
	if err := a.cache.Set(ctx, subject, request, generation, allowed, a.ttl); err != nil {
		return false, err
	}
	return allowed, authErr
}

func InvalidatePermissionCache(ctx context.Context, cache DecisionCache) error {
	if cache == nil {
		return nil
	}
	return cache.Invalidate(ctx)
}
