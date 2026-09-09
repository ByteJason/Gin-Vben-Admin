package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/application/jobs"
	"strings"
	"testing"
	"time"
)

func TestTaskManagementPersistsConfigAndPages(t *testing.T) {
	ctx := runContext(t, "tenant-page", "")
	s := NewService(NewMemoryRepository())
	s.SetClock(func() time.Time { return time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC) })
	for _, name := range []string{"alpha", "beta"} {
		_, err := s.Create(ctx, TaskDefinition{Name: name, Description: "saved description", Type: "http", HTTPConfig: json.RawMessage(`{"url":"https://example.com","method":"POST","headers":{"Authorization":"Bearer private"},"body":"text"}`), Payload: json.RawMessage(`{"days":3}`), PayloadSchema: json.RawMessage(`{}`), Cron: "@hourly", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.ListPage(ctx, DefinitionQuery{Page: 1, PageSize: 1, Name: "alpha"})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].Description != "saved description" || page.Items[0].NextRunAt == nil {
		t.Fatalf("%+v %v", page, err)
	}
	safe := PublicDefinition(page.Items[0])
	if strings.Contains(string(safe.HTTPConfig), "private") {
		t.Fatal("secret leaked")
	}
	safe.Enabled = false
	updated, err := s.Update(ctx, safe.ID, safe)
	if err != nil || !strings.Contains(string(updated.HTTPConfig), "private") {
		t.Fatalf("masked update lost secret: %v", err)
	}
}
func TestRegisteredWorkerUsesSavedMethodAndPayload(t *testing.T) {
	ctx := runContext(t, "tenant-execute", "")
	s := NewService(NewMemoryRepository())
	reg := NewMethodExecutor()
	seen := ""
	_ = reg.Register("inventory", func(_ context.Context, p json.RawMessage) (ExecutionResult, error) {
		seen = string(p)
		return ExecutionResult{ResultSummary: "counted", RedactedOutput: `{"count":2,"token":"hidden"}`}, nil
	})
	s.SetMethodExecutor(reg)
	def, err := s.Create(ctx, TaskDefinition{Name: "inventory", Type: "manual", MethodKey: "inventory", Payload: json.RawMessage(`{"days":7}`), PayloadSchema: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	queue := jobs.NewMemoryQueue(1)
	runs := NewRunService(s, NewMemoryRunRepository(), queue)
	worker := jobs.NewWorker(queue, jobs.WorkerOptions{})
	if err := runs.BindExecutors(worker, reg, nil); err != nil {
		t.Fatal(err)
	}
	run, err := runs.Enqueue(ctx, def.ID, nil, "one")
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.Execute(context.Background(), run.QueueTaskID); err != nil {
		t.Fatal(err)
	}
	got, _ := runs.Get(ctx, run.ID)
	if seen != `{"days":7}` || got.Status != RunSucceeded || got.ResultSummary != "counted" || strings.Contains(got.RedactedOutput, "hidden") {
		t.Fatalf("seen=%s run=%+v", seen, got)
	}
}
func TestTaskDefinitionRejectsUnknownRegisteredMethod(t *testing.T) {
	s := NewService(NewMemoryRepository())
	s.SetMethodExecutor(NewMethodExecutor())
	_, err := s.Create(runContext(t, "tenant", ""), TaskDefinition{Name: "unknown", Type: "manual", MethodKey: "missing", PayloadSchema: json.RawMessage(`{}`)})
	if !errors.Is(err, ErrExecutorNotRegistered) {
		t.Fatalf("err=%v", err)
	}
}

func TestPublicDefinitionKeepsJSONValidAndOpaqueSecrets(t *testing.T) {
	d := TaskDefinition{HTTPConfig: json.RawMessage(`{"url":"https://example.com/?api_key=query-secret","headers":{"Authorization":"Bearer header-secret"},"body":"{\"token\":\"body-secret\"}"}`), Payload: json.RawMessage(`{"token":"payload-secret","large":"` + strings.Repeat("a", 40000) + `"}`)}
	safe := PublicDefinition(d)
	for _, raw := range []json.RawMessage{safe.HTTPConfig, safe.Payload} {
		if !json.Valid(raw) {
			t.Fatal("redaction must preserve valid config JSON")
		}
		if strings.Contains(string(raw), "-secret") {
			t.Fatal("configuration leaked a secret")
		}
	}
	if got := mergeRedacted(safe.HTTPConfig, d.HTTPConfig); !strings.Contains(string(got), "body-secret") || !strings.Contains(string(got), "query-secret") {
		t.Fatal("masked edit lost opaque secret")
	}
}

func TestRunSnapshotSurvivesTaskEditAndDeletion(t *testing.T) {
	ctx := runContext(t, "snapshot-tenant", "")
	s := NewService(NewMemoryRepository())
	runs := NewRunService(s, NewMemoryRunRepository(), jobs.NewMemoryQueue(1))
	d, err := s.Create(ctx, TaskDefinition{Name: "Original", Description: "original description", Type: "manual", MethodKey: "inventory", PayloadSchema: json.RawMessage(`{}`), Payload: json.RawMessage(`{"days":1}`)})
	if err != nil {
		t.Fatal(err)
	}
	r, err := runs.Enqueue(ctx, d.ID, json.RawMessage(`{"days":7,"token":"hidden"}`), "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(r.ConfigSnapshot), "hidden") || !strings.Contains(string(r.ConfigSnapshot), `"days":7`) {
		t.Fatalf("invalid snapshot %s", r.ConfigSnapshot)
	}
	d.Name = "Changed"
	if _, err = s.Update(ctx, d.ID, d); err != nil {
		t.Fatal(err)
	}
	if err = s.Delete(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	page, err := runs.ListPage(ctx, RunQuery{TaskName: "Original"})
	if err != nil || page.Total != 1 || page.Items[0].TaskName != "Original" {
		t.Fatalf("history changed: %+v %v", page, err)
	}
}
