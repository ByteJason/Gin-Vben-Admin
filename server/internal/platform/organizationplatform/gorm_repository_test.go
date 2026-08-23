package organizationplatform

import (
	"context"
	"errors"
	"testing"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/organization"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

func TestGORMRepositoryRequiresTenantContext(t *testing.T) {
	repo := NewGORMRepository(nil)
	if _, err := repo.Get(context.Background(), "org-1"); !errors.Is(err, tenant.ErrTenantContextMissing) {
		t.Fatalf("Get() error = %v, want tenant context missing", err)
	}
	if _, err := repo.List(context.Background(), ""); !errors.Is(err, tenant.ErrTenantContextMissing) {
		t.Fatalf("List() error = %v, want tenant context missing", err)
	}
}

func TestGORMRepositoryRejectsCrossTenantCreateBeforeDatabase(t *testing.T) {
	repo := NewGORMRepository(nil)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a"})
	err := repo.Create(ctx, organization.Organization{ID: "org-b", TenantID: "tenant-b", Name: "B", Status: "active"})
	if !errors.Is(err, tenant.ErrCrossTenant) {
		t.Fatalf("Create() error = %v, want cross tenant", err)
	}
}
