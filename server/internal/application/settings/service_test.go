package settings

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestServiceUpdateRequiresExpectedVersionAndInvalidatesCache(t *testing.T) {
	repo := NewMemoryRepository()
	cache := &recordingInvalidator{}
	audit := &recordingAudit{}
	svc := NewService(repo, audit, cache, DefaultDefinitions())

	updated, err := svc.Update(context.Background(), Actor{ID: "admin-1"}, UpdateInput{
		Key:             "site.name",
		Value:           json.RawMessage(`"APP"`),
		ExpectedVersion: 0,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Version != 1 || cache.keys[0] != "site.name" || len(audit.events) != 1 {
		t.Fatalf("unexpected update result: %+v cache=%v audit=%v", updated, cache.keys, audit.events)
	}
	_, err = svc.Update(context.Background(), Actor{ID: "admin-1"}, UpdateInput{
		Key:             "site.name",
		Value:           json.RawMessage(`"STALE"`),
		ExpectedVersion: 0,
	})
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("stale update error = %v, want ErrVersionConflict", err)
	}
}

func TestServiceMasksSensitiveValuesAndRollsBackVersion(t *testing.T) {
	svc := NewService(NewMemoryRepository(), &recordingAudit{}, &recordingInvalidator{}, DefaultDefinitions())
	first, err := svc.Update(context.Background(), Actor{ID: "admin-1"}, UpdateInput{
		Key:             "observability.otlp.api_key",
		Value:           json.RawMessage(`"TOKEN"`),
		ExpectedVersion: 0,
	})
	if err != nil {
		t.Fatalf("first update error = %v", err)
	}
	if first.Value != maskedValue {
		t.Fatalf("sensitive value = %q, want mask", first.Value)
	}
	second, err := svc.Update(context.Background(), Actor{ID: "admin-1"}, UpdateInput{
		Key:             "observability.otlp.api_key",
		Value:           json.RawMessage(`"TOKEN-2"`),
		ExpectedVersion: 1,
	})
	if err != nil {
		t.Fatalf("second update error = %v", err)
	}
	restored, err := svc.Rollback(context.Background(), Actor{ID: "admin-1"}, RollbackInput{
		Key:             "observability.otlp.api_key",
		Version:         second.Version,
		ExpectedVersion: second.Version,
	})
	if err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if restored.Version != 3 || restored.Value != maskedValue {
		t.Fatalf("restored = %+v", restored)
	}
}

type recordingInvalidator struct{ keys []string }

func (r *recordingInvalidator) Invalidate(_ context.Context, key string) error {
	r.keys = append(r.keys, key)
	return nil
}

type recordingAudit struct{ events []AuditEvent }

func (r *recordingAudit) Record(_ context.Context, event AuditEvent) error {
	r.events = append(r.events, event)
	return nil
}
