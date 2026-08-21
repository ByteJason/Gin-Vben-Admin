package tenant

import (
	"context"
	"errors"
	"testing"
)

func TestContextRequiresTenantID(t *testing.T) {
	if _, err := NewContext("", "org-a", false); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("NewContext error = %v, want ErrTenantRequired", err)
	}
}

func TestContextCarriesOrganizationUnderTenant(t *testing.T) {
	scope, err := NewContext("tenant-a", "org-a", false)
	if err != nil {
		t.Fatalf("NewContext() error = %v", err)
	}
	if err := scope.CheckResource("tenant-a", "org-a"); err != nil {
		t.Fatalf("same tenant/org should pass: %v", err)
	}
	if err := scope.CheckResource("tenant-a", "org-b"); !errors.Is(err, ErrOrganizationDenied) {
		t.Fatalf("cross organization should be denied: %v", err)
	}
}

func TestCrossTenantIsDeniedAndPlatformAdminMustBeExplicit(t *testing.T) {
	scope, err := NewContext("tenant-a", "", false)
	if err != nil {
		t.Fatalf("NewContext() error = %v", err)
	}
	if err := scope.CheckResource("tenant-b", ""); !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("cross tenant should be denied: %v", err)
	}

	admin, err := NewContext("tenant-a", "", true)
	if err != nil {
		t.Fatalf("platform admin context error = %v", err)
	}
	if err := admin.CheckResource("tenant-b", ""); err != nil {
		t.Fatalf("explicit platform admin should pass: %v", err)
	}
}

func TestContextRoundTripsThroughRequestContext(t *testing.T) {
	scope, err := NewContext("tenant-a", "org-a", false)
	if err != nil {
		t.Fatalf("NewContext() error = %v", err)
	}
	ctx := WithContext(context.Background(), scope)
	got, ok := FromContext(ctx)
	if !ok || got != scope {
		t.Fatalf("FromContext() = %#v, %v; want %#v, true", got, ok, scope)
	}
}

func TestRequireContextUsesDefaultDenyWhenScopeIsAbsent(t *testing.T) {
	if _, err := RequireContext(context.Background()); !errors.Is(err, ErrTenantContextMissing) {
		t.Fatalf("RequireContext() error = %v, want ErrTenantContextMissing", err)
	}
}

func TestContextRejectsWhitespaceIdentifiers(t *testing.T) {
	if _, err := NewContext("tenant a", "", false); !errors.Is(err, ErrInvalidTenantID) {
		t.Fatalf("invalid tenant error = %v, want ErrInvalidTenantID", err)
	}
	if _, err := NewContext("tenant-a", "org a", false); !errors.Is(err, ErrInvalidOrganization) {
		t.Fatalf("invalid organization error = %v, want ErrInvalidOrganization", err)
	}
}
