// Package model contains the persistence-side GORM models.
//
// Files are grouped by capability (shared, identity, admin, audit and
// infrastructure) while remaining one Go package so migrations and repositories
// share exactly one set of column definitions. Relationship IDs intentionally
// remain scalar fields; ORM read relations live outside the migration registry.
package model

import "time"

type ImportExportJob struct {
	ID             string     `gorm:"column:id;size:64;primaryKey;comment:作业标识"`
	Kind           string     `gorm:"column:kind;size:16;not null;index:idx_gvba_import_jobs_scope_status,priority:3;check:chk_gvba_import_jobs_kind,kind IN ('import','export');comment:作业类型"`
	TenantID       string     `gorm:"column:tenant_id;size:128;not null;uniqueIndex:uq_gvba_import_jobs_scope_idempotency,priority:1;index:idx_gvba_import_jobs_scope_status,priority:1;comment:租户标识"`
	OrgID          string     `gorm:"column:org_id;size:128;not null;default:'';uniqueIndex:uq_gvba_import_jobs_scope_idempotency,priority:2;index:idx_gvba_import_jobs_scope_status,priority:2;comment:组织标识"`
	ActorID        string     `gorm:"column:actor_id;size:128;not null;default:'';comment:操作者标识"`
	PreviewID      string     `gorm:"column:preview_id;size:64;not null;default:'';comment:预览标识"`
	QueueTaskID    string     `gorm:"column:queue_task_id;size:64;not null;default:'';index:idx_gvba_import_jobs_queue_task;comment:队列任务标识"`
	IdempotencyKey string     `gorm:"column:idempotency_key;size:191;not null;uniqueIndex:uq_gvba_import_jobs_scope_idempotency,priority:3;comment:幂等键"`
	Status         string     `gorm:"column:status;size:32;not null;default:pending;index:idx_gvba_import_jobs_scope_status,priority:4;check:chk_gvba_import_jobs_status,status IN ('pending','running','succeeded','failed','cancelled');comment:作业状态"`
	Format         string     `gorm:"column:format;size:16;not null;default:csv;comment:文件格式"`
	TotalRows      int32      `gorm:"column:total_rows;not null;default:0;comment:总行数"`
	ProcessedRows  int32      `gorm:"column:processed_rows;not null;default:0;check:chk_gvba_import_jobs_progress,processed_rows <= total_rows;comment:已处理行数"`
	ErrorCount     int32      `gorm:"column:error_count;not null;default:0;comment:错误数量"`
	LastErrorCode  string     `gorm:"column:last_error_code;size:128;not null;default:'';comment:最近错误码"`
	DownloadKey    string     `gorm:"column:download_key;size:255;not null;default:'';comment:下载对象键"`
	ExpiresAt      *time.Time `gorm:"column:expires_at;precision:6;comment:过期时间"`
	FinishedAt     *time.Time `gorm:"column:finished_at;precision:6;comment:完成时间"`
	DeletedAt      *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
	CreatedAt      time.Time  `gorm:"column:created_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);index:idx_gvba_import_jobs_scope_status,priority:5;comment:创建时间"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:更新时间"`
}

func (ImportExportJob) TableName() string { return "gvba_import_jobs" }

type ImportExportError struct {
	ID         string     `gorm:"column:id;size:64;primaryKey;comment:错误标识"`
	JobID      string     `gorm:"column:job_id;size:64;not null;index:idx_gvba_import_errors_job_row,priority:1;comment:作业标识"`
	RowNumber  int32      `gorm:"column:row_number;not null;index:idx_gvba_import_errors_job_row,priority:2;comment:行号"`
	ColumnName string     `gorm:"column:column_name;size:128;not null;default:'';comment:列名"`
	Code       string     `gorm:"column:code;size:128;not null;comment:错误码"`
	MessageKey string     `gorm:"column:message_key;size:191;not null;comment:消息键"`
	DeletedAt  *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
	CreatedAt  time.Time  `gorm:"column:created_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);index:idx_gvba_import_errors_job_row,priority:3;comment:创建时间"`
	UpdatedAt  time.Time  `gorm:"column:updated_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:更新时间"`
}

func (ImportExportError) TableName() string { return "gvba_import_errors" }

type ImportExportArtifact struct {
	ID        string     `gorm:"column:id;size:64;primaryKey;comment:工件标识"`
	JobID     string     `gorm:"column:job_id;size:64;not null;uniqueIndex:uq_gvba_import_artifacts_job;comment:作业标识"`
	ObjectKey string     `gorm:"column:object_key;size:255;not null;comment:对象键"`
	SHA256    string     `gorm:"column:sha256;type:char(64);not null;comment:内容摘要"`
	SizeBytes uint64     `gorm:"column:size_bytes;not null;default:0;comment:工件大小"`
	ExpiresAt *time.Time `gorm:"column:expires_at;precision:6;index:idx_gvba_import_artifacts_expiry;comment:过期时间"`
	DeletedAt *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
	CreatedAt time.Time  `gorm:"column:created_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:创建时间"`
	UpdatedAt time.Time  `gorm:"column:updated_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:更新时间"`
}

func (ImportExportArtifact) TableName() string { return "gvba_import_artifacts" }
