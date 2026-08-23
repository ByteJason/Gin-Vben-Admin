package authplatform

import (
	"context"
	"os"
	"testing"
	"time"

	rediscache "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/cache/redis"
)

func TestRedisRateLimiterIntegration(t *testing.T) {
	if os.Getenv("REDIS_INTEGRATION") != "1" {
		t.Skip("set REDIS_INTEGRATION=1 to run against local Redis")
	}
	client, err := rediscache.New(rediscache.Config{Addr: "127.0.0.1:6379", Namespace: "app:v1"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	limiter := NewRedisRateLimiter(client)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	key := "integration-rate-" + time.Now().UTC().Format("20060102150405.000000000")
	// The implementation hashes the logical key; derive the physical key in
	// the same package so cleanup is exact and never scans the shared DB.
	physical, err := rateKey(client, key)
	if err != nil {
		t.Fatal(err)
	}
	_ = client.Delete(ctx, physical)
	t.Cleanup(func() { _ = client.Delete(context.Background(), physical) })
	if allowed, err := limiter.Allow(ctx, key, 2, time.Minute); err != nil || !allowed {
		t.Fatalf("first Allow() = %v, %v", allowed, err)
	}
	if allowed, err := limiter.Allow(ctx, key, 2, time.Minute); err != nil || !allowed {
		t.Fatalf("second Allow() = %v, %v", allowed, err)
	}
	if allowed, err := limiter.Allow(ctx, key, 2, time.Minute); err != nil || allowed {
		t.Fatalf("third Allow() = %v, %v, want rejection", allowed, err)
	}
}
