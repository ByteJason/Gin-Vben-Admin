package tasks

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	taskdomain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/task"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

var ErrSchedulerUnavailable = errors.New("task scheduler unavailable")

// LeaderLease optionally coordinates scheduler ticks across processes (for example Redis).
type LeaderLease interface {
	Acquire(context.Context, string, time.Duration) (release func() error, acquired bool, err error)
}

// ScopeSource supplies tenant scopes for a background scheduler. Implementations
// must return only real, non-deleted task-definition scopes; the scheduler then
// reuses Tick's tenant boundary for every scope.
type ScopeSource interface {
	TaskScopes(context.Context) ([]tenant.Context, error)
}

// Scheduler evaluates persisted cron declarations and enqueues one
// idempotent run per task/minute. It is intentionally a small single-process
// seam; a distributed scheduler can retain the same Tick contract and use a
// Redis lock before invoking it.
type Scheduler struct {
	definitions *Service
	runs        *RunService
	clock       func() time.Time
	mu          sync.Mutex
	seen        map[string]struct{}
	lease       LeaderLease
	onError     func(error)
	scopes      ScopeSource
}

func NewScheduler(definitions *Service, runs *RunService) *Scheduler {
	return &Scheduler{definitions: definitions, runs: runs, clock: time.Now, seen: map[string]struct{}{}}
}

func (s *Scheduler) SetLeaderLease(lease LeaderLease) {
	if s != nil {
		s.lease = lease
	}
}

func (s *Scheduler) SetClock(clock func() time.Time) {
	if s != nil && clock != nil {
		s.clock = clock
	}
}

// SetErrorHandler installs a best-effort observer for transient tick failures.
// Run keeps polling after a failed tick; the observer is the integration seam
// for metrics/logging without making scheduling dependent on a logger package.
func (s *Scheduler) SetErrorHandler(handler func(error)) {
	if s != nil {
		s.onError = handler
	}
}

func (s *Scheduler) SetScopeSource(source ScopeSource) {
	if s != nil {
		s.scopes = source
	}
}

// Tick evaluates all enabled definitions in the current tenant scope. The
// payload is an empty object, and the generated key makes repeated ticks safe.
func (s *Scheduler) Tick(ctx context.Context, at time.Time) (int, error) {
	if s == nil || s.definitions == nil || s.runs == nil {
		return 0, ErrSchedulerUnavailable
	}
	if _, err := tenant.RequireContext(ctx); err != nil {
		return 0, err
	}
	if s.lease != nil {
		release, acquired, leaseErr := s.lease.Acquire(ctx, "task-scheduler", 30*time.Second)
		if leaseErr != nil {
			return 0, leaseErr
		}
		if !acquired {
			return 0, nil
		}
		defer func() {
			if release != nil {
				_ = release()
			}
		}()
	}
	if at.IsZero() {
		at = s.now()
	}
	definitions, err := s.definitions.List(ctx)
	if err != nil {
		return 0, err
	}
	queued := 0
	for _, definition := range definitions {
		if !definition.Enabled || strings.TrimSpace(definition.Cron) == "" {
			continue
		}
		location, locationErr := time.LoadLocation(definition.Timezone)
		if locationErr != nil || !cronMatches(definition.Cron, at.In(location)) {
			continue
		}
		local := at.In(location)
		key := definition.ID + "@" + local.UTC().Format("200601021504")
		if taskdomain.HasSeconds(definition.Cron) {
			key = definition.ID + "@" + local.UTC().Format("20060102150405")
		}
		if s.alreadySeen(key) {
			continue
		}
		if _, enqueueErr := s.runs.EnqueueWithSource(tenant.WithContext(ctx, tenant.Context{TenantID: definition.TenantID, Organization: definition.OrgID}), definition.ID, nil, key, "schedule"); enqueueErr != nil {
			return queued, enqueueErr
		}
		s.markSeen(key)
		queued++
	}
	return queued, nil
}

// Run performs an immediate tick followed by bounded polling until context
// cancellation. The caller supplies a tenant scope in ctx.
func (s *Scheduler) Run(ctx context.Context, interval time.Duration) error {
	if s == nil {
		return ErrSchedulerUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if interval <= 0 {
		interval = time.Second
	}
	if _, err := s.tickScopes(ctx, s.now()); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		s.reportError(err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case at := <-ticker.C:
			if _, err := s.tickScopes(ctx, at); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return err
				}
				s.reportError(err)
			}
		}
	}
}

func (s *Scheduler) tickScopes(ctx context.Context, at time.Time) (int, error) {
	if s.scopes == nil {
		return s.Tick(ctx, at)
	}
	scopes, err := s.scopes.TaskScopes(ctx)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, scope := range scopes {
		count, tickErr := s.Tick(tenant.WithContext(ctx, scope), at)
		total += count
		if tickErr != nil {
			return total, tickErr
		}
	}
	return total, nil
}

func (s *Scheduler) reportError(err error) {
	if s != nil && s.onError != nil && err != nil {
		s.onError(err)
	}
}

func (s *Scheduler) alreadySeen(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.seen[key]
	return ok
}

func (s *Scheduler) markSeen(key string) {
	s.mu.Lock()
	if len(s.seen) > 10000 {
		s.seen = map[string]struct{}{}
	}
	s.seen[key] = struct{}{}
	s.mu.Unlock()
}

func (s *Scheduler) now() time.Time {
	if s != nil && s.clock != nil {
		return s.clock().UTC()
	}
	return time.Now().UTC()
}

// cronMatches shares the same parser as validation and preview.
func cronMatches(expression string, at time.Time) bool { return taskdomain.MatchesCron(expression, at) }
