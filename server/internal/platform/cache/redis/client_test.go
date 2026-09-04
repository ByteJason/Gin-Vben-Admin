package rediscache

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/redis/go-redis/v9"
)

// revisionRedisFixture implements only the commands exercised by
// InvalidateModule/ModuleRevision. Embedding UniversalClient keeps this test
// independent of a running Redis daemon while the overridden methods still
// model the atomic compare-and-set script's observable behavior.
type revisionRedisFixture struct {
	redis.UniversalClient
	values map[string]string
}

func newRevisionRedisFixture() *revisionRedisFixture {
	return &revisionRedisFixture{values: make(map[string]string)}
}

func (f *revisionRedisFixture) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	deleted := int64(0)
	for _, key := range keys {
		if _, ok := f.values[key]; ok {
			delete(f.values, key)
			deleted++
		}
	}
	argv := make([]interface{}, 1, len(keys)+1)
	argv[0] = "del"
	for _, key := range keys {
		argv = append(argv, key)
	}
	cmd := redis.NewIntCmd(ctx, argv...)
	cmd.SetVal(deleted)
	return cmd
}

func (f *revisionRedisFixture) EvalSha(ctx context.Context, _ string, keys []string, args ...interface{}) *redis.Cmd {
	return f.evalRevision(ctx, keys, args...)
}

func (f *revisionRedisFixture) Eval(ctx context.Context, _ string, keys []string, args ...interface{}) *redis.Cmd {
	return f.evalRevision(ctx, keys, args...)
}

func (f *revisionRedisFixture) evalRevision(ctx context.Context, keys []string, args ...interface{}) *redis.Cmd {
	cmd := redis.NewCmd(ctx, "evalsha", keys)
	if len(keys) == 0 || len(args) == 0 {
		cmd.SetErr(errors.New("invalid revision script arguments"))
		return cmd
	}
	incoming, err := strconv.ParseInt(args[0].(string), 10, 64)
	if err != nil {
		cmd.SetErr(err)
		return cmd
	}
	current, _ := strconv.ParseInt(f.values[keys[0]], 10, 64)
	if _, exists := f.values[keys[0]]; !exists || incoming > current {
		f.values[keys[0]] = strconv.FormatInt(incoming, 10)
		cmd.SetVal(int64(1))
		return cmd
	}
	cmd.SetVal(int64(0))
	return cmd
}

func (f *revisionRedisFixture) Get(ctx context.Context, key string) *redis.StringCmd {
	cmd := redis.NewStringCmd(ctx, "get", key)
	value, ok := f.values[key]
	if !ok {
		cmd.SetErr(redis.Nil)
		return cmd
	}
	cmd.SetVal(value)
	return cmd
}

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

func TestLegacyMailSettingPatternsDoNotMatchIndependentColonNamespaces(t *testing.T) {
	patterns := legacyMailSettingPatterns("app:v1")
	for _, pattern := range patterns {
		if pattern == "app:v1:settings:mail*" || pattern == "app:v1:config:smtp*" {
			t.Fatalf("legacy pattern is too broad: %q", pattern)
		}
	}
	want := map[string]bool{
		"app:v1:settings:mail":         true,
		"app:v1:settings:mail.host":    true,
		"app:v1:settings:module:mail":  true,
		"app:v1:settings:value:mail":   true,
		"app:v1:settings:value:mail.x": true,
	}
	for candidate := range want {
		matched := false
		for _, pattern := range patterns {
			// The test only checks shape; Redis glob semantics are represented by
			// the explicit dot suffixes in the generated patterns.
			if pattern == candidate || (strings.HasSuffix(pattern, ".*") && strings.HasPrefix(candidate, strings.TrimSuffix(pattern, "*"))) {
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("legacy candidate %q was not covered", candidate)
		}
	}
	for _, candidate := range []string{
		"app:v1:settings:module:mailbox",
		"app:v1:settings:value:emailer",
		"app:v1:settings:mail:accounts",
	} {
		for _, pattern := range patterns {
			if pattern == candidate || (strings.HasSuffix(pattern, ".*") && strings.HasPrefix(candidate, strings.TrimSuffix(pattern, "*"))) {
				t.Fatalf("independent/non-dot candidate %q matched legacy pattern %q", candidate, pattern)
			}
		}
	}
}

func TestSettingsCacheKeysArePartitionedByTenantAndOrganization(t *testing.T) {
	client, err := New(Config{Addr: "127.0.0.1:6399", Namespace: "app:v1"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer client.Close()

	tenantA, err := tenant.NewContext("tenant-a", "org-a", false)
	if err != nil {
		t.Fatal(err)
	}
	tenantB, err := tenant.NewContext("tenant-b", "org-a", false)
	if err != nil {
		t.Fatal(err)
	}
	orgB, err := tenant.NewContext("tenant-a", "org-b", false)
	if err != nil {
		t.Fatal(err)
	}
	ctxA := tenant.WithContext(context.Background(), tenantA)
	ctxB := tenant.WithContext(context.Background(), tenantB)
	ctxOrgB := tenant.WithContext(context.Background(), orgB)

	moduleA, err := client.settingsModuleKey(ctxA, "basic")
	if err != nil {
		t.Fatal(err)
	}
	revisionA, err := client.settingsRevisionKey(ctxA, "basic")
	if err != nil {
		t.Fatal(err)
	}
	for name, key := range map[string]string{"module": moduleA, "revision": revisionA} {
		if !strings.Contains(key, ":settings:") || !strings.Contains(key, ":tenant-") || !strings.HasSuffix(key, ":basic") {
			t.Fatalf("%s key = %q, want namespaced scoped layout", name, key)
		}
	}
	moduleB, _ := client.settingsModuleKey(ctxB, "basic")
	moduleOrgB, _ := client.settingsModuleKey(ctxOrgB, "basic")
	if moduleA == moduleB || moduleA == moduleOrgB || moduleB == moduleOrgB {
		t.Fatalf("scope collision: A=%q B=%q orgB=%q", moduleA, moduleB, moduleOrgB)
	}

	globalModule, err := client.settingsModuleKey(context.Background(), "basic")
	if err != nil {
		t.Fatal(err)
	}
	if globalModule == moduleA || strings.Contains(globalModule, "tenant-") {
		t.Fatalf("global key overlaps tenant scope: %q vs %q", globalModule, moduleA)
	}
	valueA, err := client.SettingsValueKey(ctxA, "basic.site_name")
	if err != nil {
		t.Fatal(err)
	}
	valueB, err := client.SettingsValueKey(ctxB, "basic.site_name")
	if err != nil {
		t.Fatal(err)
	}
	if valueA == valueB || !strings.Contains(valueA, ":value:tenant-") {
		t.Fatalf("per-key settings cache is not scoped: A=%q B=%q", valueA, valueB)
	}
}

func TestSettingsScopeSegmentDoesNotExposeTenantIdentifiers(t *testing.T) {
	scope, err := tenant.NewContext("tenant/with:punctuation", "org/value", false)
	if err != nil {
		t.Fatal(err)
	}
	segment := settingsScopeSegment(tenant.WithContext(context.Background(), scope))
	if !strings.HasPrefix(segment, "tenant-") || len(segment) != len("tenant-")+64 {
		t.Fatalf("scope segment = %q, want sha256 digest", segment)
	}
	if strings.Contains(segment, "tenant/with") || strings.Contains(segment, "org/value") || strings.Contains(segment, ":") {
		t.Fatalf("scope segment leaks/raw key separators: %q", segment)
	}
}

func TestInvalidateModuleRevisionCASIsMonotonicPerScope(t *testing.T) {
	fixture := newRevisionRedisFixture()
	client := &Client{client: fixture, namespace: "app:v1"}

	first, err := tenant.NewContext("tenant-a", "org-a", false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := tenant.NewContext("tenant-b", "org-a", false)
	if err != nil {
		t.Fatal(err)
	}
	ctxA := tenant.WithContext(context.Background(), first)
	ctxB := tenant.WithContext(context.Background(), second)

	if err := client.InvalidateModule(ctxA, "basic", 9); err != nil {
		t.Fatalf("first InvalidateModule() error = %v", err)
	}
	// Simulate an older DB commit completing after a newer commit. The CAS
	// script must leave the higher revision visible to reconciliation workers.
	if err := client.InvalidateModule(ctxA, "basic", 7); err != nil {
		t.Fatalf("out-of-order InvalidateModule() error = %v", err)
	}
	got, err := client.ModuleRevision(ctxA, "basic")
	if err != nil || got != 9 {
		t.Fatalf("scope A revision = %d err=%v, want 9", got, err)
	}

	if err := client.InvalidateModule(ctxB, "basic", 3); err != nil {
		t.Fatalf("scope B InvalidateModule() error = %v", err)
	}
	got, err = client.ModuleRevision(ctxB, "basic")
	if err != nil || got != 3 {
		t.Fatalf("scope B revision = %d err=%v, want 3", got, err)
	}
	got, err = client.ModuleRevision(ctxA, "basic")
	if err != nil || got != 9 {
		t.Fatalf("scope A revision changed after scope B write = %d err=%v", got, err)
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
