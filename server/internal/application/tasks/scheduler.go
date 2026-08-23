package tasks

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

var ErrSchedulerUnavailable = errors.New("task scheduler unavailable")

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
}

func NewScheduler(definitions *Service, runs *RunService) *Scheduler {
	return &Scheduler{definitions: definitions, runs: runs, clock: time.Now, seen: map[string]struct{}{}}
}

func (s *Scheduler) SetClock(clock func() time.Time) {
	if s != nil && clock != nil {
		s.clock = clock
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
		key := definition.ID + "@" + local.Format("200601021504")
		if s.alreadySeen(key) {
			continue
		}
		if _, enqueueErr := s.runs.Enqueue(ctx, definition.ID, []byte(`{}`), key); enqueueErr != nil {
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
		interval = time.Minute
	}
	if _, err := s.Tick(ctx, s.now()); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case at := <-ticker.C:
			if _, err := s.Tick(ctx, at); err != nil && !errors.Is(err, context.Canceled) {
				return err
			}
		}
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
	s.seen[key] = struct{}{}
	s.mu.Unlock()
}

func (s *Scheduler) now() time.Time {
	if s != nil && s.clock != nil {
		return s.clock().UTC()
	}
	return time.Now().UTC()
}

// cronMatches supports the standard five-field expression plus optional
// seconds and year fields. Lists, ranges and step values are supported; an
// unsupported/malformed expression simply evaluates false.
func cronMatches(expression string, at time.Time) bool {
	fields := strings.Fields(expression)
	if len(fields) < 5 || len(fields) > 7 {
		return false
	}
	offset := 0
	if len(fields) == 6 || len(fields) == 7 {
		if !cronFieldMatches(fields[0], at.Second(), 0, 59) {
			return false
		}
		offset = 1
	}
	if !cronFieldMatches(fields[offset], at.Minute(), 0, 59) ||
		!cronFieldMatches(fields[offset+1], at.Hour(), 0, 23) ||
		!cronFieldMatches(fields[offset+2], at.Day(), 1, 31) ||
		!cronFieldMatches(fields[offset+3], int(at.Month()), 1, 12) {
		return false
	}
	weekday := at.Weekday()
	weekdayValue := int(weekday)
	if !cronFieldMatches(fields[offset+4], weekdayValue, 0, 7) && !(weekdayValue == 0 && cronFieldMatches(fields[offset+4], 7, 0, 7)) {
		return false
	}
	if len(fields) == 7 && !cronFieldMatches(fields[6], at.Year(), 1970, 2199) {
		return false
	}
	return true
}

func cronFieldMatches(expression string, value, minimum, maximum int) bool {
	expression = strings.TrimSpace(expression)
	if expression == "" {
		return false
	}
	for _, part := range strings.Split(expression, ",") {
		part = strings.TrimSpace(part)
		if part == "*" || part == "?" {
			return true
		}
		step := 1
		if strings.Contains(part, "/") {
			pieces := strings.Split(part, "/")
			if len(pieces) != 2 {
				continue
			}
			parsed, err := strconv.Atoi(pieces[1])
			if err != nil || parsed <= 0 {
				continue
			}
			step, part = parsed, pieces[0]
		}
		start, end := minimum, maximum
		if part != "" && part != "*" {
			if strings.Contains(part, "-") {
				pieces := strings.Split(part, "-")
				if len(pieces) != 2 {
					continue
				}
				var err error
				start, err = strconv.Atoi(pieces[0])
				if err != nil {
					continue
				}
				end, err = strconv.Atoi(pieces[1])
				if err != nil {
					continue
				}
			} else {
				var err error
				start, err = strconv.Atoi(part)
				if err != nil {
					continue
				}
				end = start
			}
		}
		if start < minimum || end > maximum || start > end {
			continue
		}
		if value >= start && value <= end && (value-start)%step == 0 {
			return true
		}
	}
	return false
}
