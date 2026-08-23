package iam

import (
	"context"
	"time"

	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
)

// DecisionCache stores authorization decisions under a versioned namespace.
// Implementations must invalidate the namespace atomically; callers never
// delete arbitrary keys or scan the whole Redis database.
type DecisionCache interface {
	Get(ctx context.Context, subject domain.Subject, request domain.Request) (allowed bool, found bool, err error)
	Set(ctx context.Context, subject domain.Subject, request domain.Request, allowed bool, ttl time.Duration) error
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
	allowed, found, err := a.cache.Get(ctx, subject, request)
	if err != nil {
		return false, err
	}
	if found {
		if !allowed {
			return false, domain.ErrAccessDenied
		}
		return true, nil
	}

	allowed, authErr := a.underlying.Authorize(ctx, subject, request)
	// Cache both allow and deny decisions. A cache write failure is fail-closed
	// so a dependency outage never silently broadens authorization.
	if err := a.cache.Set(ctx, subject, request, allowed, a.ttl); err != nil {
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
