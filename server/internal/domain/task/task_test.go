package task

import (
	"testing"
	"time"
)

func TestTaskDefinitionValidate(t *testing.T) {
	now := time.Now()
	td := TaskDefinition{ID: "t1", TenantID: "tenant", Name: "daily", Type: "http", PayloadSchema: []byte(`{"type":"object"}`), Cron: "0 * * * *", Timezone: "UTC", Enabled: true, Concurrency: 1, Timeout: time.Minute, MaxAttempts: 3, IdempotencyKey: "key", CreatedAt: now, UpdatedAt: now}
	if err := td.Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestTaskDefinitionRejectsExecutableTypeAndInvalidSchema(t *testing.T) {
	td := TaskDefinition{Type: "shell", PayloadSchema: []byte(`{}`), Cron: "* * * * *", Timezone: "UTC"}
	if err := td.Validate(); err == nil {
		t.Fatal("expected type error")
	}
	td.Type = "manual"
	td.PayloadSchema = []byte(`[]`)
	if err := td.Validate(); err == nil {
		t.Fatal("expected schema error")
	}
}
