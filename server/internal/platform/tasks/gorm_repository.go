// Package tasksplatform contains the durable task-definition adapter.
package tasksplatform

import (
	"context"
	"errors"
	"strings"
	"time"

	tasksapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/tasks"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	"gorm.io/gorm"
)

type GORMRepository struct{ db *gormdb.Store }

func NewGORMRepository(db *gormdb.Store) *GORMRepository { return &GORMRepository{db: db} }

type taskDefinitionRecord struct {
	ID                string     `gorm:"column:id;primaryKey"`
	TenantID          string     `gorm:"column:tenant_id"`
	OrgID             string     `gorm:"column:org_id"`
	Name              string     `gorm:"column:name"`
	Type              string     `gorm:"column:type"`
	PayloadSchema     []byte     `gorm:"column:payload_schema"`
	Cron              string     `gorm:"column:cron"`
	Timezone          string     `gorm:"column:timezone"`
	Enabled           bool       `gorm:"column:enabled"`
	Concurrency       int        `gorm:"column:concurrency"`
	ConcurrencyPolicy string     `gorm:"column:concurrency_policy"`
	TimeoutMS         int64      `gorm:"column:timeout_ms"`
	MaxAttempts       int        `gorm:"column:max_attempts"`
	IdempotencyKey    string     `gorm:"column:idempotency_key"`
	DeletedAt         *time.Time `gorm:"column:deleted_at"`
	CreatedAt         time.Time  `gorm:"column:created_at"`
	UpdatedAt         time.Time  `gorm:"column:updated_at"`
}

func (taskDefinitionRecord) TableName() string { return "task_definitions" }

func (r *GORMRepository) Save(ctx context.Context, definition tasksapp.TaskDefinition) error {
	if r == nil || r.db == nil {
		return tasksapp.ErrRepositoryMissing
	}
	record := fromDefinition(definition)
	var existing taskDefinitionRecord
	query := r.db.Write(ctx).Where("id = ? AND tenant_id = ? AND org_id = ? AND deleted_at IS NULL", record.ID, record.TenantID, record.OrgID)
	err := query.First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if createErr := r.db.Write(ctx).Create(&record).Error; createErr != nil {
			return mapWriteError(createErr)
		}
		return nil
	}
	if err != nil {
		return err
	}
	result := r.db.Write(ctx).Model(&taskDefinitionRecord{}).Where("id = ? AND tenant_id = ? AND org_id = ? AND deleted_at IS NULL", record.ID, record.TenantID, record.OrgID).Updates(map[string]any{
		"name": record.Name, "type": record.Type, "payload_schema": record.PayloadSchema, "cron": record.Cron,
		"timezone": record.Timezone, "enabled": record.Enabled, "concurrency": record.Concurrency,
		"concurrency_policy": record.ConcurrencyPolicy, "timeout_ms": record.TimeoutMS, "max_attempts": record.MaxAttempts,
		"idempotency_key": record.IdempotencyKey, "updated_at": record.UpdatedAt,
	})
	if result.Error != nil {
		return mapWriteError(result.Error)
	}
	if result.RowsAffected == 0 {
		return tasksapp.ErrNotFound
	}
	return nil
}

func (r *GORMRepository) Get(ctx context.Context, id, tenantID, orgID string) (tasksapp.TaskDefinition, error) {
	if r == nil || r.db == nil {
		return tasksapp.TaskDefinition{}, tasksapp.ErrRepositoryMissing
	}
	var record taskDefinitionRecord
	query := r.db.Read(ctx).Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", strings.TrimSpace(id), tenantID)
	if strings.TrimSpace(orgID) != "" {
		query = query.Where("org_id = ?", orgID)
	}
	if err := query.First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tasksapp.TaskDefinition{}, tasksapp.ErrNotFound
		}
		return tasksapp.TaskDefinition{}, err
	}
	return toDefinition(record), nil
}

func (r *GORMRepository) List(ctx context.Context, tenantID, orgID string) ([]tasksapp.TaskDefinition, error) {
	if r == nil || r.db == nil {
		return nil, tasksapp.ErrRepositoryMissing
	}
	query := r.db.Read(ctx).Where("tenant_id = ? AND deleted_at IS NULL", tenantID)
	if strings.TrimSpace(orgID) != "" {
		query = query.Where("org_id = ?", orgID)
	}
	var rows []taskDefinitionRecord
	if err := query.Order("created_at ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]tasksapp.TaskDefinition, 0, len(rows))
	for _, row := range rows {
		items = append(items, toDefinition(row))
	}
	return items, nil
}

func (r *GORMRepository) Delete(ctx context.Context, id, tenantID, orgID string) error {
	if r == nil || r.db == nil {
		return tasksapp.ErrRepositoryMissing
	}
	now := time.Now().UTC()
	query := r.db.Write(ctx).Model(&taskDefinitionRecord{}).Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", strings.TrimSpace(id), tenantID)
	if strings.TrimSpace(orgID) != "" {
		query = query.Where("org_id = ?", orgID)
	}
	result := query.Updates(map[string]any{"deleted_at": now, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return tasksapp.ErrNotFound
	}
	return nil
}

func fromDefinition(definition tasksapp.TaskDefinition) taskDefinitionRecord {
	timeout := definition.Timeout
	if timeout <= 0 && definition.TimeoutSeconds > 0 {
		timeout = time.Duration(definition.TimeoutSeconds) * time.Second
	}
	return taskDefinitionRecord{
		ID: definition.ID, TenantID: definition.TenantID, OrgID: definition.OrgID, Name: definition.Name,
		Type: definition.Type, PayloadSchema: append([]byte(nil), definition.PayloadSchema...), Cron: definition.Cron,
		Timezone: definition.Timezone, Enabled: definition.Enabled, Concurrency: definition.Concurrency,
		ConcurrencyPolicy: definition.ConcurrencyPolicy, TimeoutMS: timeout.Milliseconds(), MaxAttempts: definition.MaxAttempts,
		IdempotencyKey: definition.IdempotencyKey, DeletedAt: definition.DeletedAt, CreatedAt: definition.CreatedAt, UpdatedAt: definition.UpdatedAt,
	}
}

func toDefinition(record taskDefinitionRecord) tasksapp.TaskDefinition {
	return tasksapp.TaskDefinition{
		ID: record.ID, TenantID: record.TenantID, OrgID: record.OrgID, Name: record.Name, Type: record.Type,
		PayloadSchema: append([]byte(nil), record.PayloadSchema...), Cron: record.Cron, Timezone: record.Timezone,
		Enabled: record.Enabled, Concurrency: record.Concurrency, ConcurrencyPolicy: record.ConcurrencyPolicy,
		Timeout: time.Duration(record.TimeoutMS) * time.Millisecond, TimeoutSeconds: int(record.TimeoutMS / 1000),
		MaxAttempts: record.MaxAttempts, IdempotencyKey: record.IdempotencyKey, DeletedAt: record.DeletedAt,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
}

func mapWriteError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "duplicate") || strings.Contains(message, "unique") || strings.Contains(message, "constraint") {
		return tasksapp.ErrConflict
	}
	return err
}

var _ tasksapp.Repository = (*GORMRepository)(nil)

// GORMRunRepository stores execution state and append-only attempt diagnostics
// without persisting the raw payload.
type GORMRunRepository struct{ db *gormdb.Store }

func NewGORMRunRepository(db *gormdb.Store) *GORMRunRepository { return &GORMRunRepository{db: db} }

type taskRunRecord struct {
	ID             string     `gorm:"column:id;primaryKey"`
	TaskID         string     `gorm:"column:task_id"`
	TenantID       string     `gorm:"column:tenant_id"`
	OrgID          string     `gorm:"column:org_id"`
	QueueTaskID    string     `gorm:"column:queue_task_id"`
	IdempotencyKey string     `gorm:"column:idempotency_key"`
	Status         string     `gorm:"column:status"`
	PayloadDigest  string     `gorm:"column:payload_digest"`
	AttemptCount   int        `gorm:"column:attempt_count"`
	MaxAttempts    int        `gorm:"column:max_attempts"`
	LastErrorCode  string     `gorm:"column:last_error_code"`
	StartedAt      *time.Time `gorm:"column:started_at"`
	FinishedAt     *time.Time `gorm:"column:finished_at"`
	DeletedAt      *time.Time `gorm:"column:deleted_at"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
}

func (taskRunRecord) TableName() string { return "task_runs" }

type taskRunLogRecord struct {
	ID        string     `gorm:"column:id;primaryKey"`
	RunID     string     `gorm:"column:run_id"`
	Attempt   int        `gorm:"column:attempt"`
	Status    string     `gorm:"column:status"`
	ErrorCode string     `gorm:"column:error_code"`
	Message   string     `gorm:"column:message"`
	DeletedAt *time.Time `gorm:"column:deleted_at"`
	CreatedAt time.Time  `gorm:"column:created_at"`
	UpdatedAt time.Time  `gorm:"column:updated_at"`
}

func (taskRunLogRecord) TableName() string { return "task_run_logs" }

func (r *GORMRunRepository) Create(ctx context.Context, run tasksapp.TaskRun) (tasksapp.TaskRun, error) {
	if r == nil || r.db == nil {
		return tasksapp.TaskRun{}, tasksapp.ErrRunQueueUnavailable
	}
	record := fromRun(run)
	if err := r.db.Write(ctx).Create(&record).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(strings.ToLower(err.Error()), "unique") {
			return tasksapp.TaskRun{}, tasksapp.ErrRunConflict
		}
		return tasksapp.TaskRun{}, err
	}
	return toRun(record), nil
}

func (r *GORMRunRepository) Get(ctx context.Context, id, tenantID, orgID string) (tasksapp.TaskRun, error) {
	if r == nil || r.db == nil {
		return tasksapp.TaskRun{}, tasksapp.ErrRunQueueUnavailable
	}
	query := r.db.Read(ctx).Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", strings.TrimSpace(id), tenantID)
	if strings.TrimSpace(orgID) != "" {
		query = query.Where("org_id = ?", orgID)
	}
	var row taskRunRecord
	if err := query.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tasksapp.TaskRun{}, tasksapp.ErrRunNotFound
		}
		return tasksapp.TaskRun{}, err
	}
	return toRun(row), nil
}

func (r *GORMRunRepository) GetByIdempotency(ctx context.Context, key, tenantID, orgID string) (tasksapp.TaskRun, error) {
	if r == nil || r.db == nil {
		return tasksapp.TaskRun{}, tasksapp.ErrRunQueueUnavailable
	}
	query := r.db.Read(ctx).Where("idempotency_key = ? AND tenant_id = ? AND deleted_at IS NULL", strings.TrimSpace(key), tenantID)
	if strings.TrimSpace(orgID) != "" {
		query = query.Where("org_id = ?", orgID)
	}
	var row taskRunRecord
	if err := query.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tasksapp.TaskRun{}, tasksapp.ErrRunNotFound
		}
		return tasksapp.TaskRun{}, err
	}
	return toRun(row), nil
}

func (r *GORMRunRepository) GetByQueueTask(ctx context.Context, queueTaskID string) (tasksapp.TaskRun, error) {
	if r == nil || r.db == nil {
		return tasksapp.TaskRun{}, tasksapp.ErrRunQueueUnavailable
	}
	var row taskRunRecord
	if err := r.db.Read(ctx).Where("queue_task_id = ? AND deleted_at IS NULL", strings.TrimSpace(queueTaskID)).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tasksapp.TaskRun{}, tasksapp.ErrRunNotFound
		}
		return tasksapp.TaskRun{}, err
	}
	return toRun(row), nil
}

func (r *GORMRunRepository) List(ctx context.Context, taskID, tenantID, orgID string) ([]tasksapp.TaskRun, error) {
	if r == nil || r.db == nil {
		return nil, tasksapp.ErrRunQueueUnavailable
	}
	query := r.db.Read(ctx).Where("task_id = ? AND tenant_id = ? AND deleted_at IS NULL", strings.TrimSpace(taskID), tenantID)
	if strings.TrimSpace(orgID) != "" {
		query = query.Where("org_id = ?", orgID)
	}
	var rows []taskRunRecord
	if err := query.Order("created_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]tasksapp.TaskRun, 0, len(rows))
	for _, row := range rows {
		out = append(out, toRun(row))
	}
	return out, nil
}

func (r *GORMRunRepository) ListLogs(ctx context.Context, runID, tenantID, orgID string) ([]tasksapp.TaskRunLog, error) {
	if r == nil || r.db == nil {
		return nil, tasksapp.ErrRunQueueUnavailable
	}
	query := r.db.Read(ctx).Table("task_run_logs AS l").Select("l.id, l.run_id, l.attempt, l.status, l.error_code, l.message, l.deleted_at, l.created_at, l.updated_at").Joins("JOIN task_runs AS r ON r.id = l.run_id").Where("l.run_id = ? AND r.tenant_id = ? AND r.deleted_at IS NULL AND l.deleted_at IS NULL", strings.TrimSpace(runID), tenantID)
	if strings.TrimSpace(orgID) != "" {
		query = query.Where("r.org_id = ?", orgID)
	}
	var rows []taskRunLogRecord
	if err := query.Order("l.created_at ASC, l.id ASC").Scan(&rows).Error; err != nil {
		return nil, err
	}
	logs := make([]tasksapp.TaskRunLog, 0, len(rows))
	for _, row := range rows {
		logs = append(logs, toRunLog(row))
	}
	return logs, nil
}

func (r *GORMRunRepository) Update(ctx context.Context, run tasksapp.TaskRun) (tasksapp.TaskRun, error) {
	if r == nil || r.db == nil {
		return tasksapp.TaskRun{}, tasksapp.ErrRunQueueUnavailable
	}
	record := fromRun(run)
	result := r.db.Write(ctx).Model(&taskRunRecord{}).Where("id = ? AND tenant_id = ? AND org_id = ? AND deleted_at IS NULL", record.ID, record.TenantID, record.OrgID).Updates(map[string]any{
		"queue_task_id": record.QueueTaskID, "idempotency_key": record.IdempotencyKey, "status": record.Status,
		"payload_digest": record.PayloadDigest, "attempt_count": record.AttemptCount, "max_attempts": record.MaxAttempts,
		"last_error_code": record.LastErrorCode, "started_at": record.StartedAt, "finished_at": record.FinishedAt, "updated_at": record.UpdatedAt,
	})
	if result.Error != nil {
		return tasksapp.TaskRun{}, result.Error
	}
	if result.RowsAffected == 0 {
		return tasksapp.TaskRun{}, tasksapp.ErrRunNotFound
	}
	return r.Get(ctx, run.ID, run.TenantID, run.OrgID)
}

func (r *GORMRunRepository) AppendLog(ctx context.Context, log tasksapp.TaskRunLog) error {
	if r == nil || r.db == nil {
		return tasksapp.ErrRunQueueUnavailable
	}
	row := taskRunLogRecord{ID: log.ID, RunID: log.RunID, Attempt: log.Attempt, Status: string(log.Status), ErrorCode: log.ErrorCode, Message: log.Message, DeletedAt: log.DeletedAt, CreatedAt: log.CreatedAt, UpdatedAt: log.UpdatedAt}
	return r.db.Write(ctx).Create(&row).Error
}

func toRunLog(row taskRunLogRecord) tasksapp.TaskRunLog {
	return tasksapp.TaskRunLog{ID: row.ID, RunID: row.RunID, Attempt: row.Attempt, Status: tasksapp.RunStatus(row.Status), ErrorCode: row.ErrorCode, Message: row.Message, DeletedAt: row.DeletedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func fromRun(run tasksapp.TaskRun) taskRunRecord {
	return taskRunRecord{ID: run.ID, TaskID: run.TaskID, TenantID: run.TenantID, OrgID: run.OrgID, QueueTaskID: run.QueueTaskID, IdempotencyKey: run.IdempotencyKey, Status: string(run.Status), PayloadDigest: run.PayloadDigest, AttemptCount: run.AttemptCount, MaxAttempts: run.MaxAttempts, LastErrorCode: run.LastErrorCode, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, DeletedAt: run.DeletedAt, CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt}
}

func toRun(row taskRunRecord) tasksapp.TaskRun {
	return tasksapp.TaskRun{ID: row.ID, TaskID: row.TaskID, TenantID: row.TenantID, OrgID: row.OrgID, QueueTaskID: row.QueueTaskID, IdempotencyKey: row.IdempotencyKey, Status: tasksapp.RunStatus(row.Status), PayloadDigest: row.PayloadDigest, AttemptCount: row.AttemptCount, MaxAttempts: row.MaxAttempts, LastErrorCode: row.LastErrorCode, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, DeletedAt: row.DeletedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

var _ tasksapp.RunRepository = (*GORMRunRepository)(nil)
