package auth

import (
	"context"
	"testing"
	"time"
)

func TestMemoryRateLimiterAllowsThenRejectsWithinWindow(t *testing.T) {
	limiter := NewMemoryRateLimiter()
	ctx := context.Background()
	if allowed, err := limiter.Allow(ctx, "login:alice", 2, time.Minute); err != nil || !allowed {
		t.Fatalf("first Allow() = %v, %v", allowed, err)
	}
	if allowed, err := limiter.Allow(ctx, "login:alice", 2, time.Minute); err != nil || !allowed {
		t.Fatalf("second Allow() = %v, %v", allowed, err)
	}
	if allowed, err := limiter.Allow(ctx, "login:alice", 2, time.Minute); err != nil || allowed {
		t.Fatalf("third Allow() = %v, %v, want rejection", allowed, err)
	}
}

func TestMemoryRateLimiterWindowExpires(t *testing.T) {
	limiter := NewMemoryRateLimiter()
	ctx := context.Background()
	if allowed, err := limiter.Allow(ctx, "login:alice", 1, 10*time.Millisecond); err != nil || !allowed {
		t.Fatalf("first Allow() = %v, %v", allowed, err)
	}
	time.Sleep(20 * time.Millisecond)
	if allowed, err := limiter.Allow(ctx, "login:alice", 1, 10*time.Millisecond); err != nil || !allowed {
		t.Fatalf("expired Allow() = %v, %v, want allowed", allowed, err)
	}
}

func TestMemoryRateLimiterRejectsInvalidInputsAndCanceledContext(t *testing.T) {
	limiter := NewMemoryRateLimiter()
	for _, tc := range []struct {
		name  string
		key   string
		limit int
		win   time.Duration
	}{
		{"empty key", "", 1, time.Minute},
		{"zero limit", "a", 0, time.Minute},
		{"zero window", "a", 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := limiter.Allow(context.Background(), tc.key, tc.limit, tc.win); err == nil {
				t.Fatal("Allow() error = nil")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := limiter.Allow(ctx, "a", 1, time.Minute); err == nil {
		t.Fatal("canceled Allow() error = nil")
	}
}
