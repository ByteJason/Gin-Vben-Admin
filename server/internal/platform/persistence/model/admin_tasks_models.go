// Package model contains the persistence-side GORM models.
//
// Files are grouped by capability (shared, identity, admin, audit and
// infrastructure) while remaining one Go package so migrations and repositories
// share exactly one set of column definitions. Relationship IDs intentionally
// remain scalar fields; ORM read relations live outside the migration registry.
package model

import "time"

type TaskDefinition struct {
	ID                string     `gorm:"column:id;size:64;primaryKey;comment:任务标识"`
	TenantID          string     `gorm:"column:tenant_id;size:128;not null;uniqueIndex:uq_task_definitions_scope_name,priority:1;index:idx_task_definitions_scope_enabled,priority:1;comment:租户标识"`
	OrgID             string     `gorm:"column:org_id;size:128;not null;default:'';uniqueIndex:uq_task_definitions_scope_name,priority:2;index:idx_task_definitions_scope_enabled,priority:2;comment:组织标识"`
	Name              string     `gorm:"column:name;size:191;not null;uniqueIndex:uq_task_definitions_scope_name,priority:3;comment:任务名称"`
	Type              string     `gorm:"column:type;size:32;not null;check:chk_task_definitions_type,type IN ('manual','http','webhook');comment:任务类型"`
	PayloadSchema     JSONValue  `gorm:"column:payload_schema;not null;comment:负载定义"`
	Cron              string     `gorm:"column:cron;size:128;not null;default:'';comment:定时表达式"`
	Timezone          string     `gorm:"column:timezone;size:64;not null;default:UTC;comment:时区"`
	Enabled           bool       `gorm:"column:enabled;not null;default:true;index:idx_task_definitions_scope_enabled,priority:3;comment:是否启用"`
	Concurrency       int32      `gorm:"column:concurrency;not null;default:1;check:chk_task_definitions_concurrency,concurrency > 0;comment:并发数"`
	ConcurrencyPolicy string     `gorm:"column:concurrency_policy;size:16;not null;default:forbid;check:chk_task_definitions_policy,concurrency_policy IN ('allow','forbid','replace');comment:并发策略"`
	TimeoutMS         int64      `gorm:"column:timeout_ms;not null;default:30000;comment:超时毫秒数"`
	MaxAttempts       int32      `gorm:"column:max_attempts;not null;default:1;check:chk_task_definitions_attempts,max_attempts > 0;comment:最大尝试次数"`
	IdempotencyKey    string     `gorm:"column:idempotency_key;size:191;not null;default:'';comment:幂等键"`
	DeletedAt         *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
	CreatedAt         time.Time  `gorm:"column:created_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:创建时间"`
	UpdatedAt         time.Time  `gorm:"column:updated_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:更新时间"`
}

func (TaskDefinition) TableName() string { return "task_definitions" }

type TaskRun struct {
	ID             string     `gorm:"column:id;size:64;primaryKey;comment:运行标识"`
	TaskID         string     `gorm:"column:task_id;size:64;not null;index:idx_task_runs_scope_task_status,priority:3;comment:任务标识"`
	TenantID       string     `gorm:"column:tenant_id;size:128;not null;uniqueIndex:uq_task_runs_scope_idempotency,priority:1;index:idx_task_runs_scope_task_status,priority:1;comment:租户标识"`
	OrgID          string     `gorm:"column:org_id;size:128;not null;default:'';uniqueIndex:uq_task_runs_scope_idempotency,priority:2;index:idx_task_runs_scope_task_status,priority:2;comment:组织标识"`
	QueueTaskID    string     `gorm:"column:queue_task_id;size:64;not null;default:'';index:idx_task_runs_queue_task;comment:队列任务标识"`
	IdempotencyKey string     `gorm:"column:idempotency_key;size:191;not null;uniqueIndex:uq_task_runs_scope_idempotency,priority:3;comment:幂等键"`
	Status         string     `gorm:"column:status;size:32;not null;default:pending;index:idx_task_runs_scope_task_status,priority:4;check:chk_task_runs_status,status IN ('pending','running','succeeded','failed','dead_letter','cancelled');comment:运行状态"`
	PayloadDigest  string     `gorm:"column:payload_digest;type:char(64);not null;comment:负载摘要"`
	AttemptCount   int32      `gorm:"column:attempt_count;not null;default:0;check:chk_task_runs_attempts,attempt_count >= 0 AND max_attempts > 0;comment:尝试次数"`
	MaxAttempts    int32      `gorm:"column:max_attempts;not null;default:1;comment:最大尝试次数"`
	LastErrorCode  string     `gorm:"column:last_error_code;size:128;not null;default:'';comment:最近错误码"`
	StartedAt      *time.Time `gorm:"column:started_at;precision:6;comment:开始时间"`
	FinishedAt     *time.Time `gorm:"column:finished_at;precision:6;comment:完成时间"`
	DeletedAt      *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
	CreatedAt      time.Time  `gorm:"column:created_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);index:idx_task_runs_scope_task_status,priority:5;comment:创建时间"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:更新时间"`
}

func (TaskRun) TableName() string { return "task_runs" }

type TaskRunLog struct {
	ID        string     `gorm:"column:id;size:64;primaryKey;comment:日志标识"`
	RunID     string     `gorm:"column:run_id;size:64;not null;index:idx_task_run_logs_run_created,priority:1;comment:运行标识"`
	Attempt   int32      `gorm:"column:attempt;not null;default:0;comment:尝试序号"`
	Status    string     `gorm:"column:status;size:32;not null;check:chk_task_run_logs_status,status IN ('pending','running','succeeded','failed','dead_letter','cancelled');comment:运行状态"`
	ErrorCode string     `gorm:"column:error_code;size:128;not null;default:'';comment:错误码"`
	Message   string     `gorm:"column:message;size:512;not null;default:'';comment:日志消息"`
	DeletedAt *time.Time `gorm:"column:deleted_at;precision:6;comment:删除时间"`
	CreatedAt time.Time  `gorm:"column:created_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);index:idx_task_run_logs_run_created,priority:2;comment:创建时间"`
	UpdatedAt time.Time  `gorm:"column:updated_at;precision:6;not null;default:CURRENT_TIMESTAMP(6);comment:更新时间"`
}

func (TaskRunLog) TableName() string { return "task_run_logs" }
