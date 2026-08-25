package iam

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
)

type fakeDecisionCache struct {
	found       bool
	allowed     bool
	gets        int
	sets        int
	invalidates int
	invalidate  error
}

func (f *fakeDecisionCache) Get(context.Context, domain.Subject, domain.Request) (bool, bool, DecisionCacheGeneration, error) {
	f.gets++
	return f.allowed, f.found, 0, nil
}
func (f *fakeDecisionCache) Set(_ context.Context, _ domain.Subject, _ domain.Request, _ DecisionCacheGeneration, allowed bool, _ time.Duration) error {
	f.sets++
	f.allowed = allowed
	f.found = true
	return nil
}
func (f *fakeDecisionCache) Invalidate(context.Context) error {
	f.invalidates++
	if f.invalidate != nil {
		return f.invalidate
	}
	f.found = false
	return nil
}

type countingAuthorizer struct{ calls int }

func (a *countingAuthorizer) Authorize(context.Context, domain.Subject, domain.Request) (bool, error) {
	a.calls++
	return true, nil
}

type mutableAuthorizer struct {
	allowed bool
	calls   int
}

type recoveringAuthorizer struct{ calls int }

func (a *recoveringAuthorizer) Authorize(context.Context, domain.Subject, domain.Request) (bool, error) {
	a.calls++
	if a.calls == 1 {
		return false, errAuthorizationUnavailable
	}
	return true, nil
}

func (a *mutableAuthorizer) Authorize(context.Context, domain.Subject, domain.Request) (bool, error) {
	a.calls++
	if a.allowed {
		return true, nil
	}
	return false, domain.ErrAccessDenied
}

type rotatingDecisionCache struct {
	generation DecisionCacheGeneration
	entries    map[DecisionCacheGeneration]bool
}

func (c *rotatingDecisionCache) Get(context.Context, domain.Subject, domain.Request) (bool, bool, DecisionCacheGeneration, error) {
	allowed, found := c.entries[c.generation]
	return allowed, found, c.generation, nil
}

func (c *rotatingDecisionCache) Set(_ context.Context, _ domain.Subject, _ domain.Request, generation DecisionCacheGeneration, allowed bool, _ time.Duration) error {
	c.entries[generation] = allowed
	return nil
}

func (c *rotatingDecisionCache) Invalidate(context.Context) error {
	c.generation++
	return nil
}

type grantingAuthorizer struct {
	cache *rotatingDecisionCache
	calls int
}

func (a *grantingAuthorizer) Authorize(ctx context.Context, _ domain.Subject, _ domain.Request) (bool, error) {
	a.calls++
	if a.calls == 1 {
		// Simulate a policy grant committing and invalidating the cache after
		// this request loaded the old deny snapshot but before it caches it.
		if err := a.cache.Invalidate(ctx); err != nil {
			return false, err
		}
		return false, domain.ErrAccessDenied
	}
	return true, nil
}

func TestCachedAuthorizerCachesDenyAndInvalidates(t *testing.T) {
	cache := &fakeDecisionCache{}
	underlying := &mutableAuthorizer{}
	a := NewCachedAuthorizer(underlying, cache, time.Minute)
	subject := domain.Subject{UserID: "u1"}
	request := domain.Request{Method: "GET", Path: "/orders"}
	if ok, err := a.Authorize(context.Background(), subject, request); ok || !errors.Is(err, domain.ErrAccessDenied) {
		t.Fatalf("first authorize=%v err=%v, want access denied", ok, err)
	}
	if ok, err := a.Authorize(context.Background(), subject, request); ok || !errors.Is(err, domain.ErrAccessDenied) {
		t.Fatalf("cached authorize=%v err=%v, want access denied", ok, err)
	}
	if underlying.calls != 1 || cache.sets != 1 {
		t.Fatalf("calls=%d sets=%d", underlying.calls, cache.sets)
	}
	if err := InvalidatePermissionCache(context.Background(), cache); err != nil {
		t.Fatal(err)
	}
	if cache.invalidates != 1 {
		t.Fatalf("invalidations=%d", cache.invalidates)
	}
}

func TestCachedAuthorizerFailsClosedOnCacheError(t *testing.T) {
	cache := failingDecisionCache{}
	a := NewCachedAuthorizer(&countingAuthorizer{}, cache, time.Minute)
	if _, err := a.Authorize(context.Background(), domain.Subject{UserID: "u1"}, domain.Request{Method: "GET", Path: "/"}); !errors.Is(err, errCacheUnavailable) {
		t.Fatalf("cache error=%v", err)
	}
}

func TestCachedAuthorizerDoesNotPublishOldDenyIntoInvalidatedGeneration(t *testing.T) {
	cache := &rotatingDecisionCache{entries: map[DecisionCacheGeneration]bool{}}
	underlying := &grantingAuthorizer{cache: cache}
	authorizer := NewCachedAuthorizer(underlying, cache, time.Minute)
	subject := domain.Subject{UserID: "u1", RoleIDs: []string{"reader"}, Domain: "tenant-a"}
	request := domain.Request{Domain: "tenant-a", Method: "GET", Path: "/reports"}

	if allowed, err := authorizer.Authorize(context.Background(), subject, request); allowed || !errors.Is(err, domain.ErrAccessDenied) {
		t.Fatalf("in-flight old decision allowed=%v err=%v, want access denied", allowed, err)
	}
	if allowed, err := authorizer.Authorize(context.Background(), subject, request); err != nil || !allowed {
		t.Fatalf("post-grant decision allowed=%v err=%v", allowed, err)
	}
	if underlying.calls != 2 {
		t.Fatalf("underlying authorization calls=%d, want 2", underlying.calls)
	}
}

func TestCachedAuthorizerDoesNotRetainAllowWhenRevocationInvalidationFails(t *testing.T) {
	cache := &fakeDecisionCache{invalidate: errCacheUnavailable}
	underlying := &mutableAuthorizer{allowed: true}
	authorizer := NewCachedAuthorizer(underlying, cache, time.Minute)
	subject := domain.Subject{UserID: "u1", RoleIDs: []string{"reader"}, Domain: "tenant-a"}
	request := domain.Request{Domain: "tenant-a", Method: "GET", Path: "/reports"}

	if allowed, err := authorizer.Authorize(context.Background(), subject, request); err != nil || !allowed {
		t.Fatalf("initial decision allowed=%v err=%v", allowed, err)
	}
	// The policy mutation has committed, but Redis is unavailable and the
	// version invalidation cannot advance. A previously cached allow must not
	// survive this failure and authorize a later request.
	underlying.allowed = false
	if err := InvalidatePermissionCache(context.Background(), cache); !errors.Is(err, errCacheUnavailable) {
		t.Fatalf("InvalidatePermissionCache() error=%v", err)
	}
	if allowed, err := authorizer.Authorize(context.Background(), subject, request); allowed || !errors.Is(err, domain.ErrAccessDenied) {
		t.Fatalf("post-revoke decision allowed=%v err=%v, want access denied", allowed, err)
	}
	if underlying.calls != 2 {
		t.Fatalf("underlying authorization calls=%d, want 2", underlying.calls)
	}
}

func TestCachedAuthorizerDoesNotTurnTransientFailureIntoCachedDeny(t *testing.T) {
	cache := &fakeDecisionCache{}
	underlying := &recoveringAuthorizer{}
	authorizer := NewCachedAuthorizer(underlying, cache, time.Minute)
	subject := domain.Subject{UserID: "u1", Domain: "tenant-a"}
	request := domain.Request{Domain: "tenant-a", Method: "GET", Path: "/reports"}

	if allowed, err := authorizer.Authorize(context.Background(), subject, request); allowed || !errors.Is(err, errAuthorizationUnavailable) {
		t.Fatalf("transient decision allowed=%v err=%v", allowed, err)
	}
	if allowed, err := authorizer.Authorize(context.Background(), subject, request); err != nil || !allowed {
		t.Fatalf("recovered decision allowed=%v err=%v", allowed, err)
	}
	if underlying.calls != 2 || cache.sets != 0 {
		t.Fatalf("underlying calls=%d cache sets=%d, want 2 and 0", underlying.calls, cache.sets)
	}
}

func TestCachedAuthorizerDoesNotTrustStoredAllow(t *testing.T) {
	cache := &fakeDecisionCache{found: true, allowed: true}
	underlying := &mutableAuthorizer{}
	authorizer := NewCachedAuthorizer(underlying, cache, time.Minute)

	allowed, err := authorizer.Authorize(
		context.Background(),
		domain.Subject{UserID: "u1", Domain: "tenant-a"},
		domain.Request{Domain: "tenant-a", Method: "GET", Path: "/reports"},
	)
	if allowed || !errors.Is(err, domain.ErrAccessDenied) {
		t.Fatalf("decision allowed=%v err=%v, want fresh access denial", allowed, err)
	}
	if underlying.calls != 1 {
		t.Fatalf("underlying authorization calls=%d, want 1", underlying.calls)
	}
}

var errCacheUnavailable = errors.New("cache unavailable")
var errAuthorizationUnavailable = errors.New("authorization repository unavailable")

type failingDecisionCache struct{}

func (failingDecisionCache) Get(context.Context, domain.Subject, domain.Request) (bool, bool, DecisionCacheGeneration, error) {
	return false, false, 0, errCacheUnavailable
}
func (failingDecisionCache) Set(context.Context, domain.Subject, domain.Request, DecisionCacheGeneration, bool, time.Duration) error {
	return errCacheUnavailable
}
func (failingDecisionCache) Invalidate(context.Context) error { return errCacheUnavailable }
