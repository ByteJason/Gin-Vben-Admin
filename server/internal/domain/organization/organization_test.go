package organization

import (
	"context"
	"errors"
	"testing"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

func TestValidateRejectsSelfParentAndInvalidStatus(t *testing.T) {
	if err := Validate(Organization{ID: "root", TenantID: "tenant-a", ParentID: "root", Name: "Root", Status: "active"}); !errors.Is(err, ErrInvalidOrganization) {
		t.Fatalf("self parent error = %v", err)
	}
	if err := Validate(Organization{ID: "root", TenantID: "tenant-a", Name: "Root", Status: "unknown"}); !errors.Is(err, ErrInvalidOrganization) {
		t.Fatalf("invalid status error = %v", err)
	}
}

func TestRequireTenantRejectsCrossTenantOrganization(t *testing.T) {
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a"})
	_, err := RequireTenant(ctx, Organization{ID: "org-b", TenantID: "tenant-b", Name: "B"})
	if !errors.Is(err, tenant.ErrCrossTenant) {
		t.Fatalf("cross tenant error = %v", err)
	}
}
