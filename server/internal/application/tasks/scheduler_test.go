package tasks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/application/jobs"
)

func TestSchedulerEnqueuesDueCronInDefinitionTimezoneAndIsIdempotent(t *testing.T) {
	ctx := runContext(t, "tenant-cron", "org-cron")
	definitions := NewService(NewMemoryRepository())
	due, err := definitions.Create(ctx, TaskDefinition{Name: "every minute", Type: "manual", Cron: "* * * * *", Timezone: "Asia/Shanghai", PayloadSchema: []byte(`{"type":"object"}`), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := definitions.Create(ctx, TaskDefinition{Name: "disabled", Type: "manual", Cron: "* * * * *", Timezone: "UTC", PayloadSchema: []byte(`{"type":"object"}`), Enabled: false}); err != nil {
		t.Fatal(err)
	}
	queue := jobs.NewMemoryQueue(3)
	runs := NewRunService(definitions, NewMemoryRunRepository(), queue)
	scheduler := NewScheduler(definitions, runs)
	at := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC) // 08:00 in Shanghai
	count, err := scheduler.Tick(ctx, at)
	if err != nil || count != 1 {
		t.Fatalf("first tick count=%d err=%v", count, err)
	}
	count, err = scheduler.Tick(ctx, at)
	if err != nil || count != 0 {
		t.Fatalf("duplicate tick count=%d err=%v", count, err)
	}
	items, err := runs.List(ctx, due.ID)
	if err != nil || len(items) != 1 {
		t.Fatalf("runs=%+v err=%v", items, err)
	}
}

func TestCronMatchesSupportsStepsAndRejectsMalformedFields(t *testing.T) {
	loc, _ := time.LoadLocation("UTC")
	if !cronMatches("*/15 * * * *", time.Date(2026, time.January, 1, 1, 30, 0, 0, loc)) {
		t.Fatal("step expression should match")
	}
	if cronMatches("bad cron", time.Now().UTC()) {
		t.Fatal("malformed expression should not match")
	}
}

func TestSchedulerSupportsSecondsAndAliasSchedules(t *testing.T) {
	ctx := runContext(t, "tenant-cron-seconds", "org-cron-seconds")
	definitions := NewService(NewMemoryRepository())
	seconds, err := definitions.Create(ctx, TaskDefinition{Name: "every five seconds", Type: "manual", Cron: "*/5 * * * * *", Timezone: "UTC", PayloadSchema: []byte(`{"type":"object"}`), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	queue := jobs.NewMemoryQueue(4)
	runRepo := NewMemoryRunRepository()
	runs := NewRunService(definitions, runRepo, queue)
	scheduler := NewScheduler(definitions, runs)
	at := time.Date(2026, time.January, 1, 0, 0, 5, 0, time.UTC)
	if count, err := scheduler.Tick(ctx, at); err != nil || count != 1 {
		t.Fatalf("seconds tick count=%d err=%v", count, err)
	}
	// The same instant is idempotent, including the seconds component in its key.
	if count, err := scheduler.Tick(ctx, at); err != nil || count != 0 {
		t.Fatalf("seconds duplicate count=%d err=%v", count, err)
	}
	if runs, err := runs.List(ctx, seconds.ID); err != nil || len(runs) != 1 {
		t.Fatalf("seconds runs=%+v err=%v", runs, err)
	}
}

func TestSchedulerRunReportsTransientErrorsAndKeepsPolling(t *testing.T) {
	scheduler := NewScheduler(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reported := make(chan struct{}, 4)
	scheduler.SetErrorHandler(func(err error) {
		if errors.Is(err, ErrSchedulerUnavailable) {
			select {
			case reported <- struct{}{}:
			default:
			}
			if len(reported) >= 2 {
				cancel()
			}
		}
	})
	done := make(chan error, 1)
	go func() { done <- scheduler.Run(ctx, 2*time.Millisecond) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error=%v, want context cancellation", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not keep polling and observe cancellation")
	}
	if got := len(reported); got < 2 {
		t.Fatalf("transient errors reported=%d, want at least two polling attempts", got)
	}
}

func TestSchedulerScopeSourceSchedulesAllEnabledTenants(t *testing.T) {
	definitions := NewService(NewMemoryRepository())
	for _, scope := range []struct{ tenant, org string }{{"tenant-a", "org-a"}, {"tenant-b", "org-b"}} {
		ctx := runContext(t, scope.tenant, scope.org)
		if _, err := definitions.Create(ctx, TaskDefinition{Name: "scope task " + scope.tenant, Type: "manual", Cron: "* * * * *", Timezone: "UTC", PayloadSchema: []byte(`{"type":"object"}`), Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	queue := jobs.NewMemoryQueue(4)
	runRepo := NewMemoryRunRepository()
	runs := NewRunService(definitions, runRepo, queue)
	scheduler := NewScheduler(definitions, runs)
	scheduler.SetScopeSource(definitions.repo.(*MemoryRepository))
	at := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	count, err := scheduler.tickScopes(context.Background(), at)
	if err != nil || count != 2 {
		t.Fatalf("scoped tick count=%d err=%v", count, err)
	}
	for _, scope := range []struct{ tenant, org string }{{"tenant-a", "org-a"}, {"tenant-b", "org-b"}} {
		ctx := runContext(t, scope.tenant, scope.org)
		items, err := runRepo.List(ctx, "", scope.tenant, scope.org)
		if err != nil || len(items) != 1 {
			t.Fatalf("scope %s/%s runs=%+v err=%v", scope.tenant, scope.org, items, err)
		}
	}
}
