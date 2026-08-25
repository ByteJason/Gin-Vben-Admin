package rediscache

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisRuntimeStatsPreservesPoolCapacityActivityAndZeroKeyspace(t *testing.T) {
	max := 13
	got := redisRuntimeStats(&redis.PoolStats{
		Hits: 1, Misses: 2, Timeouts: 3, WaitCount: 4, WaitDurationNs: int64(5 * time.Millisecond),
		TotalConns: 7, IdleConns: 3, StaleConns: 6, PendingRequests: 8,
	}, &max, ModeSingle, 0, true)
	if !got.ModeAvailable || got.Mode != ModeSingle {
		t.Fatalf("redis mode = %#v", got)
	}
	if !got.PoolAvailable || got.Pool.Max == nil || *got.Pool.Max != 13 || got.Pool.Total != 7 || got.Pool.Active != 4 || got.Pool.Idle != 3 {
		t.Fatalf("pool capacity/activity = %#v", got)
	}
	if got.Pool.WaitDurationMS != 5 || got.Pool.WaitCount != 4 || got.Pool.Stale != 6 || got.Pool.Pending != 8 {
		t.Fatalf("pool counters = %#v", got)
	}
	if !got.KeyspaceAvailable || got.Keyspace != 0 {
		t.Fatalf("zero keyspace = %#v", got)
	}
}

func TestNewBuildsSingleClientWithoutProbingNetwork(t *testing.T) {
	client, err := New(Config{Addr: "127.0.0.1:6399"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	if got := client.Name(); got != "redis" {
		t.Fatalf("Name() = %q, want redis", got)
	}
	key, err := client.Key("session", "42")
	if err != nil {
		t.Fatalf("Key() error = %v", err)
	}
	if key != "app:v1:session:42" {
		t.Fatalf("Key() = %q, want app:v1:session:42", key)
	}
}

func TestKeyUsesConfiguredNamespaceAndRejectsUnsafeSegments(t *testing.T) {
	client, err := New(Config{Addr: "127.0.0.1:6399", Namespace: "admin:v2"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	key, err := client.Key("profile", "42")
	if err != nil {
		t.Fatalf("Key() error = %v", err)
	}
	if key != "admin:v2:profile:42" {
		t.Fatalf("Key() = %q, want admin:v2:profile:42", key)
	}

	for _, parts := range [][]string{{}, {""}, {"profile:42"}, {" profile "}} {
		_, err := client.Key(parts...)
		if !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("Key(%q) error = %v, want ErrInvalidKey", parts, err)
		}
	}
}

func TestOperationsRejectKeysOutsideNamespaceBeforeNetworkIO(t *testing.T) {
	client, err := New(Config{Addr: "127.0.0.1:6399", Namespace: "admin:v1"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	for _, operation := range []struct {
		name string
		run  func() error
	}{
		{"set", func() error {
			return client.SetJSON(ctx, "foreign:key", struct{}{}, time.Second)
		}},
		{"get", func() error { return client.GetJSON(ctx, "foreign:key", &map[string]string{}) }},
		{"take", func() error { return client.TakeJSON(ctx, "foreign:key", &map[string]string{}) }},
		{"delete", func() error { return client.Delete(ctx, "foreign:key") }},
	} {
		t.Run(operation.name, func(t *testing.T) {
			if err := operation.run(); !errors.Is(err, ErrInvalidKey) {
				t.Fatalf("error = %v, want ErrInvalidKey", err)
			}
		})
	}
}

func TestTTLAndLockNameValidationHappenBeforeNetworkIO(t *testing.T) {
	client, err := New(Config{Addr: "127.0.0.1:6399"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	key, err := client.Key("token", "42")
	if err != nil {
		t.Fatalf("Key() error = %v", err)
	}
	if err := client.SetJSON(context.Background(), key, struct{}{}, 0); !errors.Is(err, ErrInvalidTTL) {
		t.Fatalf("SetJSON() error = %v, want ErrInvalidTTL", err)
	}
	if _, err := client.AcquireLock(context.Background(), "job:42", time.Second); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("AcquireLock() error = %v, want ErrInvalidKey", err)
	}
	if _, err := client.AcquireLock(context.Background(), "job", 0); !errors.Is(err, ErrInvalidTTL) {
		t.Fatalf("AcquireLock() error = %v, want ErrInvalidTTL", err)
	}
	if _, err := client.Increment(context.Background(), "foreign:key", time.Minute); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("Increment() error = %v, want ErrInvalidKey", err)
	}
	validRateKey, err := client.Key("rate")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Increment(context.Background(), validRateKey, 0); !errors.Is(err, ErrInvalidTTL) {
		t.Fatalf("Increment() error = %v, want ErrInvalidTTL", err)
	}
}

func TestTopologyValidationDoesNotProbeNetwork(t *testing.T) {
	valid := []Config{
		{Mode: ModeSentinel, Addrs: []string{"127.0.0.1:26379"}, MasterName: "mymaster"},
		{Mode: ModeCluster, Addrs: []string{"127.0.0.1:7000", "127.0.0.1:7001"}},
	}
	for _, config := range valid {
		t.Run(config.Mode, func(t *testing.T) {
			client, err := New(config)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			defer client.Close()
		})
	}

	invalid := []Config{
		{Mode: "unknown"},
		{Mode: ModeSentinel, MasterName: "mymaster"},
		{Mode: ModeSentinel, Addrs: []string{"127.0.0.1:26379"}},
		{Mode: ModeCluster, Addrs: []string{"127.0.0.1:7000"}},
	}
	for _, config := range invalid {
		t.Run("invalid_"+config.Mode, func(t *testing.T) {
			if _, err := New(config); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
			}
		})
	}
}

func TestAddressMapRewritesAdvertisedTopologyEndpoints(t *testing.T) {
	client, err := New(Config{
		Mode:       ModeCluster,
		Addrs:      []string{"127.0.0.1:16379", "127.0.0.1:16380"},
		AddressMap: map[string]string{"172.20.0.7:6379": "127.0.0.1:16379"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()
	if got := client.mapAddress("172.20.0.7:6379"); got != "127.0.0.1:16379" {
		t.Fatalf("mapAddress() = %q, want mapped endpoint", got)
	}
	if got := client.mapAddress("127.0.0.1:16380"); got != "127.0.0.1:16380" {
		t.Fatalf("mapAddress() unchanged = %q", got)
	}
}
