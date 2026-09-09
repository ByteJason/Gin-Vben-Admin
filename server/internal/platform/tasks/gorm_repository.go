// Package tasksplatform contains the durable task-definition adapter.
package tasksplatform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	tasksapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/tasks"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormquery"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GORMRepository struct{ db *gormdb.Store }

func NewGORMRepository(db *gormdb.Store) *GORMRepository { return &GORMRepository{db: db} }

type taskDefinitionRecord = model.TaskDefinition

func (r *GORMRepository) Save(ctx context.Context, definition tasksapp.TaskDefinition) error {
	if r == nil || r.db == nil {
		return tasksapp.ErrRepositoryMissing
	}
	record := fromDefinition(definition)
	query := gorm.G[taskDefinitionRecord](r.db.Write(ctx)).Where("id = ? AND tenant_id = ? AND org_id = ? AND deleted_at IS NULL", record.ID, record.TenantID, record.OrgID)
	_, err := query.First(ctx)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if createErr := createDefinition(ctx, r.db.Write(ctx), record); createErr != nil {
			return mapWriteError(createErr)
		}
		return nil
	}
	if err != nil {
		return err
	}
	rows, updateErr := gorm.G[taskDefinitionRecord](r.db.Write(ctx)).Where("id = ? AND tenant_id = ? AND org_id = ? AND deleted_at IS NULL", record.ID, record.TenantID, record.OrgID).Set(clause.Assignments(map[string]any{
		"name": record.Name, "description": record.Description, "payload": record.Payload, "type": record.Type, "executor_type": record.ExecutorType, "method_key": record.MethodKey, "http_config": record.HTTPConfig, "payload_schema": record.PayloadSchema, "cron": record.Cron,
		"timezone": record.Timezone, "enabled": record.Enabled, "concurrency": record.Concurrency,
		"concurrency_policy": record.ConcurrencyPolicy, "timeout_ms": record.TimeoutMS, "max_attempts": record.MaxAttempts,
		"idempotency_key": record.IdempotencyKey, "updated_at": record.UpdatedAt,
	})).Update(ctx)
	if updateErr != nil {
		return mapWriteError(updateErr)
	}
	if rows == 0 {
		return tasksapp.ErrNotFound
	}
	return nil
}

func (r *GORMRepository) Get(ctx context.Context, id, tenantID, orgID string) (tasksapp.TaskDefinition, error) {
	if r == nil || r.db == nil {
		return tasksapp.TaskDefinition{}, tasksapp.ErrRepositoryMissing
	}
	var record taskDefinitionRecord
	query := gorm.G[taskDefinitionRecord](r.db.Read(ctx)).Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", strings.TrimSpace(id), tenantID)
	if strings.TrimSpace(orgID) != "" {
		query = query.Where("org_id = ?", orgID)
	}
	record, err := query.First(ctx)
	if err != nil {
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
	query := gorm.G[taskDefinitionRecord](r.db.Read(ctx)).Where("tenant_id = ? AND deleted_at IS NULL", tenantID)
	if strings.TrimSpace(orgID) != "" {
		query = query.Where("org_id = ?", orgID)
	}
	rows, err := query.Order("created_at ASC, id ASC").Find(ctx)
	if err != nil {
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
	query := gorm.G[taskDefinitionRecord](r.db.Write(ctx)).Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", strings.TrimSpace(id), tenantID)
	if strings.TrimSpace(orgID) != "" {
		query = query.Where("org_id = ?", orgID)
	}
	rows, err := query.Set(clause.Assignments(map[string]any{"deleted_at": now, "updated_at": now})).Update(ctx)
	if err != nil {
		return err
	}
	if rows == 0 {
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
		Description: definition.Description, Payload: append([]byte(nil), definition.Payload...), Type: definition.Type, ExecutorType: definition.ExecutorType, MethodKey: definition.MethodKey, HTTPConfig: append([]byte(nil), definition.HTTPConfig...), PayloadSchema: append([]byte(nil), definition.PayloadSchema...), Cron: definition.Cron,
		Timezone: definition.Timezone, Enabled: definition.Enabled, Concurrency: int32(definition.Concurrency),
		ConcurrencyPolicy: definition.ConcurrencyPolicy, TimeoutMS: timeout.Milliseconds(), MaxAttempts: int32(definition.MaxAttempts),
		IdempotencyKey: definition.IdempotencyKey, DeletedAt: definition.DeletedAt, CreatedAt: definition.CreatedAt, UpdatedAt: definition.UpdatedAt,
	}
}

func toDefinition(record taskDefinitionRecord) tasksapp.TaskDefinition {
	return tasksapp.TaskDefinition{
		ID: record.ID, TenantID: record.TenantID, OrgID: record.OrgID, Name: record.Name, Description: record.Description, Payload: append([]byte(nil), record.Payload...), Type: record.Type, ExecutorType: record.ExecutorType, MethodKey: record.MethodKey, HTTPConfig: append([]byte(nil), record.HTTPConfig...),
		PayloadSchema: append([]byte(nil), record.PayloadSchema...), Cron: record.Cron, Timezone: record.Timezone,
		Enabled: record.Enabled, Concurrency: int(record.Concurrency), ConcurrencyPolicy: record.ConcurrencyPolicy,
		Timeout: time.Duration(record.TimeoutMS) * time.Millisecond, TimeoutSeconds: int(record.TimeoutMS / 1000),
		MaxAttempts: int(record.MaxAttempts), IdempotencyKey: record.IdempotencyKey, DeletedAt: record.DeletedAt,
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

type taskRunRecord = model.TaskRun
type taskRunLogRecord = model.TaskRunLog

func (r *GORMRunRepository) Create(ctx context.Context, run tasksapp.TaskRun) (tasksapp.TaskRun, error) {
	if r == nil || r.db == nil {
		return tasksapp.TaskRun{}, tasksapp.ErrRunQueueUnavailable
	}
	record := fromRun(run)
	if err := createRun(ctx, r.db.Write(ctx), record); err != nil {
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
	query := gorm.G[taskRunRecord](r.db.Read(ctx)).Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", strings.TrimSpace(id), tenantID)
	if strings.TrimSpace(orgID) != "" {
		query = query.Where("org_id = ?", orgID)
	}
	var row taskRunRecord
	row, err := query.First(ctx)
	if err != nil {
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
	query := gorm.G[taskRunRecord](r.db.Read(ctx)).Where("idempotency_key = ? AND tenant_id = ? AND deleted_at IS NULL", strings.TrimSpace(key), tenantID)
	if strings.TrimSpace(orgID) != "" {
		query = query.Where("org_id = ?", orgID)
	}
	var row taskRunRecord
	row, err := query.First(ctx)
	if err != nil {
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
	row, err := gorm.G[taskRunRecord](r.db.Read(ctx)).Where("queue_task_id = ? AND deleted_at IS NULL", strings.TrimSpace(queueTaskID)).First(ctx)
	if err != nil {
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
	query := gorm.G[taskRunRecord](r.db.Read(ctx)).Where("tenant_id = ? AND deleted_at IS NULL", tenantID)
	if strings.TrimSpace(taskID) != "" {
		query = query.Where("task_id = ?", strings.TrimSpace(taskID))
	}
	if strings.TrimSpace(orgID) != "" {
		query = query.Where("org_id = ?", orgID)
	}
	rows, err := query.Order("created_at DESC, id DESC").Find(ctx)
	if err != nil {
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
	// The join is a dedicated projection: logs are tenant-scoped through their
	// parent run, while the returned shape remains the persistence model.
	sql := "SELECT l.id, l.run_id, l.attempt, l.status, l.trigger_source, l.executor_type, l.error_code, l.message, l.started_at, l.finished_at, l.duration_ms, l.result_summary, l.redacted_output, l.deleted_at, l.created_at, l.updated_at FROM gvba_task_run_logs AS l JOIN gvba_task_runs AS r ON r.id = l.run_id WHERE l.run_id = ? AND r.tenant_id = ? AND r.deleted_at IS NULL AND l.deleted_at IS NULL"
	args := []any{strings.TrimSpace(runID), tenantID}
	if strings.TrimSpace(orgID) != "" {
		sql += " AND r.org_id = ?"
		args = append(args, orgID)
	}
	sql += " ORDER BY l.created_at ASC, l.id ASC"
	rows, err := gorm.G[taskRunLogRecord](r.db.Read(ctx)).Raw(sql, args...).Find(ctx)
	if err != nil {
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
	rows, updateErr := gorm.G[taskRunRecord](r.db.Write(ctx)).Where("id = ? AND tenant_id = ? AND org_id = ? AND deleted_at IS NULL", record.ID, record.TenantID, record.OrgID).Set(clause.Assignments(map[string]any{
		"queue_task_id": record.QueueTaskID, "idempotency_key": record.IdempotencyKey, "status": record.Status,
		"payload_digest": record.PayloadDigest, "attempt_count": record.AttemptCount, "max_attempts": record.MaxAttempts,
		"last_error_code": record.LastErrorCode, "error_code": record.ErrorCode, "trigger_source": record.TriggerSource, "executor_type": record.ExecutorType, "started_at": record.StartedAt, "finished_at": record.FinishedAt, "duration_ms": record.DurationMS, "result_summary": record.ResultSummary, "redacted_output": record.RedactedOutput, "updated_at": record.UpdatedAt,
	})).Update(ctx)
	if updateErr != nil {
		return tasksapp.TaskRun{}, updateErr
	}
	if rows == 0 {
		return tasksapp.TaskRun{}, tasksapp.ErrRunNotFound
	}
	return r.Get(ctx, run.ID, run.TenantID, run.OrgID)
}

func (r *GORMRunRepository) AppendLog(ctx context.Context, log tasksapp.TaskRunLog) error {
	if r == nil || r.db == nil {
		return tasksapp.ErrRunQueueUnavailable
	}
	row := taskRunLogRecord{ID: log.ID, RunID: log.RunID, Attempt: int32(log.Attempt), Status: string(log.Status), TriggerSource: log.TriggerSource, ExecutorType: log.ExecutorType, ErrorCode: log.ErrorCode, Message: log.Message, StartedAt: log.StartedAt, FinishedAt: log.FinishedAt, DurationMS: log.DurationMS, ResultSummary: log.ResultSummary, RedactedOutput: log.RedactedOutput, DeletedAt: log.DeletedAt, CreatedAt: log.CreatedAt, UpdatedAt: log.UpdatedAt}
	return createRunLog(ctx, r.db.Write(ctx), row)
}

// Task models keep database defaults in the schema for fresh installs, but a
// repository receives an already-normalized definition/run/log. Assignment
// creates preserve explicit zero values such as Enabled=false instead of
// letting GORM's default callback replace them.
func createDefinition(ctx context.Context, db *gorm.DB, record taskDefinitionRecord) error {
	values := map[string]any{
		"id": record.ID, "tenant_id": record.TenantID, "org_id": record.OrgID, "name": record.Name,
		"type": record.Type, "description": record.Description, "payload": record.Payload, "executor_type": record.ExecutorType, "method_key": record.MethodKey, "http_config": record.HTTPConfig, "payload_schema": record.PayloadSchema, "cron": record.Cron,
		"timezone": record.Timezone, "enabled": record.Enabled, "concurrency": record.Concurrency,
		"concurrency_policy": record.ConcurrencyPolicy, "timeout_ms": record.TimeoutMS,
		"max_attempts": record.MaxAttempts, "idempotency_key": record.IdempotencyKey,
		"deleted_at": record.DeletedAt,
	}
	// A zero timestamp means the caller wants the database's CURRENT_TIMESTAMP
	// default. Supplying Go's zero time explicitly would violate the intended
	// fresh-install default (and is rejected by some SQL modes).
	if !record.CreatedAt.IsZero() {
		values["created_at"] = record.CreatedAt
	}
	if !record.UpdatedAt.IsZero() {
		values["updated_at"] = record.UpdatedAt
	}
	return gormquery.CreateValues[taskDefinitionRecord](ctx, db, values)
}

func createRun(ctx context.Context, db *gorm.DB, record taskRunRecord) error {
	return gormquery.CreateValues[taskRunRecord](ctx, db, map[string]any{
		"id": record.ID, "task_id": record.TaskID, "task_name": record.TaskName, "task_description": record.TaskDescription, "config_snapshot": record.ConfigSnapshot, "tenant_id": record.TenantID, "org_id": record.OrgID,
		"queue_task_id": record.QueueTaskID, "idempotency_key": record.IdempotencyKey, "status": record.Status,
		"payload_digest": record.PayloadDigest, "attempt_count": record.AttemptCount, "max_attempts": record.MaxAttempts,
		"last_error_code": record.LastErrorCode, "trigger_source": record.TriggerSource, "executor_type": record.ExecutorType, "error_code": record.ErrorCode, "duration_ms": record.DurationMS, "result_summary": record.ResultSummary, "redacted_output": record.RedactedOutput, "started_at": record.StartedAt, "finished_at": record.FinishedAt,
		"deleted_at": record.DeletedAt, "created_at": record.CreatedAt, "updated_at": record.UpdatedAt,
	})
}

func createRunLog(ctx context.Context, db *gorm.DB, record taskRunLogRecord) error {
	return gormquery.CreateValues[taskRunLogRecord](ctx, db, map[string]any{
		"id": record.ID, "run_id": record.RunID, "attempt": record.Attempt, "status": record.Status,
		"trigger_source": record.TriggerSource, "executor_type": record.ExecutorType, "error_code": record.ErrorCode, "message": record.Message, "started_at": record.StartedAt, "finished_at": record.FinishedAt, "duration_ms": record.DurationMS, "result_summary": record.ResultSummary, "redacted_output": record.RedactedOutput, "deleted_at": record.DeletedAt,
		"created_at": record.CreatedAt, "updated_at": record.UpdatedAt,
	})
}

func toRunLog(row taskRunLogRecord) tasksapp.TaskRunLog {
	return tasksapp.TaskRunLog{ID: row.ID, RunID: row.RunID, Attempt: int(row.Attempt), Status: tasksapp.RunStatus(row.Status), TriggerSource: row.TriggerSource, ExecutorType: row.ExecutorType, ErrorCode: row.ErrorCode, Message: row.Message, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, DurationMS: row.DurationMS, ResultSummary: row.ResultSummary, RedactedOutput: row.RedactedOutput, DeletedAt: row.DeletedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func fromRun(run tasksapp.TaskRun) taskRunRecord {
	return taskRunRecord{ID: run.ID, TaskID: run.TaskID, TaskName: run.TaskName, TaskDescription: run.TaskDescription, ConfigSnapshot: model.JSONValue(run.ConfigSnapshot), TenantID: run.TenantID, OrgID: run.OrgID, QueueTaskID: run.QueueTaskID, IdempotencyKey: run.IdempotencyKey, Status: string(run.Status), PayloadDigest: run.PayloadDigest, AttemptCount: int32(run.AttemptCount), MaxAttempts: int32(run.MaxAttempts), LastErrorCode: run.LastErrorCode, ErrorCode: run.ErrorCode, TriggerSource: run.TriggerSource, ExecutorType: run.ExecutorType, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, DurationMS: run.DurationMS, ResultSummary: run.ResultSummary, RedactedOutput: run.RedactedOutput, DeletedAt: run.DeletedAt, CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt}
}

func toRun(row taskRunRecord) tasksapp.TaskRun {
	return tasksapp.TaskRun{ID: row.ID, TaskID: row.TaskID, TaskName: row.TaskName, TaskDescription: row.TaskDescription, ConfigSnapshot: json.RawMessage(row.ConfigSnapshot), TenantID: row.TenantID, OrgID: row.OrgID, QueueTaskID: row.QueueTaskID, IdempotencyKey: row.IdempotencyKey, Status: tasksapp.RunStatus(row.Status), PayloadDigest: row.PayloadDigest, AttemptCount: int(row.AttemptCount), MaxAttempts: int(row.MaxAttempts), LastErrorCode: row.LastErrorCode, ErrorCode: row.ErrorCode, TriggerSource: row.TriggerSource, ExecutorType: row.ExecutorType, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, DurationMS: row.DurationMS, ResultSummary: row.ResultSummary, RedactedOutput: row.RedactedOutput, DeletedAt: row.DeletedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

var _ tasksapp.RunRepository = (*GORMRunRepository)(nil)
