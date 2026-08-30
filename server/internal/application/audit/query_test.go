package audit

import (
	"context"
	"testing"
	"time"
)

func TestQueryFiltersPaginatesAndRedactsSensitiveDetails(t *testing.T) {
	repo := NewMemoryRepository([]Event{
		{ID: "1", ActorID: "u1", Action: "login", Resource: "auth", Outcome: "success", RequestID: "r1", Details: map[string]any{"password": "secret", "deviceId": "device-123456", "jsFingerprint": "fingerprint-123456", "nested": []any{map[string]any{"token": "secret"}}}, CreatedAt: time.Unix(1, 0)},
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
	if page.Items[0].Details["deviceId"] != "devi…3456" || page.Items[0].Details["jsFingerprint"] != "fing…3456" {
		t.Fatalf("identifier masking = %+v", page.Items[0].Details)
	}
	nested, ok := page.Items[0].Details["nested"].([]any)
	if !ok || nested[0].(map[string]any)["token"] != "[REDACTED]" {
		t.Fatalf("nested redaction = %+v", page.Items[0].Details)
	}
}

func TestQueryRejectsInvalidPage(t *testing.T) {
	service := NewService(NewMemoryRepository(nil))
	if _, err := service.Query(context.Background(), Filter{Limit: -1}); err == nil {
		t.Fatal("expected invalid page error")
	}
}

type pagedAuditRepository struct {
	items []Event
}

func (r pagedAuditRepository) Query(context.Context, Filter) ([]Event, error) {
	return r.items, nil
}

func (r pagedAuditRepository) QueryPage(_ context.Context, filter Filter) ([]Event, int, error) {
	start := filter.Offset
	if start > len(r.items) {
		start = len(r.items)
	}
	end := start + filter.Limit
	if end > len(r.items) {
		end = len(r.items)
	}
	return r.items[start:end], len(r.items), nil
}

func TestQueryDoesNotApplyPageRepositoryOffsetTwice(t *testing.T) {
	service := NewService(pagedAuditRepository{items: []Event{
		{ID: "1", CreatedAt: time.Unix(3, 0)},
		{ID: "2", CreatedAt: time.Unix(2, 0)},
		{ID: "3", CreatedAt: time.Unix(1, 0)},
	}})
	page, err := service.Query(context.Background(), Filter{Limit: 1, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || page.Offset != 1 || len(page.Items) != 1 || page.Items[0].ID != "2" {
		t.Fatalf("page=%+v", page)
	}
}
