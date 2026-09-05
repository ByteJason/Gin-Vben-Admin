// Package importsplatform contains the durable IMPORT-100 database adapter.
package importsplatform

import (
	"context"
	"errors"
	"strings"
	"time"

	importsapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/imports"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GORMRepository struct{ db *gormdb.Store }

func NewGORMRepository(db *gormdb.Store) *GORMRepository { return &GORMRepository{db: db} }

type jobRecord = model.ImportExportJob
type errorRecord = model.ImportExportError

func (r *GORMRepository) Create(ctx context.Context, job importsapp.Job) (importsapp.Job, error) {
	if r == nil || r.db == nil {
		return importsapp.Job{}, importsapp.ErrJobNotFound
	}
	record := fromJob(job)
	if err := gorm.G[jobRecord](r.db.Write(ctx)).Create(ctx, &record); err != nil {
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
	query := gorm.G[jobRecord](r.db.Read(ctx)).Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", strings.TrimSpace(id), tenantID)
	if strings.TrimSpace(orgID) != "" {
		query = query.Where("org_id = ?", orgID)
	}
	row, err := query.First(ctx)
	if err != nil {
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
	query := gorm.G[jobRecord](r.db.Read(ctx)).Where("tenant_id = ? AND deleted_at IS NULL", tenantID)
	if strings.TrimSpace(orgID) != "" {
		query = query.Where("org_id = ?", orgID)
	}
	if strings.TrimSpace(kind) != "" {
		query = query.Where("kind = ?", kind)
	}
	rows, err := query.Order("created_at DESC, id DESC").Find(ctx)
	if err != nil {
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
	rows, updateErr := gorm.G[jobRecord](r.db.Write(ctx)).Where("id = ? AND tenant_id = ? AND org_id = ? AND deleted_at IS NULL", record.ID, record.TenantID, record.OrgID).Set(clause.Assignments(map[string]any{
		"queue_task_id": record.QueueTaskID, "status": record.Status, "processed_rows": record.ProcessedRows, "error_count": record.ErrorCount,
		"last_error_code": record.LastErrorCode, "download_key": record.DownloadKey, "expires_at": record.ExpiresAt, "finished_at": record.FinishedAt, "updated_at": record.UpdatedAt,
	})).Update(ctx)
	if updateErr != nil {
		return importsapp.Job{}, updateErr
	}
	if rows == 0 {
		return importsapp.Job{}, importsapp.ErrJobNotFound
	}
	return job, nil
}

func (r *GORMRepository) AddErrors(ctx context.Context, jobID string, items []importsapp.RowError) error {
	if r == nil || r.db == nil {
		return importsapp.ErrJobNotFound
	}
	for _, item := range items {
		record := errorRecord{ID: newErrorID(jobID, item.Row, item.Column), JobID: jobID, RowNumber: int32(item.Row), ColumnName: item.Column, Code: item.Code, MessageKey: item.MessageKey, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		if err := gorm.G[errorRecord](r.db.Write(ctx)).Create(ctx, &record); err != nil {
			return err
		}
	}
	return nil
}

func (r *GORMRepository) ListErrors(ctx context.Context, jobID, tenantID, orgID string) ([]importsapp.RowError, error) {
	if r == nil || r.db == nil {
		return nil, importsapp.ErrJobNotFound
	}
	// Dedicated projection (fixed SQL allowlist): the parent-job join enforces
	// tenant/organization scope while returning only public row-error fields;
	// runtime values are always passed as bound arguments below.
	sql := "SELECT e.id, e.job_id, e.row_number, e.column_name, e.code, e.message_key, e.deleted_at, e.created_at, e.updated_at FROM gvba_import_errors AS e JOIN gvba_import_jobs AS j ON j.id = e.job_id WHERE e.job_id = ? AND e.deleted_at IS NULL AND j.tenant_id = ? AND j.deleted_at IS NULL"
	args := []any{jobID, tenantID}
	if strings.TrimSpace(orgID) != "" {
		sql += " AND j.org_id = ?"
		args = append(args, orgID)
	}
	sql += " ORDER BY e.row_number ASC, e.id ASC"
	rows, err := gorm.G[errorRecord](r.db.Read(ctx)).Raw(sql, args...).Find(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]importsapp.RowError, 0, len(rows))
	for _, row := range rows {
		out = append(out, importsapp.RowError{Row: int(row.RowNumber), Column: row.ColumnName, Code: row.Code, MessageKey: row.MessageKey})
	}
	return out, nil
}

func fromJob(job importsapp.Job) jobRecord {
	return jobRecord{ID: job.ID, Kind: job.Kind, TenantID: job.TenantID, OrgID: job.OrgID, ActorID: job.ActorID, PreviewID: job.PreviewID, QueueTaskID: job.QueueTaskID, IdempotencyKey: job.IdempotencyKey, Status: job.Status, TotalRows: int32(job.TotalRows), ProcessedRows: int32(job.ProcessedRows), ErrorCount: int32(job.ErrorCount), LastErrorCode: job.LastErrorCode, ExpiresAt: job.ExpiresAt, FinishedAt: job.FinishedAt, DeletedAt: job.DeletedAt, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt}
}

func toJob(row jobRecord) importsapp.Job {
	return importsapp.Job{ID: row.ID, Kind: row.Kind, TenantID: row.TenantID, OrgID: row.OrgID, ActorID: row.ActorID, PreviewID: row.PreviewID, QueueTaskID: row.QueueTaskID, IdempotencyKey: row.IdempotencyKey, Status: row.Status, TotalRows: int(row.TotalRows), ProcessedRows: int(row.ProcessedRows), ErrorCount: int(row.ErrorCount), LastErrorCode: row.LastErrorCode, ExpiresAt: row.ExpiresAt, FinishedAt: row.FinishedAt, DeletedAt: row.DeletedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
}

func isConflict(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "duplicate") || strings.Contains(text, "unique") || strings.Contains(text, "constraint")
}

func newErrorID(jobID string, row int, column string) string {
	return jobID + ":" + time.Now().UTC().Format("20060102150405.000000000") + ":" + strings.ReplaceAll(column, ":", "_") + ":" + time.Duration(row).String()
}

var _ importsapp.Repository = (*GORMRepository)(nil)
