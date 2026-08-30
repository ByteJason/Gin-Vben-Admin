// Package monitor provides a credential-free, read-only runtime snapshot.
package monitor

import (
	"context"
	"errors"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

type Status string

const (
	StatusOK          Status = "ok"
	StatusDegraded    Status = "degraded"
	StatusUnavailable Status = "unavailable"
)

// MetricScope identifies whose resources a measurement actually describes.
// A process metric must never be presented as a host metric merely because it
// was collected by the host operating system.
type MetricScope string

const (
	MetricScopeProcess   MetricScope = "process"
	MetricScopeContainer MetricScope = "container"
	MetricScopeHost      MetricScope = "host"
)

// Capability makes optional measurements explicit. Numeric pointers on the
// metric distinguish a valid zero from a value that could not be collected.
type Capability struct {
	Scope     MetricScope `json:"scope"`
	Available bool        `json:"available"`
	Source    string      `json:"source,omitempty"`
}

// ResourceProbe is the platform seam for read-only CPU, memory, and filesystem
// observations. Implementations are responsible for labeling every field with
// the scope it actually measures.
type ResourceProbe interface {
	CPU(context.Context) (HostMetric, error)
	Memory(context.Context) (HostMetric, error)
	Disk(context.Context, string, MetricScope) (HostMetric, error)
}

type Probe interface {
	Ping(context.Context) error
}

// DatabaseStatsProbe and RedisStatsProbe are separate credential-free ports so
// pool counters cannot be confused across unlike drivers.
type DatabaseStatsProbe interface {
	Probe
	DatabaseRuntimeStats(context.Context) (DatabaseRuntimeStats, error)
}

type RedisStatsProbe interface {
	Probe
	RedisRuntimeStats(context.Context) (RedisRuntimeStats, error)
}

// BackgroundTaskStatsProbe is the optional seam used by a queue or scheduler
// adapter. It exposes counters only: task names, payloads, queue addresses and
// error text deliberately remain outside the operations snapshot.
type BackgroundTaskStatsProbe interface {
	Probe
	BackgroundTaskRuntimeStats(context.Context) (BackgroundTaskRuntimeStats, error)
}

type Config struct {
	Version         string
	Commit          string
	Scope           string
	Start           time.Time
	Clock           func() time.Time
	DiskPath        string
	ProbeTimeout    time.Duration
	Resources       ResourceProbe
	Database        Probe
	Redis           Probe
	BackgroundTasks Probe
	// DataSource and IsSynthetic identify fixture-backed snapshots without
	// making callers infer provenance from the metric values themselves.
	DataSource      string
	IsSynthetic     bool
	RefreshInterval time.Duration
}

type HostMetric struct {
	Status       Status                `json:"status"`
	Cores        *int                  `json:"cores,omitempty"`
	Load1        *float64              `json:"load1,omitempty"`
	Load5        *float64              `json:"load5,omitempty"`
	Load15       *float64              `json:"load15,omitempty"`
	LoadPerCore  *float64              `json:"loadPerCore,omitempty"`
	PerCoreLoad  []float64             `json:"perCoreLoad,omitempty"`
	RSSBytes     *int64                `json:"rssBytes,omitempty"`
	UsedBytes    *int64                `json:"usedBytes,omitempty"`
	FreeBytes    *int64                `json:"freeBytes,omitempty"`
	TotalBytes   *int64                `json:"totalBytes,omitempty"`
	Utilization  *float64              `json:"utilization,omitempty"`
	Capabilities map[string]Capability `json:"capabilities"`
	Message      string                `json:"message,omitempty"`
}

type RuntimeMetric struct {
	Status             Status                `json:"status"`
	GoVersion          string                `json:"goVersion"`
	OS                 string                `json:"os"`
	Arch               string                `json:"arch"`
	Compiler           string                `json:"compiler"`
	ApplicationVersion *string               `json:"applicationVersion,omitempty"`
	Commit             *string               `json:"commit,omitempty"`
	HeapAllocBytes     *uint64               `json:"heapAllocBytes,omitempty"`
	HeapSysBytes       *uint64               `json:"heapSysBytes,omitempty"`
	HeapInUseBytes     *uint64               `json:"heapInUseBytes,omitempty"`
	HeapObjects        *uint64               `json:"heapObjects,omitempty"`
	NextGCBytes        *uint64               `json:"nextGcBytes,omitempty"`
	GCCount            *uint32               `json:"gcCount,omitempty"`
	LastGCPauseNS      *uint64               `json:"lastGcPauseNs,omitempty"`
	Capabilities       map[string]Capability `json:"capabilities"`
}

type DatabasePool struct {
	Open  int `json:"open"`
	InUse int `json:"inUse"`
	// Active duplicates the standard library's InUse counter using the name
	// expected by the server-status screen. Both values always match.
	Active            int     `json:"active"`
	Idle              int     `json:"idle"`
	Max               int     `json:"max"`
	WaitCount         int64   `json:"waitCount"`
	WaitDurationMS    float64 `json:"waitDurationMs"`
	MaxIdleClosed     int64   `json:"maxIdleClosed"`
	MaxIdleTimeClosed int64   `json:"maxIdleTimeClosed"`
	MaxLifetimeClosed int64   `json:"maxLifetimeClosed"`
}

type DatabaseRuntimeStats struct {
	Driver          string
	DriverAvailable bool
	Mode            string
	ModeAvailable   bool
	Pool            DatabasePool
	PoolAvailable   bool
}

type RedisPool struct {
	Max            *int    `json:"max,omitempty"`
	Total          int     `json:"total"`
	Active         int     `json:"active"`
	Idle           int     `json:"idle"`
	Hits           uint32  `json:"hits"`
	Misses         uint32  `json:"misses"`
	Timeouts       uint32  `json:"timeouts"`
	WaitCount      uint32  `json:"waitCount"`
	WaitDurationMS float64 `json:"waitDurationMs"`
	Stale          uint32  `json:"stale"`
	Pending        uint32  `json:"pending"`
}

type RedisRuntimeStats struct {
	Mode              string
	ModeAvailable     bool
	Pool              RedisPool
	PoolAvailable     bool
	Keyspace          int64
	KeyspaceAvailable bool
}

// BackgroundTaskRuntimeStats is intentionally small and source-agnostic so
// scheduler, queue and fixture adapters can implement it without leaking task
// details. Available distinguishes a valid all-zero queue from no collector.
type BackgroundTaskRuntimeStats struct {
	Queued    int
	Active    int
	Scheduled int
	Failed    int
	Available bool
}

type GoroutineMetric struct {
	Status       Status                `json:"status"`
	Count        *int                  `json:"count,omitempty"`
	Capabilities map[string]Capability `json:"capabilities"`
}

type BackgroundTaskMetric struct {
	Status       Status                `json:"status"`
	Queued       *int                  `json:"queued,omitempty"`
	Active       *int                  `json:"active,omitempty"`
	Scheduled    *int                  `json:"scheduled,omitempty"`
	Failed       *int                  `json:"failed,omitempty"`
	Capabilities map[string]Capability `json:"capabilities"`
	Message      string                `json:"message,omitempty"`
}

type DatabaseMetric struct {
	Status       Status                `json:"status"`
	LatencyMS    float64               `json:"latencyMs"`
	Driver       *string               `json:"driver,omitempty"`
	Mode         *string               `json:"mode,omitempty"`
	Pool         *DatabasePool         `json:"pool,omitempty"`
	Capabilities map[string]Capability `json:"capabilities"`
	Message      string                `json:"message,omitempty"`
}

type RedisMetric struct {
	Status       Status                `json:"status"`
	LatencyMS    float64               `json:"latencyMs"`
	Mode         *string               `json:"mode,omitempty"`
	Pool         *RedisPool            `json:"pool,omitempty"`
	Keyspace     *int64                `json:"keyspace,omitempty"`
	Capabilities map[string]Capability `json:"capabilities"`
	Message      string                `json:"message,omitempty"`
}

type Overview struct {
	// Status is a coarse summary. Individual metrics retain their own status so
	// a degraded optional collector never hides healthy snapshot data.
	Status          Status               `json:"status"`
	Scope           MetricScope          `json:"scope"`
	UptimeSeconds   float64              `json:"uptimeSeconds"`
	Version         string               `json:"version,omitempty"`
	Runtime         RuntimeMetric        `json:"runtime"`
	CPU             HostMetric           `json:"cpu"`
	Memory          HostMetric           `json:"memory"`
	Disk            HostMetric           `json:"disk"`
	Database        DatabaseMetric       `json:"database"`
	Redis           RedisMetric          `json:"redis"`
	Goroutines      GoroutineMetric      `json:"goroutines"`
	BackgroundTasks BackgroundTaskMetric `json:"backgroundTasks"`
	CollectedAt     time.Time            `json:"collectedAt"`
	// Timestamp is the canonical endpoint's refresh anchor. CollectedAt stays
	// for the original monitor endpoint's compatibility contract.
	Timestamp              time.Time `json:"timestamp"`
	RefreshIntervalSeconds int       `json:"refreshIntervalSeconds"`
	RefreshIntervalMS      int64     `json:"refreshIntervalMs"`
	DataSource             string    `json:"dataSource"`
	IsSynthetic            bool      `json:"isSynthetic"`
}

// ServerStatus is the canonical operations DTO. Keeping it as an alias makes
// /ops/server-status additive while /ops/monitor continues to serialize the
// exact same bounded snapshot.
type ServerStatus = Overview

type Service struct {
	version         string
	commit          string
	scope           MetricScope
	start           time.Time
	clock           func() time.Time
	diskPath        string
	probeTimeout    time.Duration
	resources       ResourceProbe
	database        Probe
	redis           Probe
	backgroundTasks Probe
	dataSource      string
	isSynthetic     bool
	fixtureMode     bool
	refreshInterval time.Duration
}

func NewService(cfg Config) *Service {
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	start := cfg.Start
	if start.IsZero() {
		start = clock()
	}
	scope := normalizeMetricScope(cfg.Scope)
	diskPath := strings.TrimSpace(cfg.DiskPath)
	if diskPath == "" {
		diskPath = "."
	}
	resources := cfg.Resources
	if resources == nil {
		resources = defaultResourceProbe{}
	}
	probeTimeout := cfg.ProbeTimeout
	if probeTimeout <= 0 {
		probeTimeout = 2 * time.Second
	}
	refreshInterval := cfg.RefreshInterval
	if refreshInterval <= 0 {
		refreshInterval = 10 * time.Second
	}
	dataSource := strings.ToLower(strings.TrimSpace(cfg.DataSource))
	if dataSource == "" {
		dataSource = "live"
	}
	isSynthetic := cfg.IsSynthetic || dataSource == "fixture" || dataSource == "synthetic"
	if isSynthetic && dataSource == "live" {
		dataSource = "fixture"
	}
	fixtureMode := dataSource == "fixture" && cfg.Resources == nil && cfg.Database == nil && cfg.Redis == nil && cfg.BackgroundTasks == nil
	return &Service{version: strings.TrimSpace(cfg.Version), commit: strings.TrimSpace(cfg.Commit), scope: scope, start: start, clock: clock, diskPath: diskPath, probeTimeout: probeTimeout, resources: resources, database: cfg.Database, redis: cfg.Redis, backgroundTasks: cfg.BackgroundTasks, dataSource: dataSource, isSynthetic: isSynthetic, fixtureMode: fixtureMode, refreshInterval: refreshInterval}
}

func (s *Service) Overview(ctx context.Context) (Overview, error) {
	if s == nil {
		return Overview{}, errors.New("monitor service is not initialized")
	}
	if _, err := tenant.RequireContext(ctx); err != nil {
		return Overview{}, err
	}
	now := s.clock().UTC()
	up := now.Sub(s.start)
	if up < 0 {
		up = 0
	}
	if s.fixtureMode {
		return s.fixtureSnapshot(now, up), nil
	}
	cpuResult := asyncHostMetric(ctx, s.probeTimeout, unavailableCPUMetric("cpu metrics timed out"), s.resources.CPU)
	memoryResult := asyncHostMetric(ctx, s.probeTimeout, unavailableMemoryMetric("memory metrics timed out"), s.resources.Memory)
	diskResult := asyncHostMetric(ctx, s.probeTimeout, unavailableDiskMetric(s.scope, "disk metrics timed out"), func(itemCtx context.Context) (HostMetric, error) {
		return s.resources.Disk(itemCtx, s.diskPath, s.scope)
	})
	databaseResult := asyncDatabaseMetric(ctx, s.probeTimeout, s.database)
	redisResult := asyncRedisMetric(ctx, s.probeTimeout, s.redis)
	backgroundTaskResult := asyncBackgroundTaskMetric(ctx, s.probeTimeout, s.backgroundTasks)
	goRoutines := goroutineMetric()
	overview := Overview{
		Scope: s.scope, UptimeSeconds: up.Seconds(), Version: s.version, Runtime: runtimeMetric(s.version, s.commit), CollectedAt: now,
		CPU:                    <-cpuResult,
		Memory:                 <-memoryResult,
		Disk:                   <-diskResult,
		Database:               <-databaseResult,
		Redis:                  <-redisResult,
		Goroutines:             goRoutines,
		BackgroundTasks:        <-backgroundTaskResult,
		Timestamp:              now,
		RefreshIntervalSeconds: int(s.refreshInterval / time.Second),
		RefreshIntervalMS:      s.refreshInterval.Milliseconds(),
		DataSource:             s.dataSource,
		IsSynthetic:            s.isSynthetic,
	}
	overview.CPU = normalizeCPUMetric(overview.CPU)
	overview.Status = summaryStatus(overview)
	return overview, nil
}

// fixtureSnapshot supplies deterministic local-development values for the
// standalone profile. It is selected only when no live probes were injected;
// authenticated deployments continue through the live collector path.
func (s *Service) fixtureSnapshot(now time.Time, uptime time.Duration) Overview {
	cores := runtime.GOMAXPROCS(0)
	if cores < 1 {
		cores = 1
	}
	load := 0.438
	perCore := make([]float64, cores)
	for index := range perCore {
		perCore[index] = 0.28 + float64((index*13)%37)/100
	}
	usedMemory := int64(684 * 1024 * 1024)
	totalMemory := int64(1024 * 1024 * 1024)
	usedDisk := int64(57 * 1024 * 1024 * 1024)
	totalDisk := int64(100 * 1024 * 1024 * 1024)
	diskFree := totalDisk - usedDisk
	dbPool := &DatabasePool{Open: 126, InUse: 42, Active: 42, Idle: 84, Max: 200, WaitCount: 8, WaitDurationMS: 1.4}
	redisPool := &RedisPool{Max: pointer(128), Total: 64, Active: 18, Idle: 46, Hits: 12450, Misses: 382, Timeouts: 2, WaitCount: 3, WaitDurationMS: 0.8, Stale: 1, Pending: 4}
	dbDriver, dbMode := "mysql", "single"
	redisMode := "single"
	memoryUtilization := float64(usedMemory) / float64(totalMemory)
	diskUtilization := float64(usedDisk) / float64(totalDisk)
	overview := Overview{
		Status: StatusOK, Scope: s.scope, UptimeSeconds: uptime.Seconds(), Version: s.version,
		Runtime:         runtimeMetric(s.version, s.commit),
		CPU:             HostMetric{Status: StatusOK, Cores: pointer(cores), Load1: pointer(load * float64(cores)), Load5: pointer(1.54), Load15: pointer(1.21), LoadPerCore: pointer(load), PerCoreLoad: perCore, Utilization: pointer(load), Capabilities: fixtureCapabilities("cores", "load1", "load5", "load15", "loadPerCore", "perCoreLoad", "utilization")},
		Memory:          HostMetric{Status: StatusOK, UsedBytes: pointer(usedMemory), TotalBytes: pointer(totalMemory), Utilization: pointer(memoryUtilization), Capabilities: fixtureCapabilities("usedBytes", "totalBytes", "utilization")},
		Disk:            HostMetric{Status: StatusOK, UsedBytes: pointer(usedDisk), FreeBytes: pointer(diskFree), TotalBytes: pointer(totalDisk), Utilization: pointer(diskUtilization), Capabilities: fixtureCapabilities("usedBytes", "freeBytes", "totalBytes", "utilization")},
		Database:        DatabaseMetric{Status: StatusOK, LatencyMS: 8, Driver: pointer(dbDriver), Mode: pointer(dbMode), Pool: dbPool, Capabilities: fixtureCapabilities("latency", "driver", "mode", "pool")},
		Redis:           RedisMetric{Status: StatusOK, LatencyMS: 2, Mode: pointer(redisMode), Pool: redisPool, Keyspace: pointer(int64(42318)), Capabilities: fixtureCapabilities("latency", "mode", "pool", "keyspace")},
		Goroutines:      GoroutineMetric{Status: StatusOK, Count: pointer(1284), Capabilities: fixtureCapabilities("count")},
		BackgroundTasks: BackgroundTaskMetric{Status: StatusOK, Queued: pointer(3), Active: pointer(2), Scheduled: pointer(37), Failed: pointer(1), Capabilities: fixtureCapabilities("queued", "active", "scheduled", "failed")},
		CollectedAt:     now, Timestamp: now, RefreshIntervalSeconds: int(s.refreshInterval / time.Second), RefreshIntervalMS: s.refreshInterval.Milliseconds(), DataSource: fixtureDataSource, IsSynthetic: true,
	}
	return overview
}

const fixtureDataSource = "fixture"

func fixtureCapabilities(names ...string) map[string]Capability {
	capabilities := make(map[string]Capability, len(names))
	for _, name := range names {
		capabilities[name] = Capability{Scope: MetricScopeProcess, Available: true, Source: "local.fixture"}
	}
	return capabilities
}

// normalizeCPUMetric fills the derived per-core value for custom probes as
// well as the built-in probe. A zero load is a valid measurement, so the
// presence checks intentionally use pointers rather than truthiness.
func normalizeCPUMetric(metric HostMetric) HostMetric {
	if metric.LoadPerCore == nil && metric.Load1 != nil && metric.Cores != nil && *metric.Cores > 0 {
		metric.LoadPerCore = pointer(*metric.Load1 / float64(*metric.Cores))
		if metric.Capabilities == nil {
			metric.Capabilities = map[string]Capability{}
		}
		if _, exists := metric.Capabilities["loadPerCore"]; !exists {
			metric.Capabilities["loadPerCore"] = Capability{Scope: MetricScopeHost, Available: true, Source: "derived.load1/cores"}
		}
	}
	return metric
}

// ServerStatus returns the canonical snapshot used by the server-status
// endpoint. Overview remains the compatibility method for monitor consumers.
func (s *Service) ServerStatus(ctx context.Context) (ServerStatus, error) { return s.Overview(ctx) }

type defaultResourceProbe struct{}

func (defaultResourceProbe) CPU(context.Context) (HostMetric, error) {
	cores := runtime.GOMAXPROCS(0)
	if cores < 1 {
		cores = 1
	}
	metric := HostMetric{
		Status: StatusOK,
		Cores:  pointer(cores),
		Capabilities: map[string]Capability{
			"cores":       {Scope: MetricScopeProcess, Available: true, Source: "go.runtime.gomaxprocs"},
			"load1":       {Scope: MetricScopeHost, Available: false},
			"load5":       {Scope: MetricScopeHost, Available: false},
			"load15":      {Scope: MetricScopeHost, Available: false},
			"loadPerCore": {Scope: MetricScopeHost, Available: false},
			"perCoreLoad": {Scope: MetricScopeHost, Available: false},
			"utilization": {Scope: MetricScopeProcess, Available: false},
		},
	}
	// Linux containers expose a cheap, read-only one-minute load signal. The
	// value is host-scoped and intentionally is not relabeled as CPU usage.
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		for index, field := range []struct {
			name  string
			value **float64
		}{{"load1", &metric.Load1}, {"load5", &metric.Load5}, {"load15", &metric.Load15}} {
			if len(fields) <= index {
				break
			}
			if load, parseErr := strconv.ParseFloat(fields[index], 64); parseErr == nil {
				*field.value = pointer(load)
				metric.Capabilities[field.name] = Capability{Scope: MetricScopeHost, Available: true, Source: "proc.loadavg"}
			}
		}
	}
	if metric.Load1 != nil && metric.Cores != nil && *metric.Cores > 0 {
		metric.LoadPerCore = pointer(*metric.Load1 / float64(*metric.Cores))
		metric.Capabilities["loadPerCore"] = Capability{Scope: MetricScopeHost, Available: true, Source: "proc.loadavg/gomaxprocs"}
	}
	return metric, nil
}

func (defaultResourceProbe) Memory(context.Context) (HostMetric, error) {
	rss, source, err := processRSS()
	metric := HostMetric{Status: StatusUnavailable, Capabilities: map[string]Capability{
		"rssBytes": {Scope: MetricScopeProcess, Available: false},
	}}
	if err != nil {
		metric.Message = "process RSS unavailable"
		return metric, nil
	}
	metric.Status = StatusOK
	metric.RSSBytes = pointer(rss)
	metric.Capabilities["rssBytes"] = Capability{Scope: MetricScopeProcess, Available: true, Source: source}
	return metric, nil
}

func (defaultResourceProbe) Disk(_ context.Context, path string, scope MetricScope) (HostMetric, error) {
	capabilities := map[string]Capability{
		"usedBytes":   {Scope: scope, Available: false},
		"freeBytes":   {Scope: scope, Available: false},
		"totalBytes":  {Scope: scope, Available: false},
		"utilization": {Scope: scope, Available: false},
	}
	total, free, err := diskSpace(path)
	if err != nil {
		return HostMetric{Status: StatusDegraded, Capabilities: capabilities, Message: "disk metrics unavailable"}, nil
	}
	used := total - free
	if used < 0 {
		used = 0
	}
	utilization := float64(0)
	if total > 0 {
		utilization = float64(used) / float64(total)
	}
	for field := range capabilities {
		capabilities[field] = Capability{Scope: scope, Available: true, Source: "filesystem.statfs"}
	}
	return HostMetric{Status: StatusOK, UsedBytes: pointer(used), FreeBytes: pointer(free), TotalBytes: pointer(total), Utilization: pointer(utilization), Capabilities: capabilities}, nil
}

func normalizeMetricScope(value string) MetricScope {
	switch MetricScope(strings.ToLower(strings.TrimSpace(value))) {
	case MetricScopeContainer:
		return MetricScopeContainer
	case MetricScopeHost:
		return MetricScopeHost
	default:
		return MetricScopeProcess
	}
}

func resourceMetric(metric HostMetric, err error, message string) HostMetric {
	if err != nil {
		return HostMetric{Status: StatusDegraded, Capabilities: map[string]Capability{}, Message: message}
	}
	if metric.Capabilities == nil {
		metric.Capabilities = map[string]Capability{}
	}
	return metric
}

type hostMetricResult struct {
	metric HostMetric
	err    error
}

func asyncHostMetric(ctx context.Context, timeout time.Duration, fallback HostMetric, collect func(context.Context) (HostMetric, error)) <-chan HostMetric {
	output := make(chan HostMetric, 1)
	go func() {
		itemCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		result := make(chan hostMetricResult, 1)
		go func() {
			metric, err := collect(itemCtx)
			result <- hostMetricResult{metric: metric, err: err}
		}()
		select {
		case measured := <-result:
			if measured.err != nil {
				fallback.Message = strings.Replace(fallback.Message, "timed out", "unavailable", 1)
				output <- fallback
				return
			}
			output <- resourceMetric(measured.metric, nil, fallback.Message)
		case <-itemCtx.Done():
			output <- fallback
		}
	}()
	return output
}

func asyncDatabaseMetric(ctx context.Context, timeout time.Duration, probe Probe) <-chan DatabaseMetric {
	output := make(chan DatabaseMetric, 1)
	if probe == nil {
		output <- unavailableDatabaseMetric(StatusUnavailable, "dependency not configured")
		return output
	}
	go func() {
		itemCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		result := make(chan DatabaseMetric, 1)
		go func() { result <- databaseMetric(itemCtx, probe) }()
		select {
		case metric := <-result:
			output <- metric
		case <-itemCtx.Done():
			output <- unavailableDatabaseMetric(StatusDegraded, "dependency probe timed out")
		}
	}()
	return output
}

func asyncRedisMetric(ctx context.Context, timeout time.Duration, probe Probe) <-chan RedisMetric {
	output := make(chan RedisMetric, 1)
	if probe == nil {
		output <- unavailableRedisMetric(StatusUnavailable, "dependency not configured")
		return output
	}
	go func() {
		itemCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		result := make(chan RedisMetric, 1)
		go func() { result <- redisMetric(itemCtx, probe) }()
		select {
		case metric := <-result:
			output <- metric
		case <-itemCtx.Done():
			output <- unavailableRedisMetric(StatusDegraded, "dependency probe timed out")
		}
	}()
	return output
}

func asyncBackgroundTaskMetric(ctx context.Context, timeout time.Duration, probe Probe) <-chan BackgroundTaskMetric {
	output := make(chan BackgroundTaskMetric, 1)
	if probe == nil {
		output <- unavailableBackgroundTaskMetric(StatusUnavailable, "collector not configured")
		return output
	}
	go func() {
		itemCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		result := make(chan BackgroundTaskMetric, 1)
		go func() { result <- backgroundTaskMetric(itemCtx, probe) }()
		select {
		case metric := <-result:
			output <- metric
		case <-itemCtx.Done():
			output <- unavailableBackgroundTaskMetric(StatusDegraded, "collector timed out")
		}
	}()
	return output
}

func unavailableCPUMetric(message string) HostMetric {
	return HostMetric{Status: StatusDegraded, Message: message, Capabilities: map[string]Capability{
		"cores":       {Scope: MetricScopeProcess, Available: false},
		"load1":       {Scope: MetricScopeHost, Available: false},
		"load5":       {Scope: MetricScopeHost, Available: false},
		"load15":      {Scope: MetricScopeHost, Available: false},
		"loadPerCore": {Scope: MetricScopeHost, Available: false},
		"perCoreLoad": {Scope: MetricScopeHost, Available: false},
		"utilization": {Scope: MetricScopeProcess, Available: false},
	}}
}

func unavailableMemoryMetric(message string) HostMetric {
	return HostMetric{Status: StatusDegraded, Message: message, Capabilities: map[string]Capability{
		"rssBytes": {Scope: MetricScopeProcess, Available: false},
	}}
}

func unavailableDiskMetric(scope MetricScope, message string) HostMetric {
	return HostMetric{Status: StatusDegraded, Message: message, Capabilities: map[string]Capability{
		"usedBytes":   {Scope: scope, Available: false},
		"freeBytes":   {Scope: scope, Available: false},
		"totalBytes":  {Scope: scope, Available: false},
		"utilization": {Scope: scope, Available: false},
	}}
}

func runtimeMetric(version, commit string) RuntimeMetric {
	metric := RuntimeMetric{
		Status: StatusOK, GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, Compiler: runtime.Compiler,
		Capabilities: map[string]Capability{
			"goVersion":          {Scope: MetricScopeProcess, Available: true, Source: "go.runtime"},
			"os":                 {Scope: MetricScopeProcess, Available: true, Source: "go.runtime"},
			"arch":               {Scope: MetricScopeProcess, Available: true, Source: "go.runtime"},
			"compiler":           {Scope: MetricScopeProcess, Available: true, Source: "go.runtime"},
			"applicationVersion": {Scope: MetricScopeProcess, Available: false},
			"commit":             {Scope: MetricScopeProcess, Available: false},
		},
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if strings.TrimSpace(info.GoVersion) != "" {
			metric.GoVersion = info.GoVersion
		}
		if commit == "" {
			for _, setting := range info.Settings {
				if setting.Key == "vcs.revision" {
					commit = strings.TrimSpace(setting.Value)
					break
				}
			}
		}
	}
	if version != "" {
		metric.ApplicationVersion = pointer(version)
		metric.Capabilities["applicationVersion"] = Capability{Scope: MetricScopeProcess, Available: true, Source: "build.config"}
	}
	if commit != "" {
		metric.Commit = pointer(commit)
		metric.Capabilities["commit"] = Capability{Scope: MetricScopeProcess, Available: true, Source: "build.vcs"}
	}
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	metric.HeapAllocBytes = pointer(stats.HeapAlloc)
	metric.HeapSysBytes = pointer(stats.HeapSys)
	metric.HeapInUseBytes = pointer(stats.HeapInuse)
	metric.HeapObjects = pointer(stats.HeapObjects)
	metric.NextGCBytes = pointer(stats.NextGC)
	metric.GCCount = pointer(stats.NumGC)
	for _, field := range []string{"heapAllocBytes", "heapSysBytes", "heapInUseBytes", "heapObjects", "nextGcBytes", "gcCount"} {
		metric.Capabilities[field] = Capability{Scope: MetricScopeProcess, Available: true, Source: "go.runtime.memstats"}
	}
	metric.Capabilities["lastGcPauseNs"] = Capability{Scope: MetricScopeProcess, Available: false}
	if stats.NumGC > 0 {
		pause := stats.PauseNs[(stats.NumGC-1)%uint32(len(stats.PauseNs))]
		metric.LastGCPauseNS = pointer(pause)
		metric.Capabilities["lastGcPauseNs"] = Capability{Scope: MetricScopeProcess, Available: true, Source: "go.runtime.memstats"}
	}
	return metric
}

func goroutineMetric() GoroutineMetric {
	count := runtime.NumGoroutine()
	return GoroutineMetric{Status: StatusOK, Count: pointer(count), Capabilities: map[string]Capability{
		"count": {Scope: MetricScopeProcess, Available: true, Source: "go.runtime.num_goroutine"},
	}}
}

func backgroundTaskMetric(ctx context.Context, probe Probe) BackgroundTaskMetric {
	if err := probe.Ping(ctx); err != nil {
		return unavailableBackgroundTaskMetric(StatusDegraded, "collector unavailable")
	}
	statsProbe, ok := probe.(BackgroundTaskStatsProbe)
	if !ok {
		return unavailableBackgroundTaskMetric(StatusUnavailable, "collector does not expose counters")
	}
	stats, err := statsProbe.BackgroundTaskRuntimeStats(ctx)
	if err != nil || !stats.Available {
		return unavailableBackgroundTaskMetric(StatusDegraded, "collector stats unavailable")
	}
	return BackgroundTaskMetric{
		Status: StatusOK, Queued: pointer(stats.Queued), Active: pointer(stats.Active), Scheduled: pointer(stats.Scheduled), Failed: pointer(stats.Failed),
		Capabilities: map[string]Capability{
			"queued":    {Scope: MetricScopeProcess, Available: true, Source: "background-tasks"},
			"active":    {Scope: MetricScopeProcess, Available: true, Source: "background-tasks"},
			"scheduled": {Scope: MetricScopeProcess, Available: true, Source: "background-tasks"},
			"failed":    {Scope: MetricScopeProcess, Available: true, Source: "background-tasks"},
		},
	}
}

func pointer[T any](value T) *T { return &value }

func databaseMetric(ctx context.Context, probe Probe) DatabaseMetric {
	started := time.Now()
	if err := probe.Ping(ctx); err != nil {
		metric := unavailableDatabaseMetric(StatusDegraded, "dependency unavailable")
		metric.LatencyMS = elapsedMilliseconds(started)
		metric.Capabilities["latency"] = Capability{Scope: MetricScopeProcess, Available: true, Source: "database.ping"}
		return metric
	}
	metric := DatabaseMetric{Status: StatusOK, LatencyMS: elapsedMilliseconds(started), Capabilities: map[string]Capability{
		"latency": {Scope: MetricScopeProcess, Available: true, Source: "database.ping"},
		"driver":  {Scope: MetricScopeProcess, Available: false},
		"mode":    {Scope: MetricScopeProcess, Available: false},
		"pool":    {Scope: MetricScopeProcess, Available: false},
	}}
	if statsProbe, ok := probe.(DatabaseStatsProbe); ok {
		stats, err := statsProbe.DatabaseRuntimeStats(ctx)
		if err != nil {
			metric.Status = StatusDegraded
			metric.Message = "dependency stats unavailable"
		} else {
			if stats.DriverAvailable {
				metric.Driver = pointer(stats.Driver)
				metric.Capabilities["driver"] = Capability{Scope: MetricScopeProcess, Available: true, Source: "database.config"}
			}
			if stats.ModeAvailable {
				metric.Mode = pointer(stats.Mode)
				metric.Capabilities["mode"] = Capability{Scope: MetricScopeProcess, Available: true, Source: "database.config"}
			}
			if stats.PoolAvailable {
				pool := stats.Pool
				pool.Active = pool.InUse
				metric.Pool = &pool
			}
			if stats.PoolAvailable {
				metric.Capabilities["pool"] = Capability{Scope: MetricScopeProcess, Available: true, Source: "database.sql.pool"}
			}
		}
	}
	return metric
}

func redisMetric(ctx context.Context, probe Probe) RedisMetric {
	started := time.Now()
	if err := probe.Ping(ctx); err != nil {
		metric := unavailableRedisMetric(StatusDegraded, "dependency unavailable")
		metric.LatencyMS = elapsedMilliseconds(started)
		metric.Capabilities["latency"] = Capability{Scope: MetricScopeProcess, Available: true, Source: "redis.ping"}
		return metric
	}
	metric := RedisMetric{Status: StatusOK, LatencyMS: elapsedMilliseconds(started), Capabilities: map[string]Capability{
		"latency":  {Scope: MetricScopeProcess, Available: true, Source: "redis.ping"},
		"mode":     {Scope: MetricScopeProcess, Available: false},
		"pool":     {Scope: MetricScopeProcess, Available: false},
		"pool.max": {Scope: MetricScopeProcess, Available: false},
		"keyspace": {Scope: MetricScopeProcess, Available: false},
	}}
	if statsProbe, ok := probe.(RedisStatsProbe); ok {
		stats, err := statsProbe.RedisRuntimeStats(ctx)
		if err != nil {
			metric.Status = StatusDegraded
			metric.Message = "dependency stats unavailable"
			return metric
		}
		if stats.PoolAvailable {
			pool := stats.Pool
			metric.Pool = &pool
			metric.Capabilities["pool"] = Capability{Scope: MetricScopeProcess, Available: true, Source: "redis.pool"}
			metric.Capabilities["pool.max"] = Capability{Scope: MetricScopeProcess, Available: pool.Max != nil, Source: "redis.pool.options"}
		}
		if stats.ModeAvailable {
			metric.Mode = pointer(stats.Mode)
			metric.Capabilities["mode"] = Capability{Scope: MetricScopeProcess, Available: true, Source: "redis.config"}
		}
		if stats.KeyspaceAvailable {
			metric.Keyspace = pointer(stats.Keyspace)
			metric.Capabilities["keyspace"] = Capability{Scope: MetricScopeProcess, Available: true, Source: "redis.dbsize"}
		}
	}
	return metric
}

func unavailableBackgroundTaskMetric(status Status, message string) BackgroundTaskMetric {
	return BackgroundTaskMetric{Status: status, Message: message, Capabilities: map[string]Capability{
		"queued":    {Scope: MetricScopeProcess, Available: false},
		"active":    {Scope: MetricScopeProcess, Available: false},
		"scheduled": {Scope: MetricScopeProcess, Available: false},
		"failed":    {Scope: MetricScopeProcess, Available: false},
	}}
}

func summaryStatus(overview Overview) Status {
	status := StatusOK
	for _, item := range []Status{overview.CPU.Status, overview.Memory.Status, overview.Disk.Status, overview.Database.Status, overview.Redis.Status, overview.Goroutines.Status, overview.BackgroundTasks.Status} {
		if item == StatusUnavailable {
			status = StatusUnavailable
			continue
		}
		if item == StatusDegraded && status == StatusOK {
			status = StatusDegraded
		}
	}
	return status
}

func unavailableDatabaseMetric(status Status, message string) DatabaseMetric {
	return DatabaseMetric{Status: status, Message: message, Capabilities: map[string]Capability{
		"latency": {Scope: MetricScopeProcess, Available: false},
		"driver":  {Scope: MetricScopeProcess, Available: false},
		"mode":    {Scope: MetricScopeProcess, Available: false},
		"pool":    {Scope: MetricScopeProcess, Available: false},
	}}
}

func unavailableRedisMetric(status Status, message string) RedisMetric {
	return RedisMetric{Status: status, Message: message, Capabilities: map[string]Capability{
		"latency":  {Scope: MetricScopeProcess, Available: false},
		"mode":     {Scope: MetricScopeProcess, Available: false},
		"pool":     {Scope: MetricScopeProcess, Available: false},
		"pool.max": {Scope: MetricScopeProcess, Available: false},
		"keyspace": {Scope: MetricScopeProcess, Available: false},
	}}
}

func elapsedMilliseconds(started time.Time) float64 {
	return float64(time.Since(started).Microseconds()) / 1000
}
