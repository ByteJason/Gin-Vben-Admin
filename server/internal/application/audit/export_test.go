package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExportRedactsClassifiesAndSupportsJSONAndCSV(t *testing.T) {
	repo := NewMemoryRepository([]Event{
		{ID: "login-1", ActorID: "u1", Action: "login", Resource: "auth", Outcome: "failure", Details: map[string]any{"password": "secret", "reason": "invalid"}, CreatedAt: time.Unix(2, 0)},
		{ID: "op-1", ActorID: "u1", Action: "update", Resource: "settings", Outcome: "success", Details: map[string]any{"key": "site.name"}, CreatedAt: time.Unix(1, 0)},
	})
	service := NewService(repo)

	jsonResult, err := service.Export(context.Background(), Filter{Category: CategoryLogin}, ExportFormatJSON)
	if err != nil {
		t.Fatalf("JSON export error = %v", err)
	}
	var events []Event
	if err := json.Unmarshal(jsonResult.Data, &events); err != nil {
		t.Fatalf("JSON export invalid: %v", err)
	}
	if len(events) != 1 || events[0].Category != CategoryLogin || events[0].Details["password"] != "[REDACTED]" {
		t.Fatalf("JSON events = %+v", events)
	}
	if strings.Contains(string(jsonResult.Data), "secret") {
		t.Fatal("JSON export leaked a secret")
	}

	csvResult, err := service.Export(context.Background(), Filter{Category: CategoryOperation}, ExportFormatCSV)
	if err != nil {
		t.Fatalf("CSV export error = %v", err)
	}
	if csvResult.ContentType != "text/csv; charset=utf-8" || !strings.Contains(string(csvResult.Data), "category,actorId") || !strings.Contains(string(csvResult.Data), "operation") {
		t.Fatalf("CSV result = %+v", csvResult)
	}
}

func TestRetentionDryRunReportsOnlyAndDoesNotDelete(t *testing.T) {
	now := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	repo := NewMemoryRepository([]Event{
		{ID: "old", Resource: "system", Action: "cleanup", Outcome: "success", CreatedAt: now.Add(-181 * 24 * time.Hour)},
		{ID: "new", Resource: "system", Action: "health", Outcome: "success", CreatedAt: now.Add(-10 * 24 * time.Hour)},
	})
	service := NewService(repo)
	report, err := service.RetentionDryRun(context.Background(), now, 180)
	if err != nil {
		t.Fatalf("RetentionDryRun() error = %v", err)
	}
	if report.RetentionDays != 180 || report.MatchingCount != 1 || report.Cutoff.IsZero() {
		t.Fatalf("retention report = %+v", report)
	}
	page, err := service.Query(context.Background(), Filter{Limit: 10})
	if err != nil || page.Total != 2 {
		t.Fatalf("dry-run changed data: page=%+v err=%v", page, err)
	}
}

func TestConsoleAndFileSinksRedactBeforeWriting(t *testing.T) {
	event := Event{ID: "sink-1", Resource: "settings", Action: "update", Outcome: "success", Details: map[string]any{"token": "secret"}}
	var console bytes.Buffer
	if err := NewConsoleSink(&console).Write(context.Background(), event); err != nil {
		t.Fatalf("console sink error = %v", err)
	}
	if strings.Contains(console.String(), "secret") || !strings.Contains(console.String(), "REDACTED") {
		t.Fatalf("console output = %s", console.String())
	}

	path := filepath.Join(t.TempDir(), "audit.log")
	fileSink, err := NewFileSink(path)
	if err != nil {
		t.Fatalf("NewFileSink() error = %v", err)
	}
	if err := fileSink.Write(context.Background(), event); err != nil {
		t.Fatalf("file sink error = %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(contents), "secret") || !strings.Contains(string(contents), "REDACTED") {
		t.Fatalf("file output=%s err=%v", contents, err)
	}
}
