// Package imports implements the provider-independent IMPORT-100 seam.
//
// The service owns bounded parsing, column/permission validation, idempotent
// import/export job state and redacted error artifacts.  Queue adapters invoke
// ProcessImport/ProcessExport through explicit worker registrations; request
// payloads are never interpreted as executable code.
package imports

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"example.com/gin-vben-admin/server/internal/application/jobs"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
)

const (
	JobTypeImport = "import.process"
	JobTypeExport = "export.process"

	JobKindImport = "import"
	JobKindExport = "export"

	JobPending   = "pending"
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
	JobCancelled = "cancelled"
)

var (
	ErrInvalidRequest   = errors.New("invalid import/export request")
	ErrFileTooLarge     = errors.New("import file exceeds configured size limit")
	ErrTooManyRows      = errors.New("import file exceeds configured row limit")
	ErrInvalidFormat    = errors.New("unsupported import format")
	ErrJobNotFound      = errors.New("import/export job not found")
	ErrJobConflict      = errors.New("import/export job already exists")
	ErrJobStateConflict = errors.New("import/export job state conflict")
	ErrPreviewNotFound  = errors.New("import preview not found")
	ErrColumnDenied     = errors.New("import column is not allowed")
	ErrVirusDetected    = errors.New("import file failed security scan")
)

// Limits are deliberately explicit so deployment can override them without
// changing the public service seam.
type Limits struct {
	MaxFileBytes int64
	MaxRows      int
	PreviewRows  int
	BatchSize    int
	DownloadTTL  time.Duration
}

func DefaultLimits() Limits {
	return Limits{MaxFileBytes: 50 << 20, MaxRows: 100_000, PreviewRows: 100, BatchSize: 500, DownloadTTL: 15 * time.Minute}
}

type PermissionChecker func(context.Context, string) error
type VirusScanner func(context.Context, []byte) error
type AuditSink interface {
	Record(context.Context, AuditEvent) error
}

type AuditEvent struct {
	Action      string
	JobID       string
	TenantID    string
	OrgID       string
	ActorID     string
	Status      string
	ErrorCount  int
	CreatedAt   time.Time
	ResourceKey string
}

type Config struct {
	Limits      Limits
	Clock       func() time.Time
	Permission  PermissionChecker
	VirusScan   VirusScanner
	Audit       AuditSink
	Queue       jobs.Queue
	Repository  Repository
	DownloadURL func(context.Context, string, time.Duration) (string, error)
}

type Request struct {
	TenantID       string
	OrgID          string
	ActorID        string
	IdempotencyKey string
	Format         string
	Name           string
	MIME           string
	Columns        []string
	Required       []string
	Allowlist      map[string]bool
	Types          map[string]string
	CSV            io.Reader
	Data           []byte
}

type CommitRequest struct {
	TenantID       string
	OrgID          string
	ActorID        string
	PreviewID      string
	IdempotencyKey string
}

type ExportRequest struct {
	TenantID       string
	OrgID          string
	ActorID        string
	IdempotencyKey string
	Fields         []string
	Allowlist      map[string]bool
	Rows           []map[string]string
	Redact         func(string, string) string
}

type RowError struct {
	Row        int    `json:"row"`
	Column     string `json:"column,omitempty"`
	Code       string `json:"code"`
	MessageKey string `json:"messageKey"`
}

type PreviewResult struct {
	ID            string              `json:"id"`
	TenantID      string              `json:"tenantId"`
	OrgID         string              `json:"orgId,omitempty"`
	Format        string              `json:"format"`
	Headers       []string            `json:"headers"`
	MappedColumns map[string]string   `json:"mappedColumns"`
	PreviewRows   []map[string]string `json:"previewRows"`
	TotalRows     int                 `json:"totalRows"`
	Errors        []RowError          `json:"errors"`
	SizeBytes     int64               `json:"sizeBytes"`
	SHA256        string              `json:"sha256"`
	CreatedAt     time.Time           `json:"createdAt"`
	ExpiresAt     time.Time           `json:"expiresAt"`
}

type Job struct {
	ID             string     `json:"id"`
	Kind           string     `json:"kind"`
	TenantID       string     `json:"tenantId"`
	OrgID          string     `json:"orgId,omitempty"`
	ActorID        string     `json:"actorId,omitempty"`
	PreviewID      string     `json:"previewId,omitempty"`
	QueueTaskID    string     `json:"queueTaskId,omitempty"`
	IdempotencyKey string     `json:"idempotencyKey"`
	Status         string     `json:"status"`
	TotalRows      int        `json:"totalRows"`
	ProcessedRows  int        `json:"processedRows"`
	ErrorCount     int        `json:"errorCount"`
	LastErrorCode  string     `json:"lastErrorCode,omitempty"`
	DownloadURL    string     `json:"downloadUrl,omitempty"`
	ExpiresAt      *time.Time `json:"expiresAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	FinishedAt     *time.Time `json:"finishedAt,omitempty"`
	DeletedAt      *time.Time `json:"deletedAt,omitempty"`
}

type storedPreview struct {
	result PreviewResult
	data   []byte
	rows   []map[string]string
}

type Service struct {
	mu          sync.RWMutex
	limits      Limits
	clock       func() time.Time
	permission  PermissionChecker
	virusScan   VirusScanner
	audit       AuditSink
	downloadURL func(context.Context, string, time.Duration) (string, error)
	queue       jobs.Queue
	repository  Repository
	previews    map[string]storedPreview
	jobs        map[string]Job
	jobData     map[string][]map[string]string
	errors      map[string][]RowError
	artifacts   map[string][]byte
}

// NewService accepts either Config or Limits.  The Limits form keeps the
// compact local-fixture seam convenient while Config is used by composition
// roots that need scanner/audit/download hooks.
func NewService(input interface{}) *Service {
	config := Config{}
	switch value := input.(type) {
	case Config:
		config = value
	case Limits:
		config.Limits = value
	case nil:
		// defaults below
	default:
		// Keep construction deterministic for malformed optional wiring.
		config = Config{}
	}
	limits := config.Limits
	defaults := DefaultLimits()
	if limits.MaxFileBytes <= 0 {
		limits.MaxFileBytes = defaults.MaxFileBytes
	}
	if limits.MaxRows <= 0 {
		limits.MaxRows = defaults.MaxRows
	}
	if limits.PreviewRows <= 0 {
		limits.PreviewRows = defaults.PreviewRows
	}
	if limits.BatchSize <= 0 {
		limits.BatchSize = defaults.BatchSize
	}
	if limits.DownloadTTL <= 0 {
		limits.DownloadTTL = defaults.DownloadTTL
	}
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}
	return &Service{limits: limits, clock: clock, permission: config.Permission, virusScan: config.VirusScan, audit: config.Audit, queue: config.Queue, repository: config.Repository, downloadURL: config.DownloadURL, previews: map[string]storedPreview{}, jobs: map[string]Job{}, jobData: map[string][]map[string]string{}, errors: map[string][]RowError{}, artifacts: map[string][]byte{}}
}

// Preview is the small, context-free test seam retained for local fixtures.
// HTTP callers should use PreviewContext so tenant context and scanner hooks
// are enforced.
func (s *Service) Preview(req Request) (PreviewResult, error) {
	return s.preview(context.Background(), req)
}

func (s *Service) PreviewContext(ctx context.Context, req Request) (PreviewResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if scope, err := tenant.RequireContext(ctx); err != nil {
		return PreviewResult{}, err
	} else {
		if req.TenantID == "" {
			req.TenantID = scope.TenantID
		}
		if req.OrgID == "" {
			req.OrgID = scope.Organization
		}
	}
	return s.preview(ctx, req)
}

func (s *Service) preview(ctx context.Context, req Request) (PreviewResult, error) {
	if s == nil || strings.TrimSpace(req.TenantID) == "" {
		return PreviewResult{}, ErrInvalidRequest
	}
	data, err := requestData(req, s.limits.MaxFileBytes)
	if err != nil {
		return PreviewResult{}, err
	}
	if s.virusScan != nil {
		if err := s.virusScan(ctx, data); err != nil {
			if errors.Is(err, ErrVirusDetected) {
				return PreviewResult{}, err
			}
			return PreviewResult{}, fmt.Errorf("%w: %v", ErrVirusDetected, err)
		}
	}
	format, err := detectFormat(req.Format, req.Name, req.MIME)
	if err != nil {
		return PreviewResult{}, err
	}
	rows, headers, err := parseRows(data, format, s.limits.MaxRows)
	if err != nil {
		return PreviewResult{}, err
	}
	if s.permission != nil {
		checked := req.Columns
		if len(checked) == 0 {
			checked = headers
		}
		for _, column := range checked {
			if err := s.permission(ctx, strings.TrimSpace(column)); err != nil {
				return PreviewResult{}, err
			}
		}
	}
	allow := normalizeAllowlist(req.Allowlist, req.Columns)
	required := normalizeRequired(req.Required, req.Columns)
	result := PreviewResult{ID: newID("preview"), TenantID: strings.TrimSpace(req.TenantID), OrgID: strings.TrimSpace(req.OrgID), Format: format, Headers: append([]string(nil), headers...), MappedColumns: map[string]string{}, PreviewRows: make([]map[string]string, 0), Errors: make([]RowError, 0), SizeBytes: int64(len(data)), SHA256: digest(data), CreatedAt: s.now(), ExpiresAt: s.now().Add(s.limits.DownloadTTL)}
	for _, header := range headers {
		if allow[header] {
			result.MappedColumns[header] = header
		}
	}
	for i, row := range rows {
		rowNumber := i + 2
		rowMap := make(map[string]string, len(headers))
		for j, header := range headers {
			if j < len(row) {
				rowMap[header] = strings.TrimSpace(row[j])
			}
		}
		rowErrs := validateRow(rowNumber, rowMap, headers, allow, required, req.Types)
		if len(rowErrs) > 0 {
			result.Errors = append(result.Errors, rowErrs...)
		}
		if len(result.PreviewRows) < s.limits.PreviewRows {
			result.PreviewRows = append(result.PreviewRows, redactRow(rowMap))
		}
	}
	result.TotalRows = len(rows)
	s.mu.Lock()
	s.previews[result.ID] = storedPreview{result: clonePreview(result), data: append([]byte(nil), data...), rows: rowsToMaps(rows, headers)}
	s.mu.Unlock()
	return result, nil
}

func (s *Service) Commit(ctx context.Context, req CommitRequest) (Job, error) {
	if s == nil || strings.TrimSpace(req.TenantID) == "" || strings.TrimSpace(req.PreviewID) == "" {
		return Job{}, ErrInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	preview, ok := s.previews[req.PreviewID]
	if !ok || preview.result.TenantID != strings.TrimSpace(req.TenantID) || (req.OrgID != "" && preview.result.OrgID != strings.TrimSpace(req.OrgID)) {
		s.mu.Unlock()
		return Job{}, ErrPreviewNotFound
	}
	key := strings.TrimSpace(req.IdempotencyKey)
	if key == "" {
		key = req.PreviewID
	}
	for _, existing := range s.jobs {
		if existing.DeletedAt == nil && existing.TenantID == req.TenantID && existing.OrgID == req.OrgID && existing.IdempotencyKey == key {
			s.mu.Unlock()
			return cloneJob(existing), nil
		}
	}
	now := s.now()
	job := Job{ID: newID("import"), Kind: JobKindImport, TenantID: strings.TrimSpace(req.TenantID), OrgID: strings.TrimSpace(req.OrgID), ActorID: strings.TrimSpace(req.ActorID), PreviewID: req.PreviewID, IdempotencyKey: key, Status: JobPending, TotalRows: preview.result.TotalRows, ErrorCount: len(preview.result.Errors), CreatedAt: now, UpdatedAt: now}
	s.jobs[job.ID] = job
	s.jobData[job.ID] = cloneRows(preview.rows)
	s.errors[job.ID] = append([]RowError(nil), preview.result.Errors...)
	s.mu.Unlock()
	if s.repository != nil {
		persisted, persistErr := s.repository.Create(ctx, job)
		if persistErr != nil {
			return Job{}, persistErr
		}
		job = persisted
		s.mu.Lock()
		s.jobs[job.ID] = job
		s.mu.Unlock()
		if persistErr := s.repository.AddErrors(ctx, job.ID, preview.result.Errors); persistErr != nil {
			return Job{}, persistErr
		}
	}
	if err := s.enqueue(ctx, &job); err != nil {
		return Job{}, err
	}
	s.record(ctx, "import.commit", job)
	return job, nil
}

func (s *Service) StartExport(ctx context.Context, req ExportRequest) (Job, error) {
	if s == nil || strings.TrimSpace(req.TenantID) == "" || len(req.Fields) == 0 {
		return Job{}, ErrInvalidRequest
	}
	allow := normalizeAllowlist(req.Allowlist, req.Fields)
	for _, field := range req.Fields {
		if !allow[field] {
			return Job{}, ErrColumnDenied
		}
	}
	if len(req.Rows) > s.limits.MaxRows {
		return Job{}, ErrTooManyRows
	}
	key := strings.TrimSpace(req.IdempotencyKey)
	if key == "" {
		key = newID("export-key")
	}
	s.mu.Lock()
	for _, existing := range s.jobs {
		if existing.DeletedAt == nil && existing.TenantID == req.TenantID && existing.OrgID == req.OrgID && existing.IdempotencyKey == key {
			s.mu.Unlock()
			return cloneJob(existing), nil
		}
	}
	now := s.now()
	job := Job{ID: newID("export"), Kind: JobKindExport, TenantID: strings.TrimSpace(req.TenantID), OrgID: strings.TrimSpace(req.OrgID), ActorID: strings.TrimSpace(req.ActorID), IdempotencyKey: key, Status: JobPending, TotalRows: len(req.Rows), CreatedAt: now, UpdatedAt: now}
	s.jobs[job.ID] = job
	s.jobData[job.ID] = cloneRows(req.Rows)
	// Store requested fields in a synthetic first row marker. This keeps the
	// repository seam small while preserving deterministic worker payload.
	s.jobData[job.ID] = append([]map[string]string{{"__fields": strings.Join(req.Fields, "\x1f")}}, s.jobData[job.ID]...)
	s.mu.Unlock()
	if s.repository != nil {
		persisted, persistErr := s.repository.Create(ctx, job)
		if persistErr != nil {
			return Job{}, persistErr
		}
		job = persisted
		s.mu.Lock()
		s.jobs[job.ID] = job
		s.mu.Unlock()
	}
	if err := s.enqueue(ctx, &job); err != nil {
		return Job{}, err
	}
	s.record(ctx, "export.commit", job)
	return job, nil
}

func (s *Service) Get(ctx context.Context, id string) (Job, error) {
	if s == nil {
		return Job{}, ErrJobNotFound
	}
	scope, err := scopeFrom(ctx)
	if err != nil {
		return Job{}, err
	}
	if s.repository != nil {
		job, repoErr := s.repository.Get(ctx, strings.TrimSpace(id), scope.TenantID, scope.Organization)
		if repoErr == nil {
			return cloneJob(job), nil
		}
		if !errors.Is(repoErr, ErrJobNotFound) {
			return Job{}, repoErr
		}
	}
	s.mu.RLock()
	job, ok := s.jobs[strings.TrimSpace(id)]
	s.mu.RUnlock()
	if !ok || job.DeletedAt != nil || job.TenantID != scope.TenantID || (scope.Organization != "" && job.OrgID != scope.Organization) {
		return Job{}, ErrJobNotFound
	}
	return cloneJob(job), nil
}

func (s *Service) List(ctx context.Context, kind string) ([]Job, error) {
	scope, err := scopeFrom(ctx)
	if err != nil {
		return nil, err
	}
	if s.repository != nil {
		return s.repository.List(ctx, kind, scope.TenantID, scope.Organization)
	}
	s.mu.RLock()
	items := make([]Job, 0)
	for _, job := range s.jobs {
		if job.DeletedAt == nil && job.TenantID == scope.TenantID && (scope.Organization == "" || job.OrgID == scope.Organization) && (kind == "" || job.Kind == kind) {
			items = append(items, cloneJob(job))
		}
	}
	s.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func (s *Service) Process(ctx context.Context, id string) error {
	job, err := s.getForWorker(ctx, id)
	if err != nil {
		return err
	}
	if job.Kind == JobKindExport {
		return s.ProcessExport(ctx, id)
	}
	return s.ProcessImport(ctx, id)
}

func (s *Service) ProcessImport(ctx context.Context, id string) error {
	job, err := s.getForWorker(ctx, id)
	if err != nil {
		return err
	}
	if job.Kind != JobKindImport {
		return ErrJobStateConflict
	}
	if err := s.markRunning(id); err != nil {
		return err
	}
	s.mu.RLock()
	rows := cloneRows(s.jobData[id])
	errRows := append([]RowError(nil), s.errors[id]...)
	s.mu.RUnlock()
	// Preview errors are retained; valid rows are counted in bounded batches.
	valid := len(rows) - len(errRows)
	if valid < 0 {
		valid = 0
	}
	processed := 0
	for start := 0; start < len(rows); start += s.limits.BatchSize {
		if ctx != nil {
			select {
			case <-ctx.Done():
				_ = s.markCancelled(id)
				return ctx.Err()
			default:
			}
		}
		end := start + s.limits.BatchSize
		if end > len(rows) {
			end = len(rows)
		}
		processed += end - start
		s.updateProgress(id, processed)
	}
	s.mu.Lock()
	job = s.jobs[id]
	job.ProcessedRows = processed
	job.ErrorCount = len(errRows)
	job.Status = JobSucceeded
	job.LastErrorCode = ""
	now := s.now()
	job.FinishedAt, job.UpdatedAt = &now, now
	s.jobs[id] = job
	s.mu.Unlock()
	s.persist(ctx, job)
	s.record(ctx, "import.complete", job)
	_ = valid
	return nil
}

func (s *Service) ProcessExport(ctx context.Context, id string) error {
	job, err := s.getForWorker(ctx, id)
	if err != nil {
		return err
	}
	if job.Kind != JobKindExport {
		return ErrJobStateConflict
	}
	if err := s.markRunning(id); err != nil {
		return err
	}
	s.mu.RLock()
	data := cloneRows(s.jobData[id])
	s.mu.RUnlock()
	if len(data) == 0 {
		data = []map[string]string{}
	}
	fields := strings.Split(data[0]["__fields"], "\x1f")
	if len(fields) == 0 || fields[0] == "" {
		return s.fail(id, "export.fields_missing")
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(fields); err != nil {
		return s.fail(id, "export.write_failed")
	}
	for i, row := range data[1:] {
		if ctx != nil {
			select {
			case <-ctx.Done():
				_ = s.markCancelled(id)
				return ctx.Err()
			default:
			}
		}
		values := make([]string, len(fields))
		for j, field := range fields {
			values[j] = redactValue(field, row[field])
		}
		if err := w.Write(values); err != nil {
			return s.fail(id, "export.write_failed")
		}
		s.updateProgress(id, i+1)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return s.fail(id, "export.write_failed")
	}
	s.mu.Lock()
	s.artifacts[id] = append([]byte(nil), buf.Bytes()...)
	now := s.now()
	expires := now.Add(s.limits.DownloadTTL)
	job = s.jobs[id]
	job.ProcessedRows = len(data) - 1
	job.Status = JobSucceeded
	job.ExpiresAt = &expires
	job.UpdatedAt, job.FinishedAt = now, &now
	job.DownloadURL = ""
	s.jobs[id] = job
	s.mu.Unlock()
	s.persist(ctx, job)
	if s.downloadURL != nil {
		if url, signErr := s.downloadURL(ctx, id, s.limits.DownloadTTL); signErr == nil {
			s.mu.Lock()
			job = s.jobs[id]
			job.DownloadURL = url
			s.jobs[id] = job
			s.mu.Unlock()
			s.persist(ctx, job)
		}
	}
	s.record(ctx, "export.complete", job)
	return nil
}

func (s *Service) Cancel(ctx context.Context, id string) (Job, error) {
	job, err := s.getForWorker(ctx, id)
	if err != nil {
		return Job{}, err
	}
	if job.Status != JobPending && job.Status != JobRunning && job.Status != JobFailed {
		return Job{}, ErrJobStateConflict
	}
	if err := s.markCancelled(id); err != nil {
		return Job{}, err
	}
	return s.Get(ctx, id)
}

func (s *Service) Retry(ctx context.Context, id string) (Job, error) {
	job, err := s.getForWorker(ctx, id)
	if err != nil {
		return Job{}, err
	}
	if job.Status != JobFailed && job.Status != JobCancelled {
		return Job{}, ErrJobStateConflict
	}
	s.mu.Lock()
	job.Status, job.LastErrorCode, job.FinishedAt = JobPending, "", nil
	job.UpdatedAt = s.now()
	s.jobs[id] = job
	s.mu.Unlock()
	s.persist(ctx, job)
	s.record(ctx, "job.retry", job)
	return job, nil
}

func (s *Service) ErrorRows(ctx context.Context, id string) ([]RowError, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	if s.repository != nil {
		if scope, scopeErr := scopeFrom(ctx); scopeErr == nil {
			return s.repository.ListErrors(ctx, id, scope.TenantID, scope.Organization)
		}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]RowError(nil), s.errors[id]...), nil
}

func (s *Service) ErrorCSV(ctx context.Context, id string) ([]byte, error) {
	rows, err := s.ErrorRows(ctx, id)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"row", "column", "code", "messageKey"})
	for _, row := range rows {
		_ = w.Write([]string{strconv.Itoa(row.Row), row.Column, row.Code, row.MessageKey})
	}
	w.Flush()
	return buf.Bytes(), w.Error()
}

func (s *Service) Artifact(ctx context.Context, id string) ([]byte, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	s.mu.RLock()
	data := append([]byte(nil), s.artifacts[id]...)
	s.mu.RUnlock()
	return data, nil
}

// RegisterWorker adds only the two known handlers to the task worker.
func (s *Service) RegisterWorker(worker *jobs.Worker) error {
	if s == nil || worker == nil {
		return ErrInvalidRequest
	}
	if err := worker.Register(JobTypeImport, func(ctx context.Context, task jobs.Task) error { return s.Process(ctx, jobIDFromPayload(task.Payload)) }); err != nil {
		return err
	}
	return worker.Register(JobTypeExport, func(ctx context.Context, task jobs.Task) error { return s.Process(ctx, jobIDFromPayload(task.Payload)) })
}

func (s *Service) enqueue(ctx context.Context, job *Job) error {
	if s == nil || s.queue == nil || job == nil {
		return nil
	}
	payload, _ := json.Marshal(map[string]string{"jobId": job.ID})
	taskType := JobTypeImport
	if job.Kind == JobKindExport {
		taskType = JobTypeExport
	}
	queued, err := s.queue.Enqueue(ctx, jobs.Task{Type: taskType, PayloadVersion: 1, IdempotencyKey: job.Kind + ":" + job.IdempotencyKey, Payload: payload, MaxAttempts: 3})
	if err != nil {
		return fmt.Errorf("enqueue %s: %w", job.Kind, err)
	}
	s.mu.Lock()
	current, ok := s.jobs[job.ID]
	if ok {
		current.QueueTaskID = queued.ID
		current.UpdatedAt = s.now()
		s.jobs[job.ID] = current
		*job = cloneJob(current)
	}
	s.mu.Unlock()
	if ok {
		s.persist(ctx, *job)
	}
	return nil
}

func jobIDFromPayload(payload []byte) string {
	var body struct {
		JobID string `json:"jobId"`
	}
	_ = json.Unmarshal(payload, &body)
	return strings.TrimSpace(body.JobID)
}

func (s *Service) getForWorker(ctx context.Context, id string) (Job, error) {
	if s == nil {
		return Job{}, ErrJobNotFound
	}
	s.mu.RLock()
	job, ok := s.jobs[strings.TrimSpace(id)]
	s.mu.RUnlock()
	if !ok || job.DeletedAt != nil {
		return Job{}, ErrJobNotFound
	}
	if scope, scopeErr := tenant.RequireContext(ctx); scopeErr == nil {
		if job.TenantID != scope.TenantID || (scope.Organization != "" && job.OrgID != scope.Organization) {
			return Job{}, ErrJobNotFound
		}
	}
	return cloneJob(job), nil
}

func (s *Service) markRunning(id string) error {
	s.mu.Lock()
	job, ok := s.jobs[id]
	if !ok {
		s.mu.Unlock()
		return ErrJobNotFound
	}
	if job.Status == JobSucceeded || job.Status == JobCancelled {
		s.mu.Unlock()
		return ErrJobStateConflict
	}
	job.Status = JobRunning
	job.UpdatedAt = s.now()
	s.jobs[id] = job
	s.mu.Unlock()
	s.persist(context.Background(), job)
	return nil
}

func (s *Service) markCancelled(id string) error {
	s.mu.Lock()
	job, ok := s.jobs[id]
	if !ok {
		s.mu.Unlock()
		return ErrJobNotFound
	}
	now := s.now()
	job.Status, job.FinishedAt, job.UpdatedAt = JobCancelled, &now, now
	s.jobs[id] = job
	s.mu.Unlock()
	s.persist(context.Background(), job)
	return nil
}

func (s *Service) fail(id, code string) error {
	s.mu.Lock()
	job, ok := s.jobs[id]
	if !ok {
		s.mu.Unlock()
		return ErrJobNotFound
	}
	now := s.now()
	job.Status, job.LastErrorCode, job.FinishedAt, job.UpdatedAt = JobFailed, code, &now, now
	s.jobs[id] = job
	s.mu.Unlock()
	s.persist(context.Background(), job)
	s.record(context.Background(), "job.failed", job)
	return errors.New(code)
}

func (s *Service) updateProgress(id string, processed int) {
	s.mu.Lock()
	job, ok := s.jobs[id]
	if ok {
		job.ProcessedRows = processed
		job.UpdatedAt = s.now()
		s.jobs[id] = job
	}
	s.mu.Unlock()
}

func (s *Service) persist(ctx context.Context, job Job) {
	if s == nil || s.repository == nil {
		return
	}
	_, _ = s.repository.Update(ctx, job)
}

func (s *Service) record(ctx context.Context, action string, job Job) {
	if s.audit == nil {
		return
	}
	_ = s.audit.Record(ctx, AuditEvent{Action: action, JobID: job.ID, TenantID: job.TenantID, OrgID: job.OrgID, ActorID: job.ActorID, Status: job.Status, ErrorCount: job.ErrorCount, CreatedAt: s.now(), ResourceKey: job.Kind})
}

func (s *Service) now() time.Time {
	if s != nil && s.clock != nil {
		return s.clock().UTC()
	}
	return time.Now().UTC()
}

func scopeFrom(ctx context.Context) (tenant.Context, error) {
	if ctx == nil {
		return tenant.Context{}, tenant.ErrTenantContextMissing
	}
	return tenant.RequireContext(ctx)
}

func requestData(req Request, max int64) ([]byte, error) {
	var reader io.Reader = req.CSV
	if len(req.Data) > 0 {
		reader = bytes.NewReader(req.Data)
	}
	if reader == nil {
		return nil, ErrInvalidRequest
	}
	limited := io.LimitReader(reader, max+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read import: %w", err)
	}
	if int64(len(data)) > max {
		return nil, ErrFileTooLarge
	}
	return data, nil
}

func detectFormat(raw, name, mime string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		lower := strings.ToLower(strings.TrimSpace(name))
		switch {
		case strings.HasSuffix(lower, ".xlsx"):
			value = "xlsx"
		default:
			value = "csv"
		}
	}
	if value == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" || strings.Contains(strings.ToLower(mime), "spreadsheet") {
		value = "xlsx"
	}
	if value != "csv" && value != "xlsx" {
		return "", ErrInvalidFormat
	}
	return value, nil
}

func parseRows(data []byte, format string, maxRows int) ([][]string, []string, error) {
	if format == "xlsx" {
		return parseXLSX(data, maxRows)
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	headers, err := r.Read()
	if err == io.EOF || err != nil {
		return nil, nil, ErrInvalidRequest
	}
	headers = normalizeHeaders(headers)
	if len(headers) == 0 {
		return nil, nil, ErrInvalidRequest
	}
	rows := make([][]string, 0)
	for len(rows) < maxRows {
		row, readErr := r.Read()
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, nil, fmt.Errorf("parse csv: %w", readErr)
		}
		rows = append(rows, row)
	}
	if _, err := r.Read(); err != io.EOF {
		return nil, nil, ErrTooManyRows
	}
	return rows, headers, nil
}

func normalizeHeaders(headers []string) []string {
	out := make([]string, 0, len(headers))
	seen := map[string]int{}
	for _, value := range headers {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		seen[name]++
		if seen[name] > 1 {
			name = name + "_" + strconv.Itoa(seen[name])
		}
		out = append(out, name)
	}
	return out
}

func normalizeAllowlist(allow map[string]bool, columns []string) map[string]bool {
	out := map[string]bool{}
	for key, value := range allow {
		if value {
			out[strings.TrimSpace(key)] = true
		}
	}
	if len(out) == 0 {
		for _, column := range columns {
			if strings.TrimSpace(column) != "" {
				out[strings.TrimSpace(column)] = true
			}
		}
	}
	return out
}

func normalizeRequired(required, columns []string) map[string]bool {
	out := map[string]bool{}
	values := required
	if len(values) == 0 && len(columns) > 0 {
		values = columns
	}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out[strings.TrimSpace(value)] = true
		}
	}
	return out
}

func validateRow(number int, row map[string]string, headers []string, allow, required map[string]bool, types map[string]string) []RowError {
	for _, header := range headers {
		if !allow[header] {
			return []RowError{{Row: number, Column: header, Code: "column_not_allowed", MessageKey: "import.columnNotAllowed"}}
		}
	}
	for field := range required {
		if strings.TrimSpace(row[field]) == "" {
			return []RowError{{Row: number, Column: field, Code: "required", MessageKey: "import.required"}}
		}
	}
	for field, kind := range types {
		value := strings.TrimSpace(row[field])
		if value == "" {
			continue
		}
		valid := true
		switch strings.ToLower(strings.TrimSpace(kind)) {
		case "int", "integer":
			_, err := strconv.ParseInt(value, 10, 64)
			valid = err == nil
		case "number", "float":
			_, err := strconv.ParseFloat(value, 64)
			valid = err == nil
		case "bool", "boolean":
			_, err := strconv.ParseBool(value)
			valid = err == nil
		case "email":
			valid = strings.Contains(value, "@") && !strings.ContainsAny(value, "\r\n")
		}
		if !valid {
			return []RowError{{Row: number, Column: field, Code: "invalid_type", MessageKey: "import.invalidType"}}
		}
	}
	return nil
}

func rowsToMaps(rows [][]string, headers []string) []map[string]string {
	out := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		mapped := map[string]string{}
		for i, header := range headers {
			if i < len(row) {
				mapped[header] = strings.TrimSpace(row[i])
			}
		}
		out = append(out, mapped)
	}
	return out
}

func cloneRows(rows []map[string]string) []map[string]string {
	out := make([]map[string]string, 0, len(rows))
	for _, row := range rows {
		copyRow := make(map[string]string, len(row))
		for key, value := range row {
			copyRow[key] = value
		}
		out = append(out, copyRow)
	}
	return out
}

func redactRow(row map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range row {
		out[key] = redactValue(key, value)
	}
	return out
}

func redactValue(field, value string) string {
	lower := strings.ToLower(field)
	if strings.Contains(lower, "password") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") {
		return "***"
	}
	if len(value) > 2048 {
		return value[:2048]
	}
	return value
}

func clonePreview(result PreviewResult) PreviewResult {
	result.Headers = append([]string(nil), result.Headers...)
	result.PreviewRows = cloneRows(result.PreviewRows)
	result.Errors = append([]RowError(nil), result.Errors...)
	mapped := map[string]string{}
	for key, value := range result.MappedColumns {
		mapped[key] = value
	}
	result.MappedColumns = mapped
	return result
}

func cloneJob(job Job) Job {
	if job.ExpiresAt != nil {
		value := *job.ExpiresAt
		job.ExpiresAt = &value
	}
	if job.FinishedAt != nil {
		value := *job.FinishedAt
		job.FinishedAt = &value
	}
	return job
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func newID(prefix string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return prefix + "-fallback"
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}

// Minimal audit sink useful in tests and local composition.
type MemoryAuditSink struct {
	mu     sync.Mutex
	Events []AuditEvent
}

func (s *MemoryAuditSink) Record(_ context.Context, event AuditEvent) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.Events = append(s.Events, event)
	s.mu.Unlock()
	return nil
}

// XML structures for the small, dependency-free XLSX reader.
type xlsxWorkbook struct{}
type xlsxCell struct {
	Ref  string `xml:"r,attr"`
	Type string `xml:"t,attr"`
	V    string `xml:"v"`
	IS   struct {
		Text string `xml:"t"`
	} `xml:"is"`
}
type xlsxRow struct {
	Cells []xlsxCell `xml:"c"`
}
type xlsxSheet struct {
	Rows []xlsxRow `xml:"sheetData>row"`
}

func parseXLSX(data []byte, maxRows int) ([][]string, []string, error) {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, ErrInvalidFormat
	}
	shared := []string{}
	if file := zipFile(archive, "xl/sharedStrings.xml"); file != nil {
		content, readErr := readZipFile(file)
		if readErr != nil {
			return nil, nil, ErrInvalidFormat
		}
		var doc struct {
			Items []struct {
				Text string `xml:"t"`
			} `xml:"si"`
		}
		if xml.Unmarshal(content, &doc) == nil {
			for _, item := range doc.Items {
				shared = append(shared, item.Text)
			}
		}
	}
	sheetFile := zipFile(archive, "xl/worksheets/sheet1.xml")
	if sheetFile == nil {
		return nil, nil, ErrInvalidFormat
	}
	content, err := readZipFile(sheetFile)
	if err != nil {
		return nil, nil, ErrInvalidFormat
	}
	var sheet xlsxSheet
	if err := xml.Unmarshal(content, &sheet); err != nil || len(sheet.Rows) == 0 {
		return nil, nil, ErrInvalidFormat
	}
	rows := make([][]string, 0, len(sheet.Rows))
	maxCols := 0
	for i, row := range sheet.Rows {
		if i > maxRows {
			return nil, nil, ErrTooManyRows
		}
		values := make([]string, len(row.Cells))
		for j, cell := range row.Cells {
			value := cell.V
			if cell.Type == "s" {
				index, parseErr := strconv.Atoi(strings.TrimSpace(value))
				if parseErr == nil && index >= 0 && index < len(shared) {
					value = shared[index]
				}
			} else if cell.Type == "inlineStr" {
				value = cell.IS.Text
			}
			values[j] = value
		}
		if len(values) > maxCols {
			maxCols = len(values)
		}
		rows = append(rows, values)
	}
	if len(rows) < 1 {
		return nil, nil, ErrInvalidRequest
	}
	headers := normalizeHeaders(rows[0])
	return rows[1:], headers, nil
}

func zipFile(archive *zip.Reader, name string) *zip.File {
	for _, file := range archive.File {
		if file.Name == name {
			return file
		}
	}
	return nil
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(io.LimitReader(reader, 50<<20))
}
