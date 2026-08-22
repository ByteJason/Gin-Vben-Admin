// Package importsplatform contains the durable IMPORT-100 database adapter.
package importsplatform

import (
	"context"
	"errors"
	"strings"
	"time"

	importsapp "example.com/gin-vben-admin/server/internal/application/imports"
	"example.com/gin-vben-admin/server/internal/platform/persistence/gormdb"
	"gorm.io/gorm"
)

type GORMRepository struct{ db *gormdb.Store }

func NewGORMRepository(db *gormdb.Store) *GORMRepository { return &GORMRepository{db: db} }

type jobRecord struct {
	ID             string     `gorm:"column:id;primaryKey"`
	Kind           string     `gorm:"column:kind"`
	TenantID       string     `gorm:"column:tenant_id"`
	OrgID          string     `gorm:"column:org_id"`
	ActorID        string     `gorm:"column:actor_id"`
	PreviewID      string     `gorm:"column:preview_id"`
	QueueTaskID    string     `gorm:"column:queue_task_id"`
	IdempotencyKey string     `gorm:"column:idempotency_key"`
	Status         string     `gorm:"column:status"`
	Format         string     `gorm:"column:format"`
	TotalRows      int        `gorm:"column:total_rows"`
	ProcessedRows  int        `gorm:"column:processed_rows"`
	ErrorCount     int        `gorm:"column:error_count"`
	LastErrorCode  string     `gorm:"column:last_error_code"`
	DownloadKey    string     `gorm:"column:download_key"`
	ExpiresAt      *time.Time `gorm:"column:expires_at"`
	FinishedAt     *time.Time `gorm:"column:finished_at"`
	DeletedAt      *time.Time `gorm:"column:deleted_at"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
}

func (jobRecord) TableName() string { return "import_export_jobs" }

type errorRecord struct {
	ID         string     `gorm:"column:id;primaryKey"`
	JobID      string     `gorm:"column:job_id"`
	RowNumber  int        `gorm:"column:row_number"`
	ColumnName string     `gorm:"column:column_name"`
	Code       string     `gorm:"column:code"`
	MessageKey string     `gorm:"column:message_key"`
	DeletedAt  *time.Time `gorm:"column:deleted_at"`
	CreatedAt  time.Time  `gorm:"column:created_at"`
	UpdatedAt  time.Time  `gorm:"column:updated_at"`
}

func (errorRecord) TableName() string { return "import_export_errors" }

func (r *GORMRepository) Create(ctx context.Context, job importsapp.Job) (importsapp.Job, error) {
	if r == nil || r.db == nil {
		return importsapp.Job{}, importsapp.ErrJobNotFound
	}
	record := fromJob(job)
	if err := r.db.Write(ctx).Create(&record).Error; err != nil {
		if isConflict(err) {
			return importsapp.Job{}, importsapp.ErrJobConflict
		}
		return importsapp.Job{}, err
	}
	return toJob(record), nil
}

func (r *GORMRepository) Get(ctx context.Context, id, tenantID, orgID string) (importsapp.Job, error) {
	if r == nil || r.db == nil {
		return importsapp.Job{}, importsapp.ErrJobNotFound
	}
	query := r.db.Read(ctx).Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", strings.TrimSpace(id), tenantID)
	if strings.TrimSpace(orgID) != "" {
		query = query.Where("org_id = ?", orgID)
	}
	var row jobRecord
	if err := query.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return importsapp.Job{}, importsapp.ErrJobNotFound
		}
		return importsapp.Job{}, err
	}
	return toJob(row), nil
}

func (r *GORMRepository) List(ctx context.Context, kind, tenantID, orgID string) ([]importsapp.Job, error) {
	if r == nil || r.db == nil {
		return nil, importsapp.ErrJobNotFound
	}
	query := r.db.Read(ctx).Where("tenant_id = ? AND deleted_at IS NULL", tenantID)
	if strings.TrimSpace(orgID) != "" {
		query = query.Where("org_id = ?", orgID)
	}
	if strings.TrimSpace(kind) != "" {
		query = query.Where("kind = ?", kind)
	}
	var rows []jobRecord
	if err := query.Order("created_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]importsapp.Job, 0, len(rows))
	for _, row := range rows {
		items = append(items, toJob(row))
	}
	return items, nil
}

func (r *GORMRepository) Update(ctx context.Context, job importsapp.Job) (importsapp.Job, error) {
	if r == nil || r.db == nil {
		return importsapp.Job{}, importsapp.ErrJobNotFound
	}
	record := fromJob(job)
	result := r.db.Write(ctx).Model(&jobRecord{}).Where("id = ? AND tenant_id = ? AND org_id = ? AND deleted_at IS NULL", record.ID, record.TenantID, record.OrgID).Updates(map[string]any{
		"queue_task_id": record.QueueTaskID, "status": record.Status, "processed_rows": record.ProcessedRows, "error_count": record.ErrorCount,
		"last_error_code": record.LastErrorCode, "download_key": record.DownloadKey, "expires_at": record.ExpiresAt, "finished_at": record.FinishedAt, "updated_at": record.UpdatedAt,
	})
	if result.Error != nil {
		return importsapp.Job{}, result.Error
	}
	if result.RowsAffected == 0 {
		return importsapp.Job{}, importsapp.ErrJobNotFound
	}
	return job, nil
}

func (r *GORMRepository) AddErrors(ctx context.Context, jobID string, items []importsapp.RowError) error {
	if r == nil || r.db == nil {
		return importsapp.ErrJobNotFound
	}
	for _, item := range items {
		record := errorRecord{ID: newErrorID(jobID, item.Row, item.Column), JobID: jobID, RowNumber: item.Row, ColumnName: item.Column, Code: item.Code, MessageKey: item.MessageKey, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		if err := r.db.Write(ctx).Create(&record).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *GORMRepository) ListErrors(ctx context.Context, jobID, tenantID, orgID string) ([]importsapp.RowError, error) {
	if r == nil || r.db == nil {
		return nil, importsapp.ErrJobNotFound
	}
	query := r.db.Read(ctx).Table("import_export_errors AS e").Select("e.id, e.job_id, e.row_number, e.column_name, e.code, e.message_key, e.deleted_at, e.created_at, e.updated_at").Joins("JOIN import_export_jobs AS j ON j.id = e.job_id").Where("e.job_id = ? AND e.deleted_at IS NULL AND j.tenant_id = ? AND j.deleted_at IS NULL", jobID, tenantID)
	if strings.TrimSpace(orgID) != "" {
		query = query.Where("j.org_id = ?", orgID)
	}
	var rows []errorRecord
	if err := query.Order("e.row_number ASC, e.id ASC").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]importsapp.RowError, 0, len(rows))
	for _, row := range rows {
		out = append(out, importsapp.RowError{Row: row.RowNumber, Column: row.ColumnName, Code: row.Code, MessageKey: row.MessageKey})
	}
	return out, nil
}

func fromJob(job importsapp.Job) jobRecord {
	return jobRecord{ID: job.ID, Kind: job.Kind, TenantID: job.TenantID, OrgID: job.OrgID, ActorID: job.ActorID, PreviewID: job.PreviewID, QueueTaskID: job.QueueTaskID, IdempotencyKey: job.IdempotencyKey, Status: job.Status, TotalRows: job.TotalRows, ProcessedRows: job.ProcessedRows, ErrorCount: job.ErrorCount, LastErrorCode: job.LastErrorCode, ExpiresAt: job.ExpiresAt, FinishedAt: job.FinishedAt, DeletedAt: job.DeletedAt, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt}
}

func toJob(row jobRecord) importsapp.Job {
	return importsapp.Job{ID: row.ID, Kind: row.Kind, TenantID: row.TenantID, OrgID: row.OrgID, ActorID: row.ActorID, PreviewID: row.PreviewID, QueueTaskID: row.QueueTaskID, IdempotencyKey: row.IdempotencyKey, Status: row.Status, TotalRows: row.TotalRows, ProcessedRows: row.ProcessedRows, ErrorCount: row.ErrorCount, LastErrorCode: row.LastErrorCode, ExpiresAt: row.ExpiresAt, FinishedAt: row.FinishedAt, DeletedAt: row.DeletedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func isConflict(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "duplicate") || strings.Contains(text, "unique") || strings.Contains(text, "constraint")
}

func newErrorID(jobID string, row int, column string) string {
	return jobID + ":" + time.Now().UTC().Format("20060102150405.000000000") + ":" + strings.ReplaceAll(column, ":", "_") + ":" + time.Duration(row).String()
}

var _ importsapp.Repository = (*GORMRepository)(nil)
