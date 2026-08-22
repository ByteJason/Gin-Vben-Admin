package middleware

import (
	"errors"
	"net/http"
	"strings"

	authdomain "example.com/gin-vben-admin/server/internal/domain/authdomain"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
	"example.com/gin-vben-admin/server/internal/transport/http/response"
	"github.com/gin-gonic/gin"
)

const (
	TenantHeader       = "X-Tenant-ID"
	OrganizationHeader = "X-Org-ID"

	tenantModeSingle = "single"
	tenantModeMulti  = "multi"
)

// TenantPolicy describes the request-scope policy applied after authentication.
// IsPlatformAdmin must be resolved from authenticated server-side state; this
// middleware intentionally ignores any platform-admin request header.
type TenantPolicy struct {
	Mode               string
	DefaultTenantID    string
	TenantHeader       string
	OrganizationHeader string
	IsPlatformAdmin    func(*gin.Context) bool
	// PlatformAdminSubjects is a server-side allowlist matched against
	// verified auth claims. It is never populated from request headers.
	PlatformAdminSubjects []string
}

// TenantContext installs a validated tenant/org scope in request.Context. The
// default policy is single-tenant with tenant "default", which preserves the
// bootstrap behavior while still making tenant_id explicit at the boundary.
func TenantContext(policy TenantPolicy) gin.HandlerFunc {
	policy = normalizeTenantPolicy(policy)
	return func(c *gin.Context) {
		platformAdmin := policy.IsPlatformAdmin != nil && policy.IsPlatformAdmin(c)
		if !platformAdmin {
			platformAdmin = subjectInPlatformAdminAllowlist(c, policy.PlatformAdminSubjects)
		}
		tenantID := strings.TrimSpace(c.GetHeader(policy.TenantHeader))
		if tenantID == "" && policy.Mode == tenantModeSingle {
			tenantID = policy.DefaultTenantID
		}
		if tenantID == "" {
			writeTenantError(c, tenant.ErrTenantRequired)
			c.Abort()
			return
		}
		if !platformAdmin && policy.Mode == tenantModeSingle && tenantID != policy.DefaultTenantID {
			writeTenantError(c, tenant.ErrCrossTenant)
			c.Abort()
			return
		}
		organizationID := strings.TrimSpace(c.GetHeader(policy.OrganizationHeader))
		scope, err := tenant.NewContext(tenantID, organizationID, platformAdmin)
		if err != nil {
			writeTenantError(c, err)
			c.Abort()
			return
		}
		c.Request = c.Request.WithContext(tenant.WithContext(c.Request.Context(), scope))
		c.Next()
	}
}

func subjectInPlatformAdminAllowlist(c *gin.Context, subjects []string) bool {
	if c == nil || len(subjects) == 0 {
		return false
	}
	value, ok := c.Get("auth_claims")
	claims, claimsOK := value.(authdomain.Claims)
	if !ok || !claimsOK || strings.TrimSpace(claims.Subject) == "" {
		return false
	}
	for _, subject := range subjects {
		if strings.TrimSpace(subject) == claims.Subject {
			return true
		}
	}
	return false
}

func normalizeTenantPolicy(policy TenantPolicy) TenantPolicy {
	if policy.Mode == "" {
		policy.Mode = tenantModeSingle
	}
	if policy.Mode != tenantModeSingle && policy.Mode != tenantModeMulti {
		// An invalid policy fails closed as multi-tenant rather than silently
		// granting a default scope.
		policy.Mode = tenantModeMulti
	}
	if policy.DefaultTenantID == "" {
		policy.DefaultTenantID = "default"
	}
	if strings.TrimSpace(policy.TenantHeader) == "" {
		policy.TenantHeader = TenantHeader
	}
	if strings.TrimSpace(policy.OrganizationHeader) == "" {
		policy.OrganizationHeader = OrganizationHeader
	}
	return policy
}

func writeTenantError(c *gin.Context, err error) {
	status, code, message := http.StatusForbidden, 30000, "forbidden"
	switch {
	case errors.Is(err, tenant.ErrTenantRequired), errors.Is(err, tenant.ErrInvalidTenantID), errors.Is(err, tenant.ErrInvalidOrganization):
		status, code, message = http.StatusBadRequest, 10000, "invalid tenant context"
	}
	response.Error(c, status, code, message)
}
