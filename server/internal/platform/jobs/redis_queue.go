// Package jobs contains infrastructure adapters for the application/jobs
// queue seam. RedisQueue deliberately stores only declarative task data and
// status; an Asynq-backed implementation can replace it without changing the
// application service or HTTP contract.
package jobs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	appjobs "example.com/gin-vben-admin/server/internal/application/jobs"
	rediscache "example.com/gin-vben-admin/server/internal/platform/cache/redis"
)

var ErrRedisQueueUnavailable = errors.New("redis task queue is unavailable")

const defaultRetention = 365 * 24 * time.Hour

// RedisQueue is a durable single-node queue adapter. The storage keys are
// namespaced by the configured Redis client, idempotency is guarded by a
// short distributed lock, and payloads remain internal to the worker seam.
type RedisQueue struct {
	cache       *rediscache.Client
	maxAttempts int
	retention   time.Duration
}

func NewRedisQueue(cache *rediscache.Client, maxAttempts int) *RedisQueue {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	return &RedisQueue{cache: cache, maxAttempts: maxAttempts, retention: defaultRetention}
}

// NewAsynqQueue is the migration seam for deployments that select Asynq. It
// currently uses the same versioned Queue contract and Redis storage rules.
func NewAsynqQueue(cache *rediscache.Client, maxAttempts int) *RedisQueue {
	return NewRedisQueue(cache, maxAttempts)
}

func (q *RedisQueue) Enqueue(ctx context.Context, task appjobs.Task) (appjobs.Task, error) {
	if q == nil || q.cache == nil {
		return appjobs.Task{}, ErrRedisQueueUnavailable
	}
	if strings.TrimSpace(task.Type) == "" || task.PayloadVersion <= 0 || strings.TrimSpace(task.IdempotencyKey) == "" {
		return appjobs.Task{}, appjobs.ErrInvalidTask
	}
	lock, err := q.cache.AcquireLock(ctx, "task-enqueue-"+hashSegment(task.IdempotencyKey), 5*time.Second)
	if err != nil {
		return appjobs.Task{}, err
	}
	defer func() { _ = lock.Release(context.Background()) }()
	if existing, getErr := q.lookupByKey(ctx, task.IdempotencyKey); getErr == nil {
		return existing, nil
	}
	task.ID = newID("queue")
	task.Payload = append([]byte(nil), task.Payload...)
	if task.MaxAttempts <= 0 {
		task.MaxAttempts = q.maxAttempts
	}
	task.Status = appjobs.StatusPending
	task.CreatedAt = time.Now().UTC()
	if err := q.saveTask(ctx, task); err != nil {
		return appjobs.Task{}, err
	}
	if err := q.saveKey(ctx, task.IdempotencyKey, task.ID); err != nil {
		_ = q.cache.Delete(context.Background(), q.taskKey(task.ID))
		return appjobs.Task{}, err
	}
	return cloneTask(task), nil
}

func (q *RedisQueue) Get(ctx context.Context, id string) (appjobs.Task, error) {
	if q == nil || q.cache == nil {
		return appjobs.Task{}, ErrRedisQueueUnavailable
	}
	var wire taskWire
	if err := q.cache.GetJSON(ctx, q.taskKey(id), &wire); err != nil {
		if errors.Is(err, rediscache.ErrCacheMiss) {
			return appjobs.Task{}, appjobs.ErrTaskNotFound
		}
		return appjobs.Task{}, err
	}
	task := wire.Task
	task.Payload = append([]byte(nil), wire.Payload...)
	return cloneTask(task), nil
}

func (q *RedisQueue) Fail(ctx context.Context, id string, cause error) error {
	return q.update(ctx, id, func(task *appjobs.Task) error {
		if task.Status == appjobs.StatusDeadLetter || task.Status == appjobs.StatusCancelled || task.Status == appjobs.StatusSucceeded {
			return nil
		}
		task.Attempts++
		if cause != nil {
			task.LastError = stableErrorCode(cause)
		}
		if task.Attempts >= task.MaxAttempts {
			task.Status = appjobs.StatusDeadLetter
		} else {
			task.Status = appjobs.StatusFailed
		}
		return nil
	})
}

func (q *RedisQueue) Complete(ctx context.Context, id string) error {
	return q.update(ctx, id, func(task *appjobs.Task) error {
		if task.Status == appjobs.StatusDeadLetter || task.Status == appjobs.StatusCancelled || task.Status == appjobs.StatusSucceeded {
			return appjobs.ErrTaskConflict
		}
		task.Status = appjobs.StatusSucceeded
		return nil
	})
}

func (q *RedisQueue) Start(ctx context.Context, id string) error {
	return q.update(ctx, id, func(task *appjobs.Task) error {
		if task.Status == appjobs.StatusDeadLetter || task.Status == appjobs.StatusCancelled || task.Status == appjobs.StatusSucceeded {
			return appjobs.ErrTaskConflict
		}
		task.Status = appjobs.StatusRunning
		return nil
	})
}

func (q *RedisQueue) Cancel(ctx context.Context, id string) error {
	return q.update(ctx, id, func(task *appjobs.Task) error {
		if task.Status == appjobs.StatusDeadLetter || task.Status == appjobs.StatusCancelled || task.Status == appjobs.StatusSucceeded {
			return appjobs.ErrTaskConflict
		}
		task.Status = appjobs.StatusCancelled
		return nil
	})
}

func (q *RedisQueue) update(ctx context.Context, id string, mutate func(*appjobs.Task) error) error {
	if q == nil || q.cache == nil {
		return ErrRedisQueueUnavailable
	}
	task, err := q.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := mutate(&task); err != nil {
		return err
	}
	return q.saveTask(ctx, task)
}

func (q *RedisQueue) lookupByKey(ctx context.Context, key string) (appjobs.Task, error) {
	var ref struct {
		ID string `json:"id"`
	}
	if err := q.cache.GetJSON(ctx, q.keyKey(key), &ref); err != nil {
		return appjobs.Task{}, err
	}
	return q.Get(ctx, ref.ID)
}

func (q *RedisQueue) saveKey(ctx context.Context, key, id string) error {
	return q.cache.SetJSON(ctx, q.keyKey(key), struct {
		ID string `json:"id"`
	}{ID: id}, q.retention)
}

func (q *RedisQueue) saveTask(ctx context.Context, task appjobs.Task) error {
	return q.cache.SetJSON(ctx, q.taskKey(task.ID), taskWire{Task: task, Payload: append([]byte(nil), task.Payload...)}, q.retention)
}

type taskWire struct {
	Task    appjobs.Task `json:"task"`
	Payload []byte       `json:"payload,omitempty"`
}

func (w *taskWire) UnmarshalJSON(data []byte) error {
	var raw struct {
		Task    appjobs.Task `json:"task"`
		Payload []byte       `json:"payload"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	w.Task = raw.Task
	w.Payload = append([]byte(nil), raw.Payload...)
	w.Task.Payload = append([]byte(nil), raw.Payload...)
	return nil
}

func (q *RedisQueue) taskKey(id string) string {
	key, _ := q.cache.Key("jobs", "task", hashSegment(id))
	return key
}

func (q *RedisQueue) keyKey(id string) string {
	key, _ := q.cache.Key("jobs", "idempotency", hashSegment(id))
	return key
}

func hashSegment(value string) string {
	// SHA-256 is used only for a safe, deterministic Redis key segment.
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func newID(prefix string) string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return prefix + "-" + time.Now().UTC().Format("20060102150405.000000000")
	}
	return prefix + "-" + hex.EncodeToString(raw[:])
}

func stableErrorCode(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "worker.timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "worker.cancelled"
	}
	return "worker.failed"
}

func cloneTask(task appjobs.Task) appjobs.Task {
	task.Payload = append([]byte(nil), task.Payload...)
	return task
}

var _ appjobs.Queue = (*RedisQueue)(nil)
