package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/response"
	"github.com/gin-gonic/gin"
)

// IAMAccess is the narrow application boundary required by management HTTP
// adapters. GetAuthorizationUser must return the principal used for access
// decisions rather than a presentation-oriented or request-supplied profile.
type IAMAccess interface {
	GetAuthorizationUser(context.Context, string) (domain.User, error)
	ResolveSubject(context.Context, domain.User) (domain.Subject, error)
	Authorize(context.Context, domain.Subject, domain.Request) (bool, error)
}

// IAMAuthorization applies one default-deny authorization boundary to a group
// of authenticated, tenant-scoped routes. Policies are evaluated against the
// actual HTTP method and Gin's registered FullPath rather than a menu code or
// client-provided URL.
func IAMAuthorization(service IAMAccess) gin.HandlerFunc {
	return func(c *gin.Context) {
		if service == nil {
			response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
			c.Abort()
			return
		}
		value, exists := c.Get("auth_claims")
		claims, ok := value.(authdomain.Claims)
		if !exists || !ok || strings.TrimSpace(claims.Subject) == "" {
			response.Error(c, http.StatusUnauthorized, 20000, "unauthenticated")
			c.Abort()
			return
		}
		scope, err := tenant.RequireContext(c.Request.Context())
		if err != nil {
			response.Error(c, http.StatusBadRequest, 10000, "invalid tenant context")
			c.Abort()
			return
		}
		user, err := service.GetAuthorizationUser(c.Request.Context(), claims.Subject)
		if err != nil || !user.Active {
			forbid(c)
			return
		}
		effectiveScope, err := scope.BindPrincipal(user.TenantID, user.OrgID)
		if err != nil {
			forbid(c)
			return
		}
		c.Request = c.Request.WithContext(tenant.WithContext(c.Request.Context(), effectiveScope))
		subject, err := service.ResolveSubject(c.Request.Context(), user)
		if err != nil {
			forbid(c)
			return
		}
		allowed, err := service.Authorize(c.Request.Context(), subject, domain.Request{
			Domain: effectiveScope.TenantID,
			Method: c.Request.Method,
			Path:   c.FullPath(),
		})
		if err != nil || !allowed {
			forbid(c)
			return
		}
		c.Next()
	}
}

func forbid(c *gin.Context) {
	response.Error(c, http.StatusForbidden, 30000, "forbidden")
	c.Abort()
}
