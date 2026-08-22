package jobs

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryQueueIsIdempotentAndMovesFailedTaskToDLQ(t *testing.T) {
	queue := NewMemoryQueue(2)
	task := Task{Type: "email.send", PayloadVersion: 1, IdempotencyKey: "mail-1", Payload: []byte(`{"to":"u@example.com"}`)}
	first, err := queue.Enqueue(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	second, err := queue.Enqueue(context.Background(), task)
	if err != nil || first.ID != second.ID {
		t.Fatalf("idempotency first=%+v second=%+v err=%v", first, second, err)
	}
	if err := queue.Fail(context.Background(), first.ID, errors.New("provider down")); err != nil {
		t.Fatal(err)
	}
	if err := queue.Fail(context.Background(), first.ID, errors.New("provider down")); err != nil {
		t.Fatal(err)
	}
	failed, err := queue.Get(context.Background(), first.ID)
	if err != nil || failed.Status != StatusDeadLetter {
		t.Fatalf("failed=%+v err=%v", failed, err)
	}
}

func TestMemoryQueueHonorsPerTaskAttemptLimit(t *testing.T) {
	q := NewMemoryQueue(5)
	task, err := q.Enqueue(context.Background(), Task{Type: "manual", PayloadVersion: 1, IdempotencyKey: "per-task", MaxAttempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	if task.MaxAttempts != 1 {
		t.Fatalf("max attempts=%d", task.MaxAttempts)
	}
	if err := q.Fail(context.Background(), task.ID, errors.New("temporary")); err != nil {
		t.Fatal(err)
	}
	got, _ := q.Get(context.Background(), task.ID)
	if got.Status != StatusDeadLetter {
		t.Fatalf("got=%+v", got)
	}
}

func TestWorkerExecutesRegisteredHandlerWithTimeoutAndRetries(t *testing.T) {
	q := NewMemoryQueue(2)
	task, _ := q.Enqueue(context.Background(), Task{Type: "email.send", PayloadVersion: 1, IdempotencyKey: "w-1"})
	w := NewWorker(q, WorkerOptions{Timeout: 20 * time.Millisecond, Concurrency: 1})
	called := 0
	w.Register("email.send", func(ctx context.Context, _ Task) error {
		called++
		if called == 1 {
			return errors.New("temporary")
		}
		return nil
	})
	_ = w.Execute(context.Background(), task.ID)
	if err := w.Execute(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := q.Get(context.Background(), task.ID)
	if got.Status != StatusSucceeded || got.Attempts != 1 {
		t.Fatalf("got=%+v", got)
	}
}

func TestWorkerRejectsUnregisteredAndHonorsCancellation(t *testing.T) {
	q := NewMemoryQueue(2)
	task, _ := q.Enqueue(context.Background(), Task{Type: "unknown", PayloadVersion: 1, IdempotencyKey: "w-2"})
	w := NewWorker(q, WorkerOptions{})
	if err := w.Execute(context.Background(), task.ID); !errors.Is(err, ErrHandlerNotFound) {
		t.Fatalf("err=%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.Execute(ctx, task.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestWorkerRejectsMissingQueueAndQueueKeepsTerminalState(t *testing.T) {
	var worker *Worker
	if !errors.Is(worker.Execute(context.Background(), "missing"), ErrWorkerUnavailable) {
		t.Fatal("nil worker should report an unavailable queue")
	}
	q := NewMemoryQueue(1)
	task, err := q.Enqueue(context.Background(), Task{Type: "manual", PayloadVersion: 1, IdempotencyKey: "terminal-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Complete(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	if err := q.Cancel(context.Background(), task.ID); !errors.Is(err, ErrTaskConflict) {
		t.Fatalf("cancel after complete error = %v", err)
	}
}

func TestMemoryQueueExposesRunningState(t *testing.T) {
	q := NewMemoryQueue(2)
	task, err := q.Enqueue(context.Background(), Task{Type: "manual", PayloadVersion: 1, IdempotencyKey: "running-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Start(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}
	got, err := q.Get(context.Background(), task.ID)
	if err != nil || got.Status != StatusRunning {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestWorkerTimeoutRecordsFailure(t *testing.T) {
	q := NewMemoryQueue(2)
	task, _ := q.Enqueue(context.Background(), Task{Type: "slow", PayloadVersion: 1, IdempotencyKey: "w-timeout"})
	w := NewWorker(q, WorkerOptions{Timeout: time.Millisecond})
	w.Register("slow", func(ctx context.Context, _ Task) error { <-ctx.Done(); return ctx.Err() })
	if err := w.Execute(context.Background(), task.ID); err == nil {
		t.Fatal("expected timeout error")
	}
	got, _ := q.Get(context.Background(), task.ID)
	if got.Status != StatusFailed || got.Attempts != 1 {
		t.Fatalf("got=%+v", got)
	}
}
