package tasks

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

func valid() TaskDefinition {
	return TaskDefinition{ID: "t1", TenantID: "tenant", Name: "daily", Type: "manual", PayloadSchema: []byte(`{"type":"object"}`), Timezone: "UTC", Concurrency: 1, Timeout: time.Second, MaxAttempts: 1}
}
func TestMemoryRepositoryPersistsAndScopes(t *testing.T) {
	r := NewMemoryRepository()
	s := NewService(r)
	scope, err := tenant.NewContext("tenant", "", false)
	if err != nil {
		t.Fatal(err)
	}
	ctx := tenant.WithContext(context.Background(), scope)
	created, err := s.Create(ctx, valid())
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "t1" || created.TenantID != "tenant" {
		t.Fatalf("created = %+v", created)
	}
	if _, err := s.Get(ctx, "t1"); err != nil {
		t.Fatal(err)
	}
	otherScope, _ := tenant.NewContext("other", "", false)
	got, _ := s.List(tenant.WithContext(context.Background(), otherScope))
	if len(got) != 0 {
		t.Fatal("cross tenant result")
	}
	if err := s.Delete(ctx, "t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, "t1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestServiceRequiresTenantAndRejectsDuplicateNames(t *testing.T) {
	s := NewService(NewMemoryRepository())
	if _, err := s.Create(context.Background(), valid()); !errors.Is(err, tenant.ErrTenantContextMissing) {
		t.Fatalf("missing scope error = %v", err)
	}
	scope, _ := tenant.NewContext("tenant", "org", false)
	ctx := tenant.WithContext(context.Background(), scope)
	if _, err := s.Create(ctx, valid()); err != nil {
		t.Fatal(err)
	}
	duplicate := valid()
	duplicate.ID = "t2"
	if _, err := s.Create(ctx, duplicate); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate error = %v", err)
	}
}
