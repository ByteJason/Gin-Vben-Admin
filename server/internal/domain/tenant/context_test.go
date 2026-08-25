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

func TestContextChecksTenantWideAndOrganizationBoundPrincipals(t *testing.T) {
	scope, err := NewContext("tenant-a", "org-b", false)
	if err != nil {
		t.Fatalf("NewContext() error = %v", err)
	}
	if err := scope.CheckPrincipal("tenant-a", ""); err != nil {
		t.Fatalf("tenant-wide principal should be valid in a scoped organization: %v", err)
	}
	if err := scope.CheckPrincipal("tenant-a", "org-a"); !errors.Is(err, ErrOrganizationDenied) {
		t.Fatalf("principal bound to another organization should be denied: %v", err)
	}
	if err := scope.CheckPrincipal("tenant-b", ""); !errors.Is(err, ErrCrossTenant) {
		t.Fatalf("tenant-wide principal must not cross tenants: %v", err)
	}
}

func TestContextBindsOrganizationPrincipalWhenRequestScopeIsTenantWide(t *testing.T) {
	scope, err := NewContext("tenant-a", "", false)
	if err != nil {
		t.Fatalf("NewContext() error = %v", err)
	}
	bound, err := scope.BindPrincipal("tenant-a", "org-a")
	if err != nil {
		t.Fatalf("BindPrincipal() error = %v", err)
	}
	if bound.TenantID != "tenant-a" || bound.Organization != "org-a" || bound.PlatformAdmin {
		t.Fatalf("bound scope = %#v", bound)
	}

	tenantWide, err := scope.BindPrincipal("tenant-a", "")
	if err != nil {
		t.Fatalf("tenant-wide BindPrincipal() error = %v", err)
	}
	if tenantWide.Organization != "" {
		t.Fatalf("tenant-wide principal was unexpectedly narrowed: %#v", tenantWide)
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
