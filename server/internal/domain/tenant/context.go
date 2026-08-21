// Package tenant contains the storage- and transport-neutral tenant boundary.
//
// A tenant context is required for every tenant-scoped operation.  The
// package deliberately does not decide how a principal is authenticated; the
// HTTP/application layers create the context after authentication and pass it
// through request.Context.
package tenant

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

var (
	ErrTenantRequired       = errors.New("tenant id is required")
	ErrCrossTenant          = errors.New("cross-tenant access denied")
	ErrOrganizationDenied   = errors.New("organization access denied")
	ErrInvalidTenantID      = errors.New("tenant id is invalid")
	ErrInvalidOrganization  = errors.New("organization id is invalid")
	ErrTenantContextMissing = errors.New("tenant context is missing")
)

type contextKey struct{}

// Context identifies the tenant and optional organization scope for one
// operation. PlatformAdmin is intentionally explicit and must be set by an
// authenticated server-side role resolver; it is never inferred from a
// request header.
type Context struct {
	TenantID      string
	Organization  string
	PlatformAdmin bool
}

// NewContext validates and normalizes a request scope. TenantID is mandatory
// even for platform administrators so every operation remains auditable.
func NewContext(tenantID, organizationID string, platformAdmin bool) (Context, error) {
	tenantID = strings.TrimSpace(tenantID)
	organizationID = strings.TrimSpace(organizationID)
	if tenantID == "" {
		return Context{}, ErrTenantRequired
	}
	if err := validateID(tenantID, ErrInvalidTenantID); err != nil {
		return Context{}, fmt.Errorf("%w: tenant", err)
	}
	if organizationID != "" {
		if err := validateID(organizationID, ErrInvalidOrganization); err != nil {
			return Context{}, fmt.Errorf("%w: organization", err)
		}
	}
	return Context{TenantID: tenantID, Organization: organizationID, PlatformAdmin: platformAdmin}, nil
}

// WithContext stores a validated tenant scope in a request context.
func WithContext(parent context.Context, scope Context) context.Context {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithValue(parent, contextKey{}, scope)
}

// FromContext returns the tenant scope, if one was installed.
func FromContext(ctx context.Context) (Context, bool) {
	if ctx == nil {
		return Context{}, false
	}
	scope, ok := ctx.Value(contextKey{}).(Context)
	return scope, ok && scope.TenantID != ""
}

// RequireContext enforces the B8 default-deny boundary.
func RequireContext(ctx context.Context) (Context, error) {
	scope, ok := FromContext(ctx)
	if !ok {
		return Context{}, ErrTenantContextMissing
	}
	return scope, nil
}

// CheckResource verifies that a resource belongs to the current tenant and,
// when an organization scope is active, to the current organization. A
// platform administrator may cross either boundary only when that role was
// explicitly resolved by the authenticated application layer.
func (scope Context) CheckResource(resourceTenantID, resourceOrganizationID string) error {
	resourceTenantID = strings.TrimSpace(resourceTenantID)
	resourceOrganizationID = strings.TrimSpace(resourceOrganizationID)
	if resourceTenantID == "" {
		return ErrTenantRequired
	}
	if err := validateID(resourceTenantID, ErrInvalidTenantID); err != nil {
		return fmt.Errorf("%w: resource tenant", err)
	}
	if resourceOrganizationID != "" {
		if err := validateID(resourceOrganizationID, ErrInvalidOrganization); err != nil {
			return fmt.Errorf("%w: resource organization", err)
		}
	}
	if scope.PlatformAdmin {
		return nil
	}
	if scope.TenantID == "" {
		return ErrTenantContextMissing
	}
	if scope.TenantID != resourceTenantID {
		return ErrCrossTenant
	}
	if scope.Organization != "" && scope.Organization != resourceOrganizationID {
		return ErrOrganizationDenied
	}
	return nil
}

func validateID(value string, invalid error) error {
	if len(value) > 128 {
		return invalid
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return invalid
		}
	}
	return nil
}
