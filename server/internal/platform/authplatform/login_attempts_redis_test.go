package authplatform

import (
	"context"
	"os"
	"testing"
	"time"

	rediscache "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/cache/redis"
)

func TestRedisLoginAttemptStoreIntegration(t *testing.T) {
	if os.Getenv("REDIS_INTEGRATION") != "1" {
		t.Skip("set REDIS_INTEGRATION=1 to run against the local Redis fixture")
	}
	cache, err := rediscache.New(rediscache.Config{Mode: rediscache.ModeSingle, Addr: "127.0.0.1:6379", Namespace: "app:test-lockout"})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	ctx := context.Background()
	store := NewRedisLoginAttemptStore(cache, 2, time.Minute)
	identifier := "lockout-integration-unique"
	defer store.Reset(ctx, identifier)
	if locked, err := store.RecordFailure(ctx, identifier); err != nil || locked {
		t.Fatalf("first failure locked=%v err=%v", locked, err)
	}
	if locked, err := store.RecordFailure(ctx, identifier); err != nil || !locked {
		t.Fatalf("threshold failure locked=%v err=%v", locked, err)
	}
	if locked, err := store.IsLocked(ctx, identifier); err != nil || !locked {
		t.Fatalf("locked state locked=%v err=%v", locked, err)
	}
	if err := store.Reset(ctx, identifier); err != nil {
		t.Fatal(err)
	}
	if locked, err := store.IsLocked(ctx, identifier); err != nil || locked {
		t.Fatalf("reset state locked=%v err=%v", locked, err)
	}
}
