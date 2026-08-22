package tasksplatform

import (
	"context"
	"errors"
	"testing"

	tasksapp "example.com/gin-vben-admin/server/internal/application/tasks"
)

func TestGORMRepositoryRequiresConfiguredStore(t *testing.T) {
	repo := NewGORMRepository(nil)
	if !errors.Is(repo.Save(context.Background(), tasksapp.TaskDefinition{}), tasksapp.ErrRepositoryMissing) {
		t.Fatal("Save should report a missing store")
	}
	if _, err := repo.Get(context.Background(), "id", "tenant", ""); !errors.Is(err, tasksapp.ErrRepositoryMissing) {
		t.Fatalf("Get error = %v", err)
	}
	if _, err := repo.List(context.Background(), "tenant", ""); !errors.Is(err, tasksapp.ErrRepositoryMissing) {
		t.Fatalf("List error = %v", err)
	}
	if err := repo.Delete(context.Background(), "id", "tenant", ""); !errors.Is(err, tasksapp.ErrRepositoryMissing) {
		t.Fatalf("Delete error = %v", err)
	}
}

func TestDefinitionRecordRoundTripsTimeoutAndPayload(t *testing.T) {
	definition := tasksapp.TaskDefinition{ID: "task-1", TenantID: "tenant", Name: "manual", Type: "manual", PayloadSchema: []byte(`{"type":"object"}`), Timezone: "UTC", Concurrency: 1, ConcurrencyPolicy: "forbid", TimeoutSeconds: 45, MaxAttempts: 3}
	record := fromDefinition(definition)
	got := toDefinition(record)
	if got.TimeoutSeconds != 45 || string(got.PayloadSchema) != string(definition.PayloadSchema) || got.ConcurrencyPolicy != "forbid" {
		t.Fatalf("round trip = %+v", got)
	}
}

func TestGORMRunRepositoryRequiresConfiguredStore(t *testing.T) {
	repo := NewGORMRunRepository(nil)
	if _, err := repo.Create(context.Background(), tasksapp.TaskRun{}); !errors.Is(err, tasksapp.ErrRunQueueUnavailable) {
		t.Fatalf("Create error = %v", err)
	}
	if _, err := repo.Get(context.Background(), "run", "tenant", ""); !errors.Is(err, tasksapp.ErrRunQueueUnavailable) {
		t.Fatalf("Get error = %v", err)
	}
	if _, err := repo.GetByIdempotency(context.Background(), "key", "tenant", ""); !errors.Is(err, tasksapp.ErrRunQueueUnavailable) {
		t.Fatalf("GetByIdempotency error = %v", err)
	}
	if _, err := repo.GetByQueueTask(context.Background(), "queue"); !errors.Is(err, tasksapp.ErrRunQueueUnavailable) {
		t.Fatalf("GetByQueueTask error = %v", err)
	}
	if _, err := repo.List(context.Background(), "task", "tenant", ""); !errors.Is(err, tasksapp.ErrRunQueueUnavailable) {
		t.Fatalf("List error = %v", err)
	}
	if _, err := repo.ListLogs(context.Background(), "run", "tenant", ""); !errors.Is(err, tasksapp.ErrRunQueueUnavailable) {
		t.Fatalf("ListLogs error = %v", err)
	}
	if _, err := repo.Update(context.Background(), tasksapp.TaskRun{}); !errors.Is(err, tasksapp.ErrRunQueueUnavailable) {
		t.Fatalf("Update error = %v", err)
	}
	if err := repo.AppendLog(context.Background(), tasksapp.TaskRunLog{}); !errors.Is(err, tasksapp.ErrRunQueueUnavailable) {
		t.Fatalf("AppendLog error = %v", err)
	}
}
