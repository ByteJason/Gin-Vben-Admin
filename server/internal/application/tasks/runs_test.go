package tasks

import (
	"context"
	"errors"
	"strings"
	"testing"

	"example.com/gin-vben-admin/server/internal/application/jobs"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
)

func runContext(t *testing.T, tenantID, orgID string) context.Context {
	t.Helper()
	scope, err := tenant.NewContext(tenantID, orgID, false)
	if err != nil {
		t.Fatal(err)
	}
	return tenant.WithContext(context.Background(), scope)
}

func TestRunServiceEnqueuesIdempotentlyAndTracksCancellation(t *testing.T) {
	definitions := NewService(NewMemoryRepository())
	ctx := runContext(t, "tenant-a", "org-a")
	definition, err := definitions.Create(ctx, TaskDefinition{ID: "task-1", Name: "manual", Type: "manual", PayloadSchema: []byte(`{"type":"object"}`), Timezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	queue := jobs.NewMemoryQueue(3)
	runs := NewRunService(definitions, NewMemoryRunRepository(), queue)
	first, err := runs.Enqueue(ctx, definition.ID, []byte(`{"value":1}`), "run-key-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := runs.Enqueue(ctx, definition.ID, []byte(`{"value":1}`), "run-key-1")
	if err != nil || first.ID != second.ID {
		t.Fatalf("idempotency first=%+v second=%+v err=%v", first, second, err)
	}
	cancelled, err := runs.Cancel(ctx, definition.ID, first.ID)
	if err != nil || cancelled.Status != RunCancelled {
		t.Fatalf("cancelled=%+v err=%v", cancelled, err)
	}
	if _, err := runs.Cancel(runContext(t, "other", ""), definition.ID, first.ID); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("cross tenant cancel error=%v", err)
	}
}

func TestRunServiceRejectsNonObjectPayloadAndRetriesFailedRun(t *testing.T) {
	definitions := NewService(NewMemoryRepository())
	ctx := runContext(t, "tenant-a", "")
	definition, err := definitions.Create(ctx, TaskDefinition{ID: "task-2", Name: "manual", Type: "manual", PayloadSchema: []byte(`{"type":"object"}`), Timezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	runService := NewRunService(definitions, NewMemoryRunRepository(), jobs.NewMemoryQueue(2))
	if _, err := runService.Enqueue(ctx, definition.ID, []byte(`[]`), "bad"); !errors.Is(err, ErrInvalidRunPayload) {
		t.Fatalf("payload error=%v", err)
	}
	run, err := runService.Enqueue(ctx, definition.ID, []byte(`{}`), "retry-key")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runService.MarkFailed(ctx, run.ID, "provider.unavailable"); err != nil {
		t.Fatal(err)
	}
	retried, err := runService.Retry(ctx, definition.ID, run.ID)
	if err != nil || retried.Status != RunPending || retried.AttemptCount != 1 {
		t.Fatalf("retried=%+v err=%v", retried, err)
	}
}

func TestRunServiceBindsWorkerAndPersistsStableAttemptLogs(t *testing.T) {
	definitions := NewService(NewMemoryRepository())
	ctx := runContext(t, "tenant-worker", "org-worker")
	definition, err := definitions.Create(ctx, TaskDefinition{Name: "worker", Type: "manual", PayloadSchema: []byte(`{"type":"object"}`), Timezone: "UTC", MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	queue := jobs.NewMemoryQueue(2)
	repo := NewMemoryRunRepository()
	runs := NewRunService(definitions, repo, queue)
	run, err := runs.Enqueue(ctx, definition.ID, []byte(`{"ok":true}`), "worker-key")
	if err != nil {
		t.Fatal(err)
	}
	worker := jobs.NewWorker(queue, jobs.WorkerOptions{})
	if err := runs.BindWorker(worker, "manual", func(context.Context, jobs.Task) error { return errors.New("provider detail must not be logged") }); err != nil {
		t.Fatal(err)
	}
	if err := worker.Execute(context.Background(), run.QueueTaskID); err == nil {
		t.Fatal("expected first attempt failure")
	}
	failed, err := runs.Get(ctx, run.ID)
	if err != nil || failed.Status != RunFailed || failed.LastErrorCode != "worker.failed" {
		t.Fatalf("failed=%+v err=%v", failed, err)
	}
	logs, err := runs.Logs(ctx, definition.ID, run.ID)
	if err != nil || len(logs) < 2 {
		t.Fatalf("logs=%+v err=%v", logs, err)
	}
	for _, entry := range logs {
		if strings.Contains(entry.Message, "provider detail") {
			t.Fatalf("raw handler error leaked into log: %+v", entry)
		}
	}
}

func TestRunServiceWorkerCancellationMarksRunCancelled(t *testing.T) {
	definitions := NewService(NewMemoryRepository())
	ctx := runContext(t, "tenant-cancel", "")
	definition, err := definitions.Create(ctx, TaskDefinition{Name: "cancel worker", Type: "manual", PayloadSchema: []byte(`{"type":"object"}`), Timezone: "UTC"})
	if err != nil {
		t.Fatal(err)
	}
	queue := jobs.NewMemoryQueue(2)
	runs := NewRunService(definitions, NewMemoryRunRepository(), queue)
	run, err := runs.Enqueue(ctx, definition.ID, []byte(`{}`), "cancel-worker-key")
	if err != nil {
		t.Fatal(err)
	}
	worker := jobs.NewWorker(queue, jobs.WorkerOptions{})
	cancelled, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	if err := runs.BindWorker(worker, "manual", func(handlerCtx context.Context, _ jobs.Task) error {
		close(started)
		<-handlerCtx.Done()
		return handlerCtx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	go func() { <-started; cancel() }()
	if err := worker.Execute(cancelled, run.QueueTaskID); err == nil {
		t.Fatal("expected cancellation")
	}
	got, err := runs.Get(ctx, run.ID)
	if err != nil || got.Status != RunCancelled {
		t.Fatalf("run=%+v err=%v", got, err)
	}
}
