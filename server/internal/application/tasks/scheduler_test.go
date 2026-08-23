package tasks

import (
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
