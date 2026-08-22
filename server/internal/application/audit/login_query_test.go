package audit

import (
	"context"
	"testing"
	"time"
)

func TestQueryLoginEventsScopesToAuthLoginAndRedacts(t *testing.T) {
	repo := NewMemoryRepository([]Event{
		{ID: "login-1", ActorID: "u1", Action: "login", Resource: "auth", Outcome: "success", Details: map[string]any{"password": "secret", "ipAddress": "192.0.2.1"}, CreatedAt: time.Unix(3, 0)},
		{ID: "settings-1", ActorID: "u1", Action: "update", Resource: "settings", Outcome: "success", CreatedAt: time.Unix(2, 0)},
		{ID: "login-2", ActorID: "u2", Action: "login", Resource: "auth", Outcome: "failure", CreatedAt: time.Unix(1, 0)},
	})
	page, err := NewService(repo).QueryLoginEvents(context.Background(), "u1", Filter{Limit: 10})
	if err != nil {
		t.Fatalf("QueryLoginEvents() error = %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != "login-1" || page.Items[0].Details["password"] != "[REDACTED]" {
		t.Fatalf("login page = %+v", page)
	}
}
