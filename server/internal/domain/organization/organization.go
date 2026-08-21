// Package organization defines the tenant-owned organization tree contract.
package organization

import (
	"context"
	"errors"
	"strings"

	"example.com/gin-vben-admin/server/internal/domain/tenant"
)

var (
	ErrInvalidOrganization  = errors.New("organization is invalid")
	ErrOrganizationNotFound = errors.New("organization not found")
)

type Organization struct {
	ID       string
	TenantID string
	ParentID string
	Name     string
	Status   string
}

type Repository interface {
	Create(context.Context, Organization) error
	Get(context.Context, string) (Organization, error)
	List(context.Context, string) ([]Organization, error)
}

func Validate(value Organization) error {
	if strings.TrimSpace(value.ID) == "" || strings.TrimSpace(value.TenantID) == "" || strings.TrimSpace(value.Name) == "" {
		return ErrInvalidOrganization
	}
	if value.Status == "" {
		value.Status = "active"
	}
	if value.Status != "active" && value.Status != "disabled" {
		return ErrInvalidOrganization
	}
	if value.ParentID == value.ID {
		return ErrInvalidOrganization
	}
	return nil
}

func RequireTenant(ctx context.Context, value Organization) (tenant.Context, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return tenant.Context{}, err
	}
	if value.TenantID == "" {
		return tenant.Context{}, tenant.ErrTenantRequired
	}
	if value.TenantID != scope.TenantID && !scope.PlatformAdmin {
		return tenant.Context{}, tenant.ErrCrossTenant
	}
	return scope, nil
}
