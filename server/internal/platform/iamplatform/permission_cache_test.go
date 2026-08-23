package iamplatform

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
	rediscache "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/cache/redis"
)

func TestDecisionDigestIsIndependentOfRoleOrder(t *testing.T) {
	request := domain.Request{Domain: "admin", Method: "GET", Path: "/users"}
	left := decisionDigest(domain.Subject{UserID: "7", RoleIDs: []string{"role-b", "role-a"}}, request)
	right := decisionDigest(domain.Subject{UserID: "7", RoleIDs: []string{"role-a", "role-b"}}, request)
	if left == "" || left != right {
		t.Fatalf("digest mismatch: left=%q right=%q", left, right)
	}
}

func TestRedisPermissionCacheRequiresClient(t *testing.T) {
	cache := NewRedisPermissionCache(nil)
	subject := domain.Subject{UserID: "7"}
	request := domain.Request{Method: "GET", Path: "/users"}
	if _, _, err := cache.Get(context.Background(), subject, request); !errors.Is(err, ErrPermissionCacheUnavailable) {
		t.Fatalf("Get() error=%v", err)
	}
	if err := cache.Set(context.Background(), subject, request, true, time.Minute); !errors.Is(err, ErrPermissionCacheUnavailable) {
		t.Fatalf("Set() error=%v", err)
	}
	if err := cache.Invalidate(context.Background()); !errors.Is(err, ErrPermissionCacheUnavailable) {
		t.Fatalf("Invalidate() error=%v", err)
	}
}

func TestRedisPermissionCacheIntegration(t *testing.T) {
	if os.Getenv("REDIS_INTEGRATION") != "1" {
		t.Skip("set REDIS_INTEGRATION=1 to run against local Redis")
	}
	client, err := rediscache.New(rediscache.Config{Addr: "127.0.0.1:6379", Namespace: "app:v1:test:iamcache"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx); err != nil {
		_ = client.Close()
		t.Fatal(err)
	}
	cache := NewRedisPermissionCache(client, time.Minute)
	subject := domain.Subject{UserID: "7", RoleIDs: []string{"role-a"}, Domain: "admin"}
	request := domain.Request{Domain: "admin", Method: "GET", Path: "/users"}
	versionKey, err := client.Key("iam", "permission", "version")
	if err != nil {
		_ = client.Close()
		t.Fatal(err)
	}
	oldKey, err := cache.decisionKey(0, subject, request)
	if err != nil {
		_ = client.Close()
		t.Fatal(err)
	}
	newKey, err := cache.decisionKey(1, subject, request)
	if err != nil {
		_ = client.Close()
		t.Fatal(err)
	}
	cleanup := func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = client.Delete(cleanupCtx, versionKey)
		_ = client.Delete(cleanupCtx, oldKey)
		_ = client.Delete(cleanupCtx, newKey)
		_ = client.Close()
	}
	defer cleanup()
	_ = client.Delete(ctx, versionKey)
	_ = client.Delete(ctx, oldKey)
	_ = client.Delete(ctx, newKey)

	if allowed, found, err := cache.Get(ctx, subject, request); err != nil || found || allowed {
		t.Fatalf("initial Get() allowed=%v found=%v err=%v", allowed, found, err)
	}
	if err := cache.Set(ctx, subject, request, true, time.Minute); err != nil {
		t.Fatal(err)
	}
	if allowed, found, err := cache.Get(ctx, subject, request); err != nil || !found || !allowed {
		t.Fatalf("cached Get() allowed=%v found=%v err=%v", allowed, found, err)
	}
	if err := cache.Invalidate(ctx); err != nil {
		t.Fatal(err)
	}
	if allowed, found, err := cache.Get(ctx, subject, request); err != nil || found || allowed {
		t.Fatalf("invalidated Get() allowed=%v found=%v err=%v", allowed, found, err)
	}
}
