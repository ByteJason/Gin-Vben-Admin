package jobs

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrHandlerNotFound   = errors.New("task handler not registered")
	ErrWorkerUnavailable = errors.New("task worker is not configured")
)

// Handler executes one queued task. Returning an error makes the task retryable.
type Handler func(context.Context, Task) error

type WorkerOptions struct {
	Timeout     time.Duration
	Concurrency int
}

type Worker struct {
	queue    Queue
	mu       sync.RWMutex
	handlers map[string]Handler
	inFlight map[string]struct{}
	sem      chan struct{}
	timeout  time.Duration
}

func NewWorker(queue Queue, options WorkerOptions) *Worker {
	if options.Concurrency <= 0 {
		options.Concurrency = 1
	}
	return &Worker{queue: queue, handlers: map[string]Handler{}, inFlight: map[string]struct{}{}, sem: make(chan struct{}, options.Concurrency), timeout: options.Timeout}
}

func (w *Worker) Register(taskType string, handler Handler) error {
	if w == nil {
		return ErrWorkerUnavailable
	}
	taskType = strings.TrimSpace(taskType)
	if taskType == "" || handler == nil {
		return ErrInvalidTask
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.handlers[taskType] = handler
	return nil
}

func (w *Worker) Execute(ctx context.Context, id string) error {
	if w == nil || w.queue == nil {
		return ErrWorkerUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		_ = w.queue.Cancel(context.Background(), id)
		return err
	}
	task, err := w.queue.Get(ctx, id)
	if err != nil {
		return err
	}
	w.mu.RLock()
	handler, ok := w.handlers[task.Type]
	w.mu.RUnlock()
	if !ok {
		return ErrHandlerNotFound
	}
	w.mu.Lock()
	if _, exists := w.inFlight[id]; exists {
		w.mu.Unlock()
		return nil
	}
	w.inFlight[id] = struct{}{}
	w.mu.Unlock()
	defer func() { w.mu.Lock(); delete(w.inFlight, id); w.mu.Unlock() }()
	select {
	case w.sem <- struct{}{}:
	case <-ctx.Done():
		w.mu.Lock()
		delete(w.inFlight, id)
		w.mu.Unlock()
		_ = w.queue.Cancel(context.Background(), id)
		return ctx.Err()
	}
	defer func() { <-w.sem }()
	if task.Status == StatusSucceeded || task.Status == StatusDeadLetter || task.Status == StatusCancelled {
		return nil
	}
	if starter, ok := w.queue.(RunningQueue); ok {
		if err := starter.Start(context.Background(), id); err != nil && !errors.Is(err, ErrTaskConflict) {
			return err
		}
	}
	callCtx := ctx
	var cancel context.CancelFunc
	if w.timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, w.timeout)
		defer cancel()
	}
	err = handler(callCtx, task)
	if err == nil {
		return w.queue.Complete(context.Background(), id)
	}
	if errors.Is(callCtx.Err(), context.Canceled) && ctx.Err() != nil {
		_ = w.queue.Cancel(context.Background(), id)
		return ctx.Err()
	}
	returnErr := w.queue.Fail(context.Background(), id, err)
	if returnErr != nil {
		return returnErr
	}
	return err
}

// RunOnce executes the first pending or retryable task available in MemoryQueue.
func (w *Worker) RunOnce(ctx context.Context) error {
	if w == nil || w.queue == nil {
		return ErrWorkerUnavailable
	}
	if q, ok := w.queue.(*MemoryQueue); ok {
		for _, task := range q.snapshot() {
			if task.Status == StatusPending || task.Status == StatusFailed {
				return w.Execute(ctx, task.ID)
			}
		}
		return nil
	}
	return nil
}

func (w *Worker) Run(ctx context.Context, interval time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if interval <= 0 {
		interval = 10 * time.Millisecond
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		_ = w.RunOnce(ctx)
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (q *MemoryQueue) snapshot() []Task {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := make([]Task, 0, len(q.tasks))
	for _, task := range q.tasks {
		out = append(out, clone(task))
	}
	return out
}
