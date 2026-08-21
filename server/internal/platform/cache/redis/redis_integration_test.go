package rediscache

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestRedisJSONAndLockIntegration(t *testing.T) {
	if os.Getenv("REDIS_INTEGRATION") != "1" {
		t.Skip("set REDIS_INTEGRATION=1 to run against local Redis")
	}

	client, err := New(Config{Addr: "127.0.0.1:6379", Namespace: "app:v1"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}

	key, err := client.Key("test", "redis-integration-json")
	if err != nil {
		t.Fatalf("Key() error = %v", err)
	}
	if err := client.Delete(ctx, key); err != nil {
		t.Fatalf("initial Delete() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Delete(context.Background(), key) })

	var missing map[string]string
	if err := client.GetJSON(ctx, key, &missing); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("GetJSON() missing error = %v, want ErrCacheMiss", err)
	}

	want := struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}{ID: 42, Name: "cache-integration"}
	if err := client.SetJSON(ctx, key, want, time.Minute); err != nil {
		t.Fatalf("SetJSON() error = %v", err)
	}
	var got struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := client.GetJSON(ctx, key, &got); err != nil {
		t.Fatalf("GetJSON() error = %v", err)
	}
	if got != want {
		t.Fatalf("GetJSON() = %#v, want %#v", got, want)
	}
	var consumed struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := client.TakeJSON(ctx, key, &consumed); err != nil {
		t.Fatalf("TakeJSON() error = %v", err)
	}
	if consumed != want {
		t.Fatalf("TakeJSON() = %#v, want %#v", consumed, want)
	}
	if err := client.TakeJSON(ctx, key, &consumed); !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("TakeJSON(replay) error = %v, want ErrCacheMiss", err)
	}
	if err := client.SetJSON(ctx, key, want, time.Minute); err != nil {
		t.Fatalf("SetJSON(recreate) error = %v", err)
	}

	lock, err := client.AcquireLock(ctx, "redis-integration-lock", time.Minute)
	if err != nil {
		t.Fatalf("AcquireLock() error = %v", err)
	}
	t.Cleanup(func() { _ = lock.Release(context.Background()) })

	if _, err := client.AcquireLock(ctx, "redis-integration-lock", time.Minute); !errors.Is(err, ErrLockNotAcquired) {
		t.Fatalf("second AcquireLock() error = %v, want ErrLockNotAcquired", err)
	}
	foreign := &Lock{client: client, key: lock.key, owner: "different-owner"}
	if err := foreign.Release(ctx); !errors.Is(err, ErrLockNotAcquired) {
		t.Fatalf("foreign Release() error = %v, want ErrLockNotAcquired", err)
	}
	if _, err := client.AcquireLock(ctx, "redis-integration-lock", time.Minute); !errors.Is(err, ErrLockNotAcquired) {
		t.Fatalf("AcquireLock() after foreign release error = %v, want ErrLockNotAcquired", err)
	}
	if err := lock.Release(ctx); err != nil {
		t.Fatalf("owner Release() error = %v", err)
	}
	if next, err := client.AcquireLock(ctx, "redis-integration-lock", time.Minute); err != nil {
		t.Fatalf("AcquireLock() after owner release error = %v", err)
	} else {
		t.Cleanup(func() { _ = next.Release(context.Background()) })
	}
}
