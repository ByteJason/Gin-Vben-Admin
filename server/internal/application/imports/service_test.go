package imports

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"example.com/gin-vben-admin/server/internal/application/jobs"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
)

func TestCSVPreviewEnforcesAllowlistAndReportsRows(t *testing.T) {
	s := NewService(DefaultLimits())
	req := Request{TenantID: "tenant-a", IdempotencyKey: "k1", Columns: []string{"name", "email"}, Allowlist: map[string]bool{"name": true, "email": true}, CSV: strings.NewReader("name,email,secret\nAlice,a@example.test,x\n,Bad\n")}
	got, err := s.Preview(req)
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalRows != 2 || len(got.Errors) != 2 {
		t.Fatalf("preview=%+v", got)
	}
	if got.PreviewRows[0]["secret"] != "***" {
		t.Fatalf("preview leaked secret: %+v", got.PreviewRows[0])
	}
}

func TestImportCommitIsTenantScopedIdempotentAndProcessesInBatches(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 8, 23, 1, 2, 3, 0, time.UTC) }
	audit := &MemoryAuditSink{}
	s := NewService(Config{Limits: Limits{MaxRows: 10, BatchSize: 1}, Clock: clock, Audit: audit})
	preview, err := s.Preview(Request{TenantID: "tenant-a", Columns: []string{"name"}, CSV: strings.NewReader("name\nA\nB\n")})
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.Commit(context.Background(), CommitRequest{TenantID: "tenant-a", PreviewID: preview.ID, IdempotencyKey: "same"})
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := s.Commit(context.Background(), CommitRequest{TenantID: "tenant-a", PreviewID: preview.ID, IdempotencyKey: "same"})
	if err != nil || repeat.ID != job.ID {
		t.Fatalf("idempotency repeat=%+v err=%v", repeat, err)
	}
	scope, err := tenant.NewContext("tenant-a", "", false)
	if err != nil {
		t.Fatal(err)
	}
	jobCtx := tenant.WithContext(context.Background(), scope)
	if err := s.ProcessImport(jobCtx, job.ID); err != nil {
		t.Fatal(err)
	}
	finished, err := s.Get(jobCtx, job.ID)
	if err != nil || finished.Status != JobSucceeded || finished.ProcessedRows != 2 {
		t.Fatalf("finished=%+v err=%v", finished, err)
	}
	if len(audit.Events) < 2 {
		t.Fatalf("audit events=%+v", audit.Events)
	}
	otherScope, _ := tenant.NewContext("tenant-b", "", false)
	if _, err := s.Get(tenant.WithContext(context.Background(), otherScope), job.ID); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("cross tenant get err=%v", err)
	}
}

func TestExportRedactsFieldsAndExpiresDownload(t *testing.T) {
	now := time.Date(2026, 8, 23, 1, 2, 3, 0, time.UTC)
	s := NewService(Config{Limits: Limits{DownloadTTL: time.Minute}, Clock: func() time.Time { return now }})
	job, err := s.StartExport(context.Background(), ExportRequest{TenantID: "tenant-a", IdempotencyKey: "export-1", Fields: []string{"name", "secret"}, Allowlist: map[string]bool{"name": true, "secret": true}, Rows: []map[string]string{{"name": "Alice", "secret": "token-value"}}})
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := tenant.NewContext("tenant-a", "", false)
	ctx := tenant.WithContext(context.Background(), scope)
	if err := s.ProcessExport(ctx, job.ID); err != nil {
		t.Fatal(err)
	}
	artifact, err := s.Artifact(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(artifact), "token-value") || !strings.Contains(string(artifact), "***") {
		t.Fatalf("artifact redaction failed: %q", artifact)
	}
	finished, _ := s.Get(ctx, job.ID)
	if finished.ExpiresAt == nil || !finished.ExpiresAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("expiry=%v", finished.ExpiresAt)
	}
}

func TestRegisterWorkerOnlyHandlesKnownJobTypes(t *testing.T) {
	queue := jobs.NewMemoryQueue(2)
	s := NewService(DefaultLimits())
	worker := jobs.NewWorker(queue, jobs.WorkerOptions{})
	if err := s.RegisterWorker(worker); err != nil {
		t.Fatal(err)
	}
	if err := worker.Execute(context.Background(), "missing"); !errors.Is(err, jobs.ErrTaskNotFound) {
		t.Fatalf("missing task err=%v", err)
	}
}

func TestXLSXPreviewAndSecurityHooksAreBounded(t *testing.T) {
	data := minimalXLSX()
	called := false
	s := NewService(Config{Limits: Limits{MaxFileBytes: int64(len(data))}, VirusScan: func(_ context.Context, got []byte) error {
		called = len(got) == len(data)
		return nil
	}})
	result, err := s.PreviewContext(tenant.WithContext(context.Background(), mustScope("tenant-a")), Request{Format: "xlsx", Name: "users.xlsx", TenantID: "tenant-a", Data: data, Columns: []string{"name", "email"}, Allowlist: map[string]bool{"name": true, "email": true}})
	if err != nil {
		t.Fatal(err)
	}
	if !called || result.Format != "xlsx" || result.TotalRows != 1 || result.PreviewRows[0]["name"] != "Alice" {
		t.Fatalf("xlsx result=%+v called=%v", result, called)
	}
	tooLarge := NewService(Limits{MaxFileBytes: 2})
	if _, err := tooLarge.Preview(Request{TenantID: "tenant-a", Data: []byte("name\nAlice\n")}); !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("too large err=%v", err)
	}
}

func mustScope(id string) tenant.Context {
	scope, err := tenant.NewContext(id, "", false)
	if err != nil {
		panic(err)
	}
	return scope
}

func minimalXLSX() []byte {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	files := map[string]string{
		"xl/sharedStrings.xml":     `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>name</t></si><si><t>email</t></si><si><t>Alice</t></si><si><t>a@example.test</t></si></sst>`,
		"xl/worksheets/sheet1.xml": `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row><row><c r="A2" t="s"><v>2</v></c><c r="B2" t="s"><v>3</v></c></row></sheetData></worksheet>`,
	}
	for name, content := range files {
		writer, err := archive.Create(name)
		if err != nil {
			panic(err)
		}
		_, _ = io.WriteString(writer, content)
	}
	if err := archive.Close(); err != nil {
		panic(err)
	}
	return buffer.Bytes()
}

func TestCommitEnqueuesKnownWorkerTask(t *testing.T) {
	queue := jobs.NewMemoryQueue(2)
	s := NewService(Config{Queue: queue})
	preview, err := s.Preview(Request{TenantID: "tenant-a", Columns: []string{"name"}, CSV: strings.NewReader("name\nA\n")})
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.Commit(context.Background(), CommitRequest{TenantID: "tenant-a", PreviewID: preview.ID, IdempotencyKey: "queue-key"})
	if err != nil {
		t.Fatal(err)
	}
	queued, err := queue.Get(context.Background(), job.QueueTaskID)
	if err != nil || queued.Type != JobTypeImport || !strings.Contains(string(queued.Payload), job.ID) {
		t.Fatalf("queued=%+v err=%v", queued, err)
	}
}

func TestWorkerProcessesQueuedImportAndPreservesTenantScope(t *testing.T) {
	queue := jobs.NewMemoryQueue(2)
	s := NewService(Config{Queue: queue})
	worker := jobs.NewWorker(queue, jobs.WorkerOptions{})
	if err := s.RegisterWorker(worker); err != nil {
		t.Fatal(err)
	}
	preview, err := s.Preview(Request{TenantID: "tenant-a", Columns: []string{"name"}, CSV: strings.NewReader("name\nAlice\n")})
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.Commit(context.Background(), CommitRequest{TenantID: "tenant-a", PreviewID: preview.ID, IdempotencyKey: "worker-1"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := tenant.WithContext(context.Background(), mustScope("tenant-a"))
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	finished, err := s.Get(ctx, job.ID)
	if err != nil || finished.Status != JobSucceeded {
		t.Fatalf("finished=%+v err=%v", finished, err)
	}
}
