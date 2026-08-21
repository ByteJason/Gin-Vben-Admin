package audit

import (
	"context"
	"testing"
	"time"
)

func TestQueryFiltersPaginatesAndRedactsSensitiveDetails(t *testing.T) {
	repo := NewMemoryRepository([]Event{
		{ID: "1", ActorID: "u1", Action: "login", Resource: "auth", Outcome: "success", RequestID: "r1", Details: map[string]any{"password": "secret"}, CreatedAt: time.Unix(1, 0)},
		{ID: "2", ActorID: "u2", Action: "update", Resource: "settings", Outcome: "success", RequestID: "r2", Details: map[string]any{"key": "site.name"}, CreatedAt: time.Unix(2, 0)},
	})
	service := NewService(repo)
	page, err := service.Query(context.Background(), Filter{Resource: "auth", Limit: 10})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Details["password"] != "[REDACTED]" {
		t.Fatalf("page = %+v", page)
	}
}

func TestQueryRejectsInvalidPage(t *testing.T) {
	service := NewService(NewMemoryRepository(nil))
	if _, err := service.Query(context.Background(), Filter{Limit: -1}); err == nil {
		t.Fatal("expected invalid page error")
	}
}
