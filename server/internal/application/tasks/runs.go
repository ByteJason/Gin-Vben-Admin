package tasks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"example.com/gin-vben-admin/server/internal/application/jobs"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
)

type RunStatus string

const (
	RunPending    RunStatus = "pending"
	RunRunning    RunStatus = "running"
	RunSucceeded  RunStatus = "succeeded"
	RunFailed     RunStatus = "failed"
	RunDeadLetter RunStatus = "dead_letter"
	RunCancelled  RunStatus = "cancelled"
)

var (
	ErrRunNotFound         = errors.New("task run not found")
	ErrRunConflict         = errors.New("task run already exists")
	ErrRunStateConflict    = errors.New("task run state conflict")
	ErrInvalidRunPayload   = errors.New("task run payload must be a JSON object")
	ErrRunQueueUnavailable = errors.New("task run queue unavailable")
)

type TaskRun struct {
	ID             string     `json:"id"`
	TaskID         string     `json:"taskId"`
	TenantID       string     `json:"tenantId"`
	OrgID          string     `json:"orgId,omitempty"`
	QueueTaskID    string     `json:"queueTaskId,omitempty"`
	IdempotencyKey string     `json:"idempotencyKey"`
	Status         RunStatus  `json:"status"`
	PayloadDigest  string     `json:"payloadDigest"`
	AttemptCount   int        `json:"attemptCount"`
	MaxAttempts    int        `json:"maxAttempts"`
	LastErrorCode  string     `json:"lastErrorCode,omitempty"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	FinishedAt     *time.Time `json:"finishedAt,omitempty"`
	DeletedAt      *time.Time `json:"deletedAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type TaskRunLog struct {
	ID        string     `json:"id"`
	RunID     string     `json:"runId"`
	Attempt   int        `json:"attempt"`
	Status    RunStatus  `json:"status"`
	ErrorCode string     `json:"errorCode,omitempty"`
	Message   string     `json:"message,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
}

type RunRepository interface {
	Create(context.Context, TaskRun) (TaskRun, error)
	Get(context.Context, string, string, string) (TaskRun, error)
	GetByIdempotency(context.Context, string, string, string) (TaskRun, error)
	GetByQueueTask(context.Context, string) (TaskRun, error)
	List(context.Context, string, string, string) ([]TaskRun, error)
	ListLogs(context.Context, string, string, string) ([]TaskRunLog, error)
	Update(context.Context, TaskRun) (TaskRun, error)
	AppendLog(context.Context, TaskRunLog) error
}

type MemoryRunRepository struct {
	mu   sync.RWMutex
	runs map[string]TaskRun
	logs map[string][]TaskRunLog
}

func NewMemoryRunRepository() *MemoryRunRepository {
	return &MemoryRunRepository{runs: map[string]TaskRun{}, logs: map[string][]TaskRunLog{}}
}

func (r *MemoryRunRepository) Create(ctx context.Context, run TaskRun) (TaskRun, error) {
	if err := contextErr(ctx); err != nil {
		return TaskRun{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.runs == nil {
		r.runs = map[string]TaskRun{}
	}
	for _, existing := range r.runs {
		if existing.DeletedAt == nil && existing.TenantID == run.TenantID && existing.OrgID == run.OrgID && existing.IdempotencyKey == run.IdempotencyKey {
			return cloneRun(existing), ErrRunConflict
		}
	}
	if _, exists := r.runs[run.ID]; exists {
		return TaskRun{}, ErrRunConflict
	}
	r.runs[run.ID] = cloneRun(run)
	return cloneRun(run), nil
}

func (r *MemoryRunRepository) Get(ctx context.Context, id, tenantID, orgID string) (TaskRun, error) {
	if err := contextErr(ctx); err != nil {
		return TaskRun{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	run, ok := r.runs[strings.TrimSpace(id)]
	if !ok || run.DeletedAt != nil || !runInScope(run, tenantID, orgID) {
		return TaskRun{}, ErrRunNotFound
	}
	return cloneRun(run), nil
}

func (r *MemoryRunRepository) GetByIdempotency(ctx context.Context, key, tenantID, orgID string) (TaskRun, error) {
	if err := contextErr(ctx); err != nil {
		return TaskRun{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, run := range r.runs {
		if run.DeletedAt == nil && run.IdempotencyKey == key && runInScope(run, tenantID, orgID) {
			return cloneRun(run), nil
		}
	}
	return TaskRun{}, ErrRunNotFound
}

func (r *MemoryRunRepository) GetByQueueTask(ctx context.Context, queueTaskID string) (TaskRun, error) {
	if err := contextErr(ctx); err != nil {
		return TaskRun{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, run := range r.runs {
		if run.DeletedAt == nil && run.QueueTaskID == strings.TrimSpace(queueTaskID) {
			return cloneRun(run), nil
		}
	}
	return TaskRun{}, ErrRunNotFound
}

func (r *MemoryRunRepository) List(ctx context.Context, taskID, tenantID, orgID string) ([]TaskRun, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]TaskRun, 0)
	for _, run := range r.runs {
		if run.DeletedAt == nil && run.TaskID == taskID && runInScope(run, tenantID, orgID) {
			out = append(out, cloneRun(run))
		}
	}
	return out, nil
}

func (r *MemoryRunRepository) Update(ctx context.Context, run TaskRun) (TaskRun, error) {
	if err := contextErr(ctx); err != nil {
		return TaskRun{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.runs[run.ID]; !ok {
		return TaskRun{}, ErrRunNotFound
	}
	r.runs[run.ID] = cloneRun(run)
	return cloneRun(run), nil
}

func (r *MemoryRunRepository) ListLogs(ctx context.Context, runID, tenantID, orgID string) ([]TaskRunLog, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	run, ok := r.runs[strings.TrimSpace(runID)]
	if !ok || run.DeletedAt != nil || !runInScope(run, tenantID, orgID) {
		return nil, ErrRunNotFound
	}
	logs := append([]TaskRunLog(nil), r.logs[run.ID]...)
	return logs, nil
}

func (r *MemoryRunRepository) AppendLog(ctx context.Context, log TaskRunLog) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.logs == nil {
		r.logs = map[string][]TaskRunLog{}
	}
	r.logs[log.RunID] = append(r.logs[log.RunID], log)
	return nil
}

type RunService struct {
	definitions *Service
	repo        RunRepository
	queue       jobs.Queue
	clock       func() time.Time
}

func NewRunService(definitions *Service, repo RunRepository, queue jobs.Queue) *RunService {
	if repo == nil {
		repo = NewMemoryRunRepository()
	}
	if queue == nil {
		queue = jobs.NewMemoryQueue(3)
	}
	return &RunService{definitions: definitions, repo: repo, queue: queue, clock: time.Now}
}

func (s *RunService) SetClock(clock func() time.Time) {
	if s != nil && clock != nil {
		s.clock = clock
	}
}

func (s *RunService) Enqueue(ctx context.Context, taskID string, payload []byte, idempotencyKey string) (TaskRun, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return TaskRun{}, err
	}
	if s == nil || s.definitions == nil || s.repo == nil || s.queue == nil {
		return TaskRun{}, ErrRunQueueUnavailable
	}
	definition, err := s.definitions.Get(ctx, taskID)
	if err != nil {
		return TaskRun{}, err
	}
	payload = normalizePayload(payload)
	if !isJSONObject(payload) {
		return TaskRun{}, ErrInvalidRunPayload
	}
	key := strings.TrimSpace(idempotencyKey)
	if key == "" {
		key = strings.TrimSpace(definition.IdempotencyKey)
	}
	if key == "" {
		key = newID("run-key")
	}
	if existing, getErr := s.repo.GetByIdempotency(ctx, key, scope.TenantID, scope.Organization); getErr == nil {
		return existing, nil
	}
	queued, err := s.queue.Enqueue(ctx, jobs.Task{Type: definition.Type, PayloadVersion: 1, IdempotencyKey: key, Payload: payload, MaxAttempts: definition.MaxAttempts})
	if err != nil {
		return TaskRun{}, ErrRunQueueUnavailable
	}
	now := s.now()
	run := TaskRun{ID: newID("run"), TaskID: definition.ID, TenantID: scope.TenantID, OrgID: scope.Organization, QueueTaskID: queued.ID, IdempotencyKey: key, Status: RunPending, PayloadDigest: digest(payload), MaxAttempts: definition.MaxAttempts, CreatedAt: now, UpdatedAt: now}
	created, err := s.repo.Create(ctx, run)
	if errors.Is(err, ErrRunConflict) {
		if existing, getErr := s.repo.GetByIdempotency(ctx, key, scope.TenantID, scope.Organization); getErr == nil {
			return existing, nil
		}
	}
	return created, err
}

func (s *RunService) List(ctx context.Context, taskID string) ([]TaskRun, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	if s == nil || s.repo == nil {
		return nil, ErrRunQueueUnavailable
	}
	if s.definitions != nil {
		if _, err := s.definitions.Get(ctx, taskID); err != nil {
			return nil, err
		}
	}
	return s.repo.List(ctx, taskID, scope.TenantID, scope.Organization)
}

// Get returns one tenant-scoped execution record.
func (s *RunService) Get(ctx context.Context, runID string) (TaskRun, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return TaskRun{}, err
	}
	if s == nil || s.repo == nil {
		return TaskRun{}, ErrRunQueueUnavailable
	}
	return s.repo.Get(ctx, runID, scope.TenantID, scope.Organization)
}

// Logs returns append-only execution diagnostics for one tenant-scoped run.
// Messages are supplied by trusted worker adapters and never contain payloads.
func (s *RunService) Logs(ctx context.Context, taskID, runID string) ([]TaskRunLog, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	if s == nil || s.repo == nil {
		return nil, ErrRunQueueUnavailable
	}
	run, err := s.repo.Get(ctx, runID, scope.TenantID, scope.Organization)
	if err != nil || run.TaskID != taskID {
		return nil, ErrRunNotFound
	}
	return s.repo.ListLogs(ctx, runID, scope.TenantID, scope.Organization)
}

func (s *RunService) Cancel(ctx context.Context, taskID, runID string) (TaskRun, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return TaskRun{}, err
	}
	run, err := s.repo.Get(ctx, runID, scope.TenantID, scope.Organization)
	if err != nil || run.TaskID != taskID {
		return TaskRun{}, ErrRunNotFound
	}
	if run.Status != RunPending && run.Status != RunRunning && run.Status != RunFailed {
		return TaskRun{}, ErrRunStateConflict
	}
	if run.QueueTaskID != "" {
		_ = s.queue.Cancel(ctx, run.QueueTaskID)
	}
	now := s.now()
	run.Status, run.FinishedAt, run.UpdatedAt = RunCancelled, &now, now
	return s.repo.Update(ctx, run)
}

func (s *RunService) Retry(ctx context.Context, taskID, runID string) (TaskRun, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return TaskRun{}, err
	}
	run, err := s.repo.Get(ctx, runID, scope.TenantID, scope.Organization)
	if err != nil || run.TaskID != taskID {
		return TaskRun{}, ErrRunNotFound
	}
	if run.Status != RunFailed && run.Status != RunDeadLetter {
		return TaskRun{}, ErrRunStateConflict
	}
	old, getErr := s.queue.Get(ctx, run.QueueTaskID)
	if getErr != nil {
		return TaskRun{}, ErrRunQueueUnavailable
	}
	newKey := run.IdempotencyKey + ":retry:" + s.now().UTC().Format("20060102150405.000000000")
	queued, queueErr := s.queue.Enqueue(ctx, jobs.Task{Type: old.Type, PayloadVersion: old.PayloadVersion, IdempotencyKey: newKey, Payload: old.Payload, MaxAttempts: run.MaxAttempts})
	if queueErr != nil {
		return TaskRun{}, ErrRunQueueUnavailable
	}
	now := s.now()
	run.QueueTaskID, run.IdempotencyKey, run.Status, run.FinishedAt, run.UpdatedAt = queued.ID, newKey, RunPending, nil, now
	return s.repo.Update(ctx, run)
}

func (s *RunService) MarkRunning(ctx context.Context, runID string) (TaskRun, error) {
	return s.transition(ctx, runID, RunRunning, "")
}

func (s *RunService) MarkSucceeded(ctx context.Context, runID string) (TaskRun, error) {
	return s.transition(ctx, runID, RunSucceeded, "")
}

func (s *RunService) MarkCancelled(ctx context.Context, runID string) (TaskRun, error) {
	return s.transition(ctx, runID, RunCancelled, "worker.cancelled")
}

func (s *RunService) MarkFailed(ctx context.Context, runID, errorCode string) (TaskRun, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return TaskRun{}, err
	}
	run, err := s.repo.Get(ctx, runID, scope.TenantID, scope.Organization)
	if err != nil {
		return TaskRun{}, err
	}
	run.AttemptCount++
	run.LastErrorCode = strings.TrimSpace(errorCode)
	if run.AttemptCount >= run.MaxAttempts {
		run.Status = RunDeadLetter
	} else {
		run.Status = RunFailed
	}
	now := s.now()
	run.UpdatedAt = now
	if run.Status == RunDeadLetter {
		run.FinishedAt = &now
	} else {
		run.FinishedAt = nil
	}
	updated, err := s.repo.Update(ctx, run)
	if err == nil {
		_ = s.repo.AppendLog(ctx, TaskRunLog{ID: newID("run-log"), RunID: run.ID, Attempt: run.AttemptCount, Status: run.Status, ErrorCode: run.LastErrorCode, CreatedAt: now, UpdatedAt: now})
	}
	return updated, err
}

func (s *RunService) transition(ctx context.Context, runID string, status RunStatus, errorCode string) (TaskRun, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return TaskRun{}, err
	}
	run, err := s.repo.Get(ctx, runID, scope.TenantID, scope.Organization)
	if err != nil {
		return TaskRun{}, err
	}
	if run.Status == RunCancelled || run.Status == RunSucceeded || run.Status == RunDeadLetter {
		return TaskRun{}, ErrRunStateConflict
	}
	now := s.now()
	run.Status, run.UpdatedAt = status, now
	if status == RunRunning {
		run.StartedAt = &now
	}
	if status == RunSucceeded || status == RunCancelled {
		run.FinishedAt = &now
	}
	run.LastErrorCode = errorCode
	updated, updateErr := s.repo.Update(ctx, run)
	if updateErr == nil {
		_ = s.repo.AppendLog(ctx, TaskRunLog{ID: newID("run-log"), RunID: run.ID, Attempt: run.AttemptCount, Status: status, ErrorCode: errorCode, CreatedAt: now, UpdatedAt: now})
	}
	return updated, updateErr
}

// BindWorker registers a task handler that mirrors queue transitions into the
// durable run record. A worker can be composed with multiple explicit task
// types; no request payload is interpreted as executable code.
func (s *RunService) BindWorker(worker *jobs.Worker, taskType string, handler jobs.Handler) error {
	if s == nil || s.repo == nil || worker == nil || handler == nil {
		return ErrRunQueueUnavailable
	}
	return worker.Register(taskType, func(ctx context.Context, queued jobs.Task) error {
		run, err := s.repo.GetByQueueTask(ctx, queued.ID)
		if err != nil {
			return err
		}
		runCtx := ctx
		if _, scopeErr := tenant.RequireContext(ctx); scopeErr != nil {
			scope, scopeErr := tenant.NewContext(run.TenantID, run.OrgID, false)
			if scopeErr != nil {
				return scopeErr
			}
			runCtx = tenant.WithContext(ctx, scope)
		}
		if _, err := s.MarkRunning(runCtx, run.ID); err != nil {
			return err
		}
		if err := handler(runCtx, queued); err != nil {
			stateCtx := runCtx
			if runCtx.Err() != nil {
				stateCtx = context.WithoutCancel(runCtx)
			}
			if errors.Is(err, context.Canceled) {
				_, _ = s.MarkCancelled(stateCtx, run.ID)
				return err
			}
			_, _ = s.MarkFailed(stateCtx, run.ID, stableRunErrorCode(err))
			return err
		}
		_, err = s.MarkSucceeded(runCtx, run.ID)
		return err
	})
}

// RegisterWorker is a readable alias retained for composition roots.
func (s *RunService) RegisterWorker(worker *jobs.Worker, taskType string, handler jobs.Handler) error {
	return s.BindWorker(worker, taskType, handler)
}

func (s *RunService) now() time.Time {
	if s != nil && s.clock != nil {
		return s.clock().UTC()
	}
	return time.Now().UTC()
}

func runInScope(run TaskRun, tenantID, orgID string) bool {
	return run.TenantID == tenantID && (orgID == "" || run.OrgID == orgID)
}

func cloneRun(run TaskRun) TaskRun { return run }

func normalizePayload(payload []byte) []byte {
	if len(strings.TrimSpace(string(payload))) == 0 {
		return []byte(`{}`)
	}
	return append([]byte(nil), payload...)
}

func isJSONObject(payload []byte) bool {
	var value any
	if json.Unmarshal(payload, &value) != nil {
		return false
	}
	_, ok := value.(map[string]any)
	return ok
}

func digest(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func stableRunErrorCode(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "worker.timeout"
	case errors.Is(err, context.Canceled):
		return "worker.cancelled"
	default:
		return "worker.failed"
	}
}
