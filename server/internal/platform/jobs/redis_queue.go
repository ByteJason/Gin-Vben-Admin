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
	"sync"
	"time"

	appjobs "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/jobs"
	rediscache "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/cache/redis"
)

var ErrRedisQueueUnavailable = errors.New("redis task queue is unavailable")

const defaultRetention = 365 * 24 * time.Hour
const claimLease = 30 * time.Second

// RedisQueue is a durable single-node queue adapter. Pending IDs are moved
// atomically into a processing list before a worker claims them; a short lease
// suppresses duplicate claims while work is active and expires to make crashed
// workers recoverable. Completion/failure/cancellation removes the processing
// entry, while retryable failures are appended back to pending. The storage
// keys are namespaced by the configured Redis client, idempotency is guarded
// by a short distributed lock, and payloads remain internal to the worker seam.
type RedisQueue struct {
	cache       *rediscache.Client
	maxAttempts int
	retention   time.Duration
	claimsMu    sync.Mutex
	claims      map[string]string
}

func NewRedisQueue(cache *rediscache.Client, maxAttempts int) *RedisQueue {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	return &RedisQueue{cache: cache, maxAttempts: maxAttempts, retention: defaultRetention, claims: make(map[string]string)}
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
	if strings.TrimSpace(task.ID) == "" {
		task.ID = newID("queue")
	} else if existing, getErr := q.Get(ctx, task.ID); getErr == nil {
		if existing.IdempotencyKey == task.IdempotencyKey {
			return existing, nil
		}
		return appjobs.Task{}, appjobs.ErrTaskConflict
	} else if !errors.Is(getErr, appjobs.ErrTaskNotFound) {
		return appjobs.Task{}, getErr
	}
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
	if err := q.cache.PushString(ctx, q.pendingKey(), task.ID, q.retention); err != nil {
		_ = q.cache.Delete(context.Background(), q.taskKey(task.ID))
		return appjobs.Task{}, err
	}
	return cloneTask(task), nil
}

// ClaimPending atomically reserves one pending/retryable task for a worker.
// The list pop chooses a single candidate; a short distributed lock protects
// the status transition when multiple workers share the same Redis queue.
func (q *RedisQueue) ClaimPending(ctx context.Context) (appjobs.Task, error) {
	if q == nil || q.cache == nil {
		return appjobs.Task{}, ErrRedisQueueUnavailable
	}
	for attempts := 0; attempts < 16; attempts++ {
		id, moveErr := q.cache.MoveList(ctx, q.pendingKey(), q.processingKey())
		fromPending := moveErr == nil
		if moveErr != nil && !errors.Is(moveErr, rediscache.ErrCacheMiss) {
			return appjobs.Task{}, moveErr
		}
		if !fromPending {
			id, moveErr = q.cache.MoveList(ctx, q.processingKey(), q.processingKey())
		}
		if errors.Is(moveErr, rediscache.ErrCacheMiss) {
			if !fromPending {
				return appjobs.Task{}, appjobs.ErrQueueEmpty
			}
			continue
		}
		if moveErr != nil {
			return appjobs.Task{}, moveErr
		}
		_, leaseErr := q.cache.GetString(ctx, q.leaseKey(id))
		if leaseErr == nil {
			continue
		} else if !errors.Is(leaseErr, rediscache.ErrCacheMiss) {
			return appjobs.Task{}, leaseErr
		}
		lock, lockErr := q.cache.AcquireLock(ctx, "task-claim-"+hashSegment(id), 5*time.Second)
		if errors.Is(lockErr, rediscache.ErrLockNotAcquired) {
			return appjobs.Task{}, lockErr
		}
		if lockErr != nil {
			return appjobs.Task{}, lockErr
		}
		// Re-check after acquiring the claim lock. Another worker may have
		// installed a lease between the initial probe and lock acquisition.
		if _, leaseErr := q.cache.GetString(ctx, q.leaseKey(id)); leaseErr == nil {
			_ = lock.Release(context.Background())
			continue
		} else if !errors.Is(leaseErr, rediscache.ErrCacheMiss) {
			_ = lock.Release(context.Background())
			return appjobs.Task{}, leaseErr
		}
		task, getErr := q.Get(ctx, id)
		if getErr == nil && (task.Status == appjobs.StatusPending || task.Status == appjobs.StatusFailed || task.Status == appjobs.StatusRunning) {
			task.Status = appjobs.StatusRunning
			owner := newID("claim")
			getErr = q.cache.SetString(ctx, q.leaseKey(id), owner, claimLease)
			if getErr == nil {
				getErr = q.saveTask(ctx, task)
			}
			if getErr == nil {
				q.claimsMu.Lock()
				q.claims[id] = owner
				q.claimsMu.Unlock()
			}
		}
		_ = lock.Release(context.Background())
		if errors.Is(getErr, appjobs.ErrTaskNotFound) {
			continue
		}
		if getErr != nil {
			if task.ID != "" {
				_ = q.cache.Delete(context.Background(), q.leaseKey(id))
			}
			return appjobs.Task{}, getErr
		}
		if task.Status != appjobs.StatusRunning {
			if task.Status == appjobs.StatusSucceeded || task.Status == appjobs.StatusDeadLetter || task.Status == appjobs.StatusCancelled {
				_ = q.releaseProcessing(context.Background(), id)
			}
			continue
		}
		return task, nil
	}
	return appjobs.Task{}, appjobs.ErrQueueEmpty
}

func (q *RedisQueue) RenewClaim(ctx context.Context, id string) error {
	if q == nil || q.cache == nil {
		return ErrRedisQueueUnavailable
	}
	q.claimsMu.Lock()
	owner := q.claims[id]
	q.claimsMu.Unlock()
	if owner == "" {
		return rediscache.ErrLockNotAcquired
	}
	return q.cache.RenewString(ctx, q.leaseKey(id), owner, claimLease)
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
	if err := q.ensureClaimOwner(ctx, id); err != nil {
		return err
	}
	if err := q.updateOwned(ctx, id, func(task *appjobs.Task) error {
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
	}); err != nil {
		return err
	}
	task, err := q.Get(ctx, id)
	if err == nil && task.Status == appjobs.StatusFailed {
		if err := q.releaseProcessing(ctx, id); err != nil {
			return err
		}
		return q.cache.PushString(ctx, q.pendingKey(), id, q.retention)
	}
	if err == nil {
		return q.releaseProcessing(ctx, id)
	}
	return err
}

func (q *RedisQueue) Complete(ctx context.Context, id string) error {
	if err := q.ensureClaimOwner(ctx, id); err != nil {
		return err
	}
	if err := q.updateOwned(ctx, id, func(task *appjobs.Task) error {
		if task.Status == appjobs.StatusDeadLetter || task.Status == appjobs.StatusCancelled || task.Status == appjobs.StatusSucceeded {
			return appjobs.ErrTaskConflict
		}
		task.Status = appjobs.StatusSucceeded
		return nil
	}); err != nil {
		return err
	}
	return q.releaseProcessing(ctx, id)
}

func (q *RedisQueue) Start(ctx context.Context, id string) error {
	if err := q.ensureClaimOwner(ctx, id); err != nil {
		return err
	}
	return q.updateOwned(ctx, id, func(task *appjobs.Task) error {
		if task.Status == appjobs.StatusDeadLetter || task.Status == appjobs.StatusCancelled || task.Status == appjobs.StatusSucceeded {
			return appjobs.ErrTaskConflict
		}
		task.Status = appjobs.StatusRunning
		return nil
	})
}

func (q *RedisQueue) Cancel(ctx context.Context, id string) error {
	if err := q.ensureClaimOwner(ctx, id); err != nil {
		return err
	}
	if err := q.updateOwned(ctx, id, func(task *appjobs.Task) error {
		if task.Status == appjobs.StatusDeadLetter || task.Status == appjobs.StatusCancelled || task.Status == appjobs.StatusSucceeded {
			return appjobs.ErrTaskConflict
		}
		task.Status = appjobs.StatusCancelled
		return nil
	}); err != nil {
		return err
	}
	return q.releaseProcessing(ctx, id)
}

func (q *RedisQueue) ensureClaimOwner(ctx context.Context, id string) error {
	q.claimsMu.Lock()
	owner := q.claims[id]
	q.claimsMu.Unlock()
	if owner == "" {
		return nil
	}
	current, err := q.cache.GetString(ctx, q.leaseKey(id))
	if err != nil {
		if errors.Is(err, rediscache.ErrCacheMiss) {
			return rediscache.ErrLockNotAcquired
		}
		return err
	}
	if current != owner {
		return rediscache.ErrLockNotAcquired
	}
	return nil
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

func (q *RedisQueue) updateOwned(ctx context.Context, id string, mutate func(*appjobs.Task) error) error {
	if q == nil || q.cache == nil {
		return ErrRedisQueueUnavailable
	}
	q.claimsMu.Lock()
	owner := q.claims[id]
	q.claimsMu.Unlock()
	if owner == "" {
		return q.update(ctx, id, mutate)
	}
	task, err := q.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := mutate(&task); err != nil {
		return err
	}
	return q.cache.SetJSONIfValue(ctx, q.taskKey(id), q.leaseKey(id), owner, taskWire{Task: task, Payload: append([]byte(nil), task.Payload...)}, q.retention)
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

func (q *RedisQueue) pendingKey() string {
	key, _ := q.cache.Key("jobs", "pending")
	return key
}

func (q *RedisQueue) processingKey() string {
	key, _ := q.cache.Key("jobs", "processing")
	return key
}

func (q *RedisQueue) leaseKey(id string) string {
	key, _ := q.cache.Key("jobs", "lease", hashSegment(id))
	return key
}

func (q *RedisQueue) releaseProcessing(ctx context.Context, id string) error {
	q.claimsMu.Lock()
	owner := q.claims[id]
	delete(q.claims, id)
	q.claimsMu.Unlock()
	if owner != "" {
		if err := q.cache.ReleaseListClaim(ctx, q.leaseKey(id), q.processingKey(), owner, id); err != nil {
			if errors.Is(err, rediscache.ErrLockNotAcquired) {
				// A different worker owns the renewed lease; never remove its
				// processing candidate from the recovery list.
				return nil
			}
			return err
		}
	}
	return q.cache.RemoveString(ctx, q.processingKey(), id)
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
var _ appjobs.PendingQueue = (*RedisQueue)(nil)
