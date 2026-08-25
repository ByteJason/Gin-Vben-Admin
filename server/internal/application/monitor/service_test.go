package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

type resourceProbeStub struct {
	cpu    HostMetric
	memory HostMetric
	disk   HostMetric
}

type slowResourceProbe struct{ resourceProbeStub }

func (p slowResourceProbe) CPU(context.Context) (HostMetric, error) {
	time.Sleep(200 * time.Millisecond)
	return HostMetric{}, errors.New("secret process detail")
}

type slowProbe struct{}

func (slowProbe) Ping(context.Context) error {
	time.Sleep(200 * time.Millisecond)
	return errors.New("secret dependency endpoint")
}

func (p resourceProbeStub) CPU(context.Context) (HostMetric, error) { return p.cpu, nil }

func (p resourceProbeStub) Memory(context.Context) (HostMetric, error) { return p.memory, nil }

func (p resourceProbeStub) Disk(context.Context, string, MetricScope) (HostMetric, error) {
	return p.disk, nil
}

func number[T int | int64 | float64](value T) *T { return &value }

type probeFunc struct{ err error }

func (p probeFunc) Ping(context.Context) error { return p.err }

type databaseStatsProbe struct{ probeFunc }

func (databaseStatsProbe) DatabaseRuntimeStats(context.Context) (DatabaseRuntimeStats, error) {
	return DatabaseRuntimeStats{
		Driver: "postgres", DriverAvailable: true, Mode: "read_write", ModeAvailable: true, PoolAvailable: true,
		Pool: DatabasePool{Open: 7, InUse: 4, Idle: 3, Max: 11, WaitCount: 5, WaitDurationMS: 1.25},
	}, nil
}

type redisStatsProbe struct{ probeFunc }

func (redisStatsProbe) RedisRuntimeStats(context.Context) (RedisRuntimeStats, error) {
	return RedisRuntimeStats{
		Mode:              "single",
		ModeAvailable:     true,
		Pool:              RedisPool{Max: number(11), Active: 4, Idle: 3, Total: 7, WaitCount: 5, WaitDurationMS: 1.25},
		PoolAvailable:     true,
		Keyspace:          0,
		KeyspaceAvailable: true,
	}, nil
}

func TestServiceIncludesSafeDependencyPoolStats(t *testing.T) {
	scope, err := tenant.NewContext("tenant-a", "", true)
	if err != nil {
		t.Fatal(err)
	}
	ctx := tenant.WithContext(context.Background(), scope)
	overview, err := NewService(Config{Database: databaseStatsProbe{probeFunc{}}, Redis: redisStatsProbe{probeFunc{}}}).Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Database.Pool == nil || *overview.Database.Pool != (DatabasePool{Open: 7, InUse: 4, Idle: 3, Max: 11, WaitCount: 5, WaitDurationMS: 1.25}) {
		t.Fatalf("database stats = %#v", overview.Database)
	}
	if overview.Database.Driver == nil || *overview.Database.Driver != "postgres" || overview.Database.Mode == nil || *overview.Database.Mode != "read_write" {
		t.Fatalf("database identity = %#v", overview.Database)
	}
	if overview.Redis.Pool == nil || overview.Redis.Pool.Max == nil || *overview.Redis.Pool.Max != 11 || overview.Redis.Pool.Active != 4 {
		t.Fatalf("redis pool stats = %#v", overview.Redis)
	}
	if overview.Redis.Keyspace == nil || *overview.Redis.Keyspace != 0 || !overview.Redis.Capabilities["keyspace"].Available {
		t.Fatalf("redis zero keyspace capability = %#v", overview.Redis)
	}
	if overview.Redis.Mode == nil || *overview.Redis.Mode != "single" || !overview.Redis.Capabilities["mode"].Available {
		t.Fatalf("redis mode = %#v", overview.Redis)
	}
}

func TestServiceReturnsDegradedDependencyWithoutSecrets(t *testing.T) {
	scope, err := tenant.NewContext("tenant-a", "", true)
	if err != nil {
		t.Fatal(err)
	}
	ctx := tenant.WithContext(context.Background(), scope)
	svc := NewService(Config{Version: "fixture", Start: time.Unix(1, 0), Clock: func() time.Time { return time.Unix(2, 0) }, Database: probeFunc{err: errors.New("dsn/password must not escape")}, Redis: probeFunc{err: errors.New("token must not escape")}})
	overview, err := svc.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Database.Status != StatusDegraded || overview.Redis.Status != StatusDegraded {
		t.Fatalf("dependency statuses = %#v", overview)
	}
	if overview.Database.Message == "" || overview.Redis.Message == "" {
		t.Fatalf("degraded messages missing: %#v", overview)
	}
	if overview.Database.Message == "dsn/password must not escape" || overview.Redis.Message == "token must not escape" {
		t.Fatalf("raw dependency error leaked: %#v", overview)
	}
}

func TestServiceCollectionRequiresTenantButDoesNotInventPlatformAdminAuthorization(t *testing.T) {
	scope, err := tenant.NewContext("tenant-a", "", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewService(Config{}).Overview(tenant.WithContext(context.Background(), scope))
	if err != nil {
		t.Fatalf("Overview() error = %v; IAM authorization belongs at the HTTP boundary", err)
	}
	if _, err := NewService(Config{}).Overview(context.Background()); err == nil {
		t.Fatal("Overview() without tenant context error = nil")
	}
}

func TestOverviewSerializesAvailableZeroValuesWithMeasurementCapabilities(t *testing.T) {
	scope, err := tenant.NewContext("tenant-a", "", true)
	if err != nil {
		t.Fatal(err)
	}
	capabilities := map[string]Capability{
		"load1":       {Scope: MetricScopeHost, Available: true, Source: "fixture"},
		"utilization": {Scope: MetricScopeProcess, Available: true, Source: "fixture"},
	}
	resources := resourceProbeStub{
		cpu: HostMetric{
			Status:       StatusOK,
			Cores:        number(0),
			Load1:        number(0.0),
			Utilization:  number(0.0),
			Capabilities: capabilities,
		},
		memory: HostMetric{Status: StatusOK},
		disk:   HostMetric{Status: StatusOK},
	}
	overview, err := NewService(Config{Resources: resources}).Overview(tenant.WithContext(context.Background(), scope))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(overview)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(payload)
	for _, fragment := range []string{`"cores":0`, `"load1":0`, `"utilization":0`, `"scope":"host"`, `"available":true`} {
		if !strings.Contains(serialized, fragment) {
			t.Fatalf("overview JSON missing %s: %s", fragment, serialized)
		}
	}
}

func TestOverviewTimesOutItemsIndependentlyAndKeepsPartialResults(t *testing.T) {
	scope, err := tenant.NewContext("tenant-a", "", true)
	if err != nil {
		t.Fatal(err)
	}
	resources := slowResourceProbe{resourceProbeStub: resourceProbeStub{
		memory: HostMetric{Status: StatusOK},
		disk:   HostMetric{Status: StatusOK},
	}}
	started := time.Now()
	overview, err := NewService(Config{
		Resources:    resources,
		Database:     slowProbe{},
		Redis:        probeFunc{},
		ProbeTimeout: 20 * time.Millisecond,
	}).Overview(tenant.WithContext(context.Background(), scope))
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= 100*time.Millisecond {
		t.Fatalf("independent probes took %s; want bounded concurrent collection", elapsed)
	}
	if overview.CPU.Status != StatusDegraded || overview.Database.Status != StatusDegraded {
		t.Fatalf("timed-out metrics = cpu:%#v database:%#v", overview.CPU, overview.Database)
	}
	if overview.Memory.Status != StatusOK || overview.Disk.Status != StatusOK || overview.Redis.Status != StatusOK {
		t.Fatalf("healthy partial metrics were lost: %#v", overview)
	}
	payload, err := json.Marshal(overview)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "secret") {
		t.Fatalf("raw probe detail leaked: %s", payload)
	}
}

func TestDefaultOverviewSeparatesRuntimeHeapProcessRSSAndHostLoad(t *testing.T) {
	scope, err := tenant.NewContext("tenant-a", "", true)
	if err != nil {
		t.Fatal(err)
	}
	overview, err := NewService(Config{Version: "v1.2.3", Commit: "abc123"}).Overview(tenant.WithContext(context.Background(), scope))
	if err != nil {
		t.Fatal(err)
	}
	if overview.Runtime.GoVersion == "" || overview.Runtime.OS == "" || overview.Runtime.Arch == "" {
		t.Fatalf("runtime build identity = %#v", overview.Runtime)
	}
	if overview.Runtime.ApplicationVersion == nil || *overview.Runtime.ApplicationVersion != "v1.2.3" || overview.Runtime.Commit == nil || *overview.Runtime.Commit != "abc123" {
		t.Fatalf("injected build metadata = %#v", overview.Runtime)
	}
	if overview.Runtime.HeapAllocBytes == nil || overview.Runtime.HeapSysBytes == nil || overview.Runtime.GCCount == nil {
		t.Fatalf("Go heap/GC metrics missing: %#v", overview.Runtime)
	}
	if capability := overview.Runtime.Capabilities["heapAllocBytes"]; !capability.Available || capability.Scope != MetricScopeProcess {
		t.Fatalf("heap capability = %#v", capability)
	}
	for _, field := range []string{"load1", "load5", "load15", "utilization"} {
		if _, exists := overview.CPU.Capabilities[field]; !exists {
			t.Fatalf("CPU capability %q missing: %#v", field, overview.CPU)
		}
	}
	if overview.CPU.Utilization != nil || overview.CPU.Capabilities["utilization"].Available {
		t.Fatalf("host load was mislabeled as CPU utilization: %#v", overview.CPU)
	}
	if _, exists := overview.Memory.Capabilities["rssBytes"]; !exists {
		t.Fatalf("process RSS capability missing: %#v", overview.Memory)
	}
	if overview.Disk.FreeBytes == nil || !overview.Disk.Capabilities["freeBytes"].Available {
		t.Fatalf("filesystem free-space metric missing: %#v", overview.Disk)
	}
}
