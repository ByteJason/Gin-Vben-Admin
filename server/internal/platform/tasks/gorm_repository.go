// Package tasksplatform contains the durable task-definition adapter.
package tasksplatform

import (
	"context"
	"errors"
	"strings"
	"time"

	tasksapp "example.com/gin-vben-admin/server/internal/application/tasks"
	"example.com/gin-vben-admin/server/internal/platform/persistence/gormdb"
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
