// Package dashboard aggregates bounded, tenant-scoped management facts for
// the home page. It never manufactures sample values: unavailable collectors
// are represented without a value.
package dashboard

import (
	"context"
	"time"

	auditapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/audit"
	fileapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/file"
	importsapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/imports"
	mailapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/mail"
	monitorapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/monitor"
	tasksapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/tasks"
	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

type Status string

const (
	StatusOK          Status = "ok"
	StatusDegraded    Status = "degraded"
	StatusUnavailable Status = "unavailable"
)

type IAMReader interface {
	ListUsersPage(context.Context, domain.UserListQuery) (domain.UserPage, error)
	ListRoles(context.Context) ([]domain.Role, error)
}

type TaskReader interface {
	List(context.Context) ([]tasksapp.TaskDefinition, error)
}

type ImportExportReader interface {
	List(context.Context, string) ([]importsapp.Job, error)
}

type FileReader interface {
	List(context.Context, fileapp.ListFilter) (fileapp.Page, error)
}

type AuditReader interface {
	Query(context.Context, auditapp.Filter) (auditapp.Page, error)
}

type MailReader interface {
	ListAccounts(context.Context) ([]mailapp.Account, error)
	ListMessages(context.Context, mailapp.MessageFilter) (mailapp.MessagePage, error)
}

type MonitorReader interface {
	Overview(context.Context) (monitorapp.Overview, error)
}

type Config struct {
	IAM          IAMReader
	Tasks        TaskReader
	ImportExport ImportExportReader
	Files        FileReader
	Audit        AuditReader
	Mail         MailReader
	Monitor      MonitorReader
	Timeout      time.Duration
	Clock        func() time.Time
	// DataSource defaults to live. Fixture is intentionally opt-in so deployed
	// installations never present sample analytics as operational facts.
	DataSource DataSource
}

type CountMetric struct {
	Status  Status `json:"status"`
	Value   *int64 `json:"value,omitempty"`
	Message string `json:"message,omitempty"`
}

type Counts struct {
	Users        CountMetric `json:"users"`
	Roles        CountMetric `json:"roles"`
	Tasks        CountMetric `json:"tasks"`
	ImportJobs   CountMetric `json:"importJobs"`
	ExportJobs   CountMetric `json:"exportJobs"`
	Files        CountMetric `json:"files"`
	AuditEvents  CountMetric `json:"auditEvents"`
	MailAccounts CountMetric `json:"mailAccounts"`
	MailMessages CountMetric `json:"mailMessages"`
}

type HealthMetric struct {
	Status Status `json:"status"`
	State  string `json:"state,omitempty"`
}

type Health struct {
	Runtime  HealthMetric `json:"runtime"`
	Database HealthMetric `json:"database"`
	Redis    HealthMetric `json:"redis"`
}

type InstanceMetric struct {
	Status        Status   `json:"status"`
	State         string   `json:"state,omitempty"`
	Scope         *string  `json:"scope,omitempty"`
	Version       *string  `json:"version,omitempty"`
	UptimeSeconds *float64 `json:"uptimeSeconds,omitempty"`
}

type Summary struct {
	Status      Status         `json:"status"`
	Counts      Counts         `json:"counts"`
	Instance    InstanceMetric `json:"instance"`
	Health      Health         `json:"health"`
	CollectedAt time.Time      `json:"collectedAt"`
}

type Service struct {
	config  Config
	timeout time.Duration
	clock   func() time.Time
}

func NewService(config Config) *Service {
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}
	return &Service{config: config, timeout: timeout, clock: clock}
}

func (s *Service) Summary(ctx context.Context) (Summary, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return Summary{}, err
	}
	// Dashboard values are always a scoped slice, even for a platform
	// administrator. Repositories must not interpret this aggregation as a
	// request for cross-tenant or cross-organization totals.
	scope.PlatformAdmin = false
	ctx = tenant.WithContext(ctx, scope)
	users := asyncCount(ctx, s.timeout, s.config.IAM != nil, func(itemCtx context.Context) (int64, error) {
		page, err := s.config.IAM.ListUsersPage(itemCtx, domain.UserListQuery{Page: 1, PageSize: 1})
		return int64(page.Total), err
	})
	roles := asyncCount(ctx, s.timeout, s.config.IAM != nil, func(itemCtx context.Context) (int64, error) {
		items, err := s.config.IAM.ListRoles(itemCtx)
		return int64(len(items)), err
	})
	tasks := asyncCount(ctx, s.timeout, s.config.Tasks != nil, func(itemCtx context.Context) (int64, error) {
		items, err := s.config.Tasks.List(itemCtx)
		return int64(len(items)), err
	})
	importJobs := asyncCount(ctx, s.timeout, s.config.ImportExport != nil, func(itemCtx context.Context) (int64, error) {
		items, err := s.config.ImportExport.List(itemCtx, importsapp.JobKindImport)
		return int64(len(items)), err
	})
	exportJobs := asyncCount(ctx, s.timeout, s.config.ImportExport != nil, func(itemCtx context.Context) (int64, error) {
		items, err := s.config.ImportExport.List(itemCtx, importsapp.JobKindExport)
		return int64(len(items)), err
	})
	files := asyncCount(ctx, s.timeout, s.config.Files != nil, func(itemCtx context.Context) (int64, error) {
		page, err := s.config.Files.List(itemCtx, fileapp.ListFilter{TenantID: scope.TenantID, OrgID: scope.Organization, Limit: 1})
		return int64(page.Total), err
	})
	auditEvents := asyncCount(ctx, s.timeout, s.config.Audit != nil, func(itemCtx context.Context) (int64, error) {
		page, err := s.config.Audit.Query(itemCtx, auditapp.Filter{Limit: 1})
		return int64(page.Total), err
	})
	mailAccounts := asyncCount(ctx, s.timeout, s.config.Mail != nil, func(itemCtx context.Context) (int64, error) {
		items, err := s.config.Mail.ListAccounts(itemCtx)
		return int64(len(items)), err
	})
	mailMessages := asyncCount(ctx, s.timeout, s.config.Mail != nil, func(itemCtx context.Context) (int64, error) {
		page, err := s.config.Mail.ListMessages(itemCtx, mailapp.MessageFilter{Limit: 1})
		return int64(page.Total), err
	})
	monitorSummary := asyncMonitorSummary(ctx, s.timeout, s.config.Monitor)

	summary := Summary{CollectedAt: s.clock().UTC()}
	summary.Counts = Counts{
		Users: <-users, Roles: <-roles, Tasks: <-tasks,
		ImportJobs: <-importJobs, ExportJobs: <-exportJobs, Files: <-files,
		AuditEvents: <-auditEvents, MailAccounts: <-mailAccounts, MailMessages: <-mailMessages,
	}
	collectedMonitor := <-monitorSummary
	summary.Instance = collectedMonitor.instance
	summary.Health = collectedMonitor.health
	summary.Status = summaryStatus(summary)
	return summary, nil
}

type countResult struct {
	value int64
	err   error
}

func asyncCount(ctx context.Context, timeout time.Duration, configured bool, collect func(context.Context) (int64, error)) <-chan CountMetric {
	output := make(chan CountMetric, 1)
	if !configured {
		output <- CountMetric{Status: StatusUnavailable, Message: "collector not configured"}
		return output
	}
	go func() {
		itemCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		result := make(chan countResult, 1)
		go func() {
			value, err := collect(itemCtx)
			result <- countResult{value: value, err: err}
		}()
		select {
		case collected := <-result:
			if collected.err != nil {
				output <- CountMetric{Status: StatusDegraded, Message: "collector unavailable"}
				return
			}
			output <- CountMetric{Status: StatusOK, Value: pointer(collected.value)}
		case <-itemCtx.Done():
			output <- CountMetric{Status: StatusDegraded, Message: "collector timed out"}
		}
	}()
	return output
}

type collectedMonitorSummary struct {
	instance InstanceMetric
	health   Health
}

func asyncMonitorSummary(ctx context.Context, timeout time.Duration, monitor MonitorReader) <-chan collectedMonitorSummary {
	output := make(chan collectedMonitorSummary, 1)
	unavailableHealth := Health{
		Runtime: HealthMetric{Status: StatusUnavailable}, Database: HealthMetric{Status: StatusUnavailable}, Redis: HealthMetric{Status: StatusUnavailable},
	}
	if monitor == nil {
		output <- collectedMonitorSummary{instance: InstanceMetric{Status: StatusUnavailable}, health: unavailableHealth}
		return output
	}
	go func() {
		itemCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		type result struct {
			overview monitorapp.Overview
			err      error
		}
		resultCh := make(chan result, 1)
		go func() {
			overview, err := monitor.Overview(itemCtx)
			resultCh <- result{overview: overview, err: err}
		}()
		select {
		case collected := <-resultCh:
			if collected.err != nil {
				output <- degradedMonitorSummary()
				return
			}
			scope := string(collected.overview.Scope)
			instance := InstanceMetric{
				Status: monitorStatus(collected.overview.Runtime.Status), State: string(collected.overview.Runtime.Status), Scope: pointer(scope), UptimeSeconds: pointer(collected.overview.UptimeSeconds),
			}
			if collected.overview.Version != "" {
				instance.Version = pointer(collected.overview.Version)
			}
			output <- collectedMonitorSummary{instance: instance, health: Health{
				Runtime:  HealthMetric{Status: monitorStatus(collected.overview.Runtime.Status), State: string(collected.overview.Runtime.Status)},
				Database: HealthMetric{Status: monitorStatus(collected.overview.Database.Status), State: string(collected.overview.Database.Status)},
				Redis:    HealthMetric{Status: monitorStatus(collected.overview.Redis.Status), State: string(collected.overview.Redis.Status)},
			}}
		case <-itemCtx.Done():
			output <- degradedMonitorSummary()
		}
	}()
	return output
}

func monitorStatus(status monitorapp.Status) Status {
	switch status {
	case monitorapp.StatusOK:
		return StatusOK
	case monitorapp.StatusDegraded:
		return StatusDegraded
	default:
		return StatusUnavailable
	}
}

func degradedMonitorSummary() collectedMonitorSummary {
	return collectedMonitorSummary{
		instance: InstanceMetric{Status: StatusDegraded},
		health:   Health{Runtime: HealthMetric{Status: StatusDegraded}, Database: HealthMetric{Status: StatusDegraded}, Redis: HealthMetric{Status: StatusDegraded}},
	}
}

func summaryStatus(summary Summary) Status {
	metrics := []Status{
		summary.Counts.Users.Status, summary.Counts.Roles.Status, summary.Counts.Tasks.Status,
		summary.Counts.ImportJobs.Status, summary.Counts.ExportJobs.Status, summary.Counts.Files.Status,
		summary.Counts.AuditEvents.Status, summary.Counts.MailAccounts.Status, summary.Counts.MailMessages.Status,
		summary.Health.Runtime.Status, summary.Health.Database.Status, summary.Health.Redis.Status,
		summary.Instance.Status,
	}
	ok := 0
	for _, status := range metrics {
		if status == StatusOK {
			ok++
		}
	}
	if ok == len(metrics) {
		return StatusOK
	}
	if ok == 0 {
		return StatusUnavailable
	}
	return StatusDegraded
}

func pointer[T any](value T) *T { return &value }
