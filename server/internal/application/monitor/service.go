// Package monitor provides a credential-free, read-only runtime snapshot.
package monitor

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"example.com/gin-vben-admin/server/internal/domain/tenant"
)

var ErrPermissionDenied = errors.New("monitor permission denied")

type Status string

const (
	StatusOK          Status = "ok"
	StatusDegraded    Status = "degraded"
	StatusUnavailable Status = "unavailable"
)

type Probe interface {
	Ping(context.Context) error
}

// StatsProbe is an optional, credential-free extension implemented by local
// database/Redis adapters. The primitive return values keep the application
// layer independent of a particular driver or pool package.
type StatsProbe interface {
	Probe
	RuntimeStats(context.Context) (open, idle, max int, keyspace int64, err error)
}

type Config struct {
	Version  string
	Scope    string
	Start    time.Time
	Clock    func() time.Time
	DiskPath string
	Database Probe
	Redis    Probe
}

type HostMetric struct {
	Status      Status  `json:"status"`
	Cores       int     `json:"cores,omitempty"`
	Load1       float64 `json:"load1,omitempty"`
	UsedBytes   int64   `json:"usedBytes,omitempty"`
	TotalBytes  int64   `json:"totalBytes,omitempty"`
	Utilization float64 `json:"utilization,omitempty"`
	Message     string  `json:"message,omitempty"`
}

type DependencyMetric struct {
	Status    Status  `json:"status"`
	LatencyMS float64 `json:"latencyMs,omitempty"`
	PoolOpen  int     `json:"poolOpen,omitempty"`
	PoolIdle  int     `json:"poolIdle,omitempty"`
	PoolMax   int     `json:"poolMax,omitempty"`
	Keyspace  int64   `json:"keyspace,omitempty"`
	Message   string  `json:"message,omitempty"`
}

type Overview struct {
	Scope         string           `json:"scope"`
	UptimeSeconds float64          `json:"uptimeSeconds"`
	Version       string           `json:"version,omitempty"`
	CPU           HostMetric       `json:"cpu"`
	Memory        HostMetric       `json:"memory"`
	Disk          HostMetric       `json:"disk"`
	Database      DependencyMetric `json:"database"`
	Redis         DependencyMetric `json:"redis"`
	CollectedAt   time.Time        `json:"collectedAt"`
}

type Service struct {
	version  string
	scope    string
	start    time.Time
	clock    func() time.Time
	diskPath string
	database Probe
	redis    Probe
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
	scope := strings.TrimSpace(cfg.Scope)
	if scope == "" {
		scope = "process"
	}
	diskPath := strings.TrimSpace(cfg.DiskPath)
	if diskPath == "" {
		diskPath = "."
	}
	return &Service{version: strings.TrimSpace(cfg.Version), scope: scope, start: start, clock: clock, diskPath: diskPath, database: cfg.Database, redis: cfg.Redis}
}

func (s *Service) Overview(ctx context.Context) (Overview, error) {
	if s == nil {
		return Overview{}, errors.New("monitor service is not initialized")
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return Overview{}, err
	}
	if !scope.PlatformAdmin {
		return Overview{}, ErrPermissionDenied
	}
	now := s.clock().UTC()
	up := now.Sub(s.start)
	if up < 0 {
		up = 0
	}
	overview := Overview{Scope: s.scope, UptimeSeconds: up.Seconds(), Version: s.version, CollectedAt: now, CPU: cpuMetric(), Memory: memoryMetric(), Disk: diskMetric(s.diskPath)}
	overview.Database = probeMetric(ctx, s.database)
	overview.Redis = probeMetric(ctx, s.redis)
	return overview, nil
}

func cpuMetric() HostMetric {
	cores := runtime.NumCPU()
	if cores < 1 {
		cores = 1
	}
	metric := HostMetric{Status: StatusOK, Cores: cores, Message: "process scope"}
	// Linux containers expose a cheap, read-only one-minute load signal. The
	// process-scope fallback remains valid on platforms without /proc.
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) > 0 {
			if load, parseErr := strconv.ParseFloat(fields[0], 64); parseErr == nil {
				metric.Load1 = load
				if cores > 0 {
					metric.Utilization = load / float64(cores)
				}
			}
		}
	}
	return metric
}

func memoryMetric() HostMetric {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	used := int64(stats.Alloc)
	total := int64(stats.Sys)
	if total < used {
		total = used
	}
	utilization := float64(0)
	if total > 0 {
		utilization = float64(used) / float64(total)
	}
	return HostMetric{Status: StatusOK, UsedBytes: used, TotalBytes: total, Utilization: utilization, Message: "process scope"}
}

func diskMetric(path string) HostMetric {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return HostMetric{Status: StatusDegraded, Message: "disk metrics unavailable"}
	}
	total := int64(stat.Blocks) * int64(stat.Bsize)
	free := int64(stat.Bavail) * int64(stat.Bsize)
	used := total - free
	if used < 0 {
		used = 0
	}
	utilization := float64(0)
	if total > 0 {
		utilization = float64(used) / float64(total)
	}
	return HostMetric{Status: StatusOK, UsedBytes: used, TotalBytes: total, Utilization: utilization}
}

func probeMetric(ctx context.Context, probe Probe) DependencyMetric {
	if probe == nil {
		return DependencyMetric{Status: StatusUnavailable, Message: "dependency not configured"}
	}
	started := time.Now()
	if err := probe.Ping(ctx); err != nil {
		return DependencyMetric{Status: StatusDegraded, LatencyMS: float64(time.Since(started).Microseconds()) / 1000, Message: "dependency unavailable"}
	}
	metric := DependencyMetric{Status: StatusOK, LatencyMS: float64(time.Since(started).Microseconds()) / 1000}
	if statsProbe, ok := probe.(StatsProbe); ok {
		open, idle, max, keyspace, err := statsProbe.RuntimeStats(ctx)
		if err != nil {
			metric.Status = StatusDegraded
			metric.Message = "dependency stats unavailable"
		} else {
			metric.PoolOpen, metric.PoolIdle, metric.PoolMax, metric.Keyspace = open, idle, max, keyspace
		}
	}
	return metric
}
