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
}

func (f *fakeDecisionCache) Get(context.Context, domain.Subject, domain.Request) (bool, bool, error) {
	f.gets++
	return f.allowed, f.found, nil
}
func (f *fakeDecisionCache) Set(_ context.Context, _ domain.Subject, _ domain.Request, allowed bool, _ time.Duration) error {
	f.sets++
	f.allowed = allowed
	f.found = true
	return nil
}
func (f *fakeDecisionCache) Invalidate(context.Context) error {
	f.invalidates++
	f.found = false
	return nil
}

type countingAuthorizer struct{ calls int }

func (a *countingAuthorizer) Authorize(context.Context, domain.Subject, domain.Request) (bool, error) {
	a.calls++
	return true, nil
}

func TestCachedAuthorizerCachesAndInvalidates(t *testing.T) {
	cache := &fakeDecisionCache{}
	underlying := &countingAuthorizer{}
	a := NewCachedAuthorizer(underlying, cache, time.Minute)
	subject := domain.Subject{UserID: "u1"}
	request := domain.Request{Method: "GET", Path: "/orders"}
	if ok, err := a.Authorize(context.Background(), subject, request); err != nil || !ok {
		t.Fatalf("first authorize=%v err=%v", ok, err)
	}
	if ok, err := a.Authorize(context.Background(), subject, request); err != nil || !ok {
		t.Fatalf("cached authorize=%v err=%v", ok, err)
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

var errCacheUnavailable = errors.New("cache unavailable")

type failingDecisionCache struct{}

func (failingDecisionCache) Get(context.Context, domain.Subject, domain.Request) (bool, bool, error) {
	return false, false, errCacheUnavailable
}
func (failingDecisionCache) Set(context.Context, domain.Subject, domain.Request, bool, time.Duration) error {
	return errCacheUnavailable
}
func (failingDecisionCache) Invalidate(context.Context) error { return errCacheUnavailable }
