// Package jobs defines the versioned task queue seam. A future Asynq adapter
// can implement Queue; MemoryQueue makes idempotency/retry/DLQ behavior testable
// without introducing a worker or broker in this release.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidTask  = errors.New("invalid task")
	ErrTaskNotFound = errors.New("task not found")
	ErrTaskConflict = errors.New("task idempotency conflict")
	ErrQueueEmpty   = errors.New("task queue is empty")
	ErrClaimLost    = errors.New("task claim lease lost")
)

type Status string

const (
	StatusPending    Status = "pending"
	StatusRunning    Status = "running"
	StatusSucceeded  Status = "succeeded"
	StatusFailed     Status = "failed"
	StatusDeadLetter Status = "dead_letter"
	StatusCancelled  Status = "cancelled"
)

type Task struct {
	ID             string    `json:"id"`
	Type           string    `json:"type"`
	PayloadVersion int       `json:"payloadVersion"`
	IdempotencyKey string    `json:"idempotencyKey"`
	Payload        []byte    `json:"-"`
	Attempts       int       `json:"attempts"`
	MaxAttempts    int       `json:"maxAttempts"`
	Status         Status    `json:"status"`
	LastError      string    `json:"lastError,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

type Queue interface {
	Enqueue(context.Context, Task) (Task, error)
	Get(context.Context, string) (Task, error)
	Fail(context.Context, string, error) error
	Complete(context.Context, string) error
	Cancel(context.Context, string) error
}

// PendingQueue is the optional worker-facing dequeue capability. ClaimPending
// atomically reserves one pending/retryable task so multiple workers cannot
// execute the same queue item concurrently.
type PendingQueue interface {
	ClaimPending(context.Context) (Task, error)
}

// RunningLeaseQueue renews a distributed claim while a handler is executing.
// A failed renewal means ownership was lost and the handler context must stop.
type RunningLeaseQueue interface {
	RenewClaim(context.Context, string) error
}

// RunningQueue is an optional capability used by workers that expose an
// explicit in-progress state. Queue adapters that do not implement it remain
// compatible with the base contract.
type RunningQueue interface {
	Start(context.Context, string) error
}

type MemoryQueue struct {
	mu            sync.RWMutex
	maxAttempts   int
	tasks         map[string]Task
	byIdempotency map[string]string
}

func NewMemoryQueue(maxAttempts int) *MemoryQueue {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	return &MemoryQueue{maxAttempts: maxAttempts, tasks: map[string]Task{}, byIdempotency: map[string]string{}}
}

func (q *MemoryQueue) Enqueue(_ context.Context, task Task) (Task, error) {
	if q == nil || strings.TrimSpace(task.Type) == "" || task.PayloadVersion <= 0 || strings.TrimSpace(task.IdempotencyKey) == "" {
		return Task{}, ErrInvalidTask
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if existingID, ok := q.byIdempotency[task.IdempotencyKey]; ok {
		return clone(q.tasks[existingID]), nil
	}
	if strings.TrimSpace(task.ID) == "" {
		id, err := taskID()
		if err != nil {
			return Task{}, err
		}
		task.ID = id
	} else if existing, exists := q.tasks[task.ID]; exists {
		if existing.IdempotencyKey == task.IdempotencyKey {
			return clone(existing), nil
		}
		return Task{}, ErrTaskConflict
	}
	task.Payload = append([]byte(nil), task.Payload...)
	if task.MaxAttempts <= 0 {
		task.MaxAttempts = q.maxAttempts
	}
	task.Status = StatusPending
	task.CreatedAt = time.Now().UTC()
	q.tasks[task.ID] = task
	q.byIdempotency[task.IdempotencyKey] = task.ID
	return clone(task), nil
}

func (q *MemoryQueue) ClaimPending(_ context.Context) (Task, error) {
	if q == nil {
		return Task{}, ErrQueueEmpty
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	for id, task := range q.tasks {
		if task.Status != StatusPending && task.Status != StatusFailed {
			continue
		}
		task.Status = StatusRunning
		q.tasks[id] = task
		return clone(task), nil
	}
	return Task{}, ErrQueueEmpty
}

func (q *MemoryQueue) Get(_ context.Context, id string) (Task, error) {
	if q == nil {
		return Task{}, ErrTaskNotFound
	}
	q.mu.RLock()
	defer q.mu.RUnlock()
	task, ok := q.tasks[id]
	if !ok {
		return Task{}, ErrTaskNotFound
	}
	return clone(task), nil
}

func (q *MemoryQueue) Fail(_ context.Context, id string, cause error) error {
	if q == nil {
		return ErrTaskNotFound
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	task, ok := q.tasks[id]
	if !ok {
		return ErrTaskNotFound
	}
	if task.Status == StatusDeadLetter || task.Status == StatusCancelled || task.Status == StatusSucceeded {
		return nil
	}
	task.Attempts++
	if cause != nil {
		switch {
		case errors.Is(cause, context.DeadlineExceeded):
			task.LastError = "worker.timeout"
		case errors.Is(cause, context.Canceled):
			task.LastError = "worker.cancelled"
		default:
			task.LastError = "worker.failed"
		}
	}
	if task.Attempts >= task.MaxAttempts {
		task.Status = StatusDeadLetter
	} else {
		task.Status = StatusFailed
	}
	q.tasks[id] = task
	return nil
}

func (q *MemoryQueue) Complete(_ context.Context, id string) error {
	return q.transition(id, StatusSucceeded)
}

func (q *MemoryQueue) Start(_ context.Context, id string) error {
	if q == nil {
		return ErrTaskNotFound
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	task, ok := q.tasks[id]
	if !ok {
		return ErrTaskNotFound
	}
	if task.Status == StatusDeadLetter || task.Status == StatusCancelled || task.Status == StatusSucceeded {
		return ErrTaskConflict
	}
	task.Status = StatusRunning
	q.tasks[id] = task
	return nil
}

func (q *MemoryQueue) Cancel(_ context.Context, id string) error {
	return q.transition(id, StatusCancelled)
}

func (q *MemoryQueue) transition(id string, status Status) error {
	if q == nil {
		return ErrTaskNotFound
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	task, ok := q.tasks[id]
	if !ok {
		return ErrTaskNotFound
	}
	if task.Status == StatusDeadLetter || task.Status == StatusCancelled || task.Status == StatusSucceeded {
		return ErrTaskConflict
	}
	task.Status = status
	q.tasks[id] = task
	return nil
}

func clone(task Task) Task {
	task.Payload = append([]byte(nil), task.Payload...)
	return task
}

func taskID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

var _ Queue = (*MemoryQueue)(nil)
var _ PendingQueue = (*MemoryQueue)(nil)
