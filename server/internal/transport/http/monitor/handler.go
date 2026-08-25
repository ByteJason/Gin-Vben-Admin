// Package monitorhttp exposes the IAM-protected read-only runtime snapshot.
package monitorhttp

import (
	"context"
	"net/http"
	"strings"

	monitorapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/monitor"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/response"
	"github.com/gin-gonic/gin"
)

const (
	basePath       = "/api/admin/v1/ops/monitor"
	PermissionRead = "ops:monitor:read"
)

type IAMAccess interface {
	GetAuthorizationUser(context.Context, string) (domain.User, error)
	ResolveSubject(context.Context, domain.User) (domain.Subject, error)
	Authorize(context.Context, domain.Subject, domain.Request) (bool, error)
}

type Handler struct {
	service    *monitorapp.Service
	iam        IAMAccess
	allowLocal bool
}

func NewHandler(service *monitorapp.Service) *Handler {
	return &Handler{service: service, allowLocal: true}
}

func NewHandlerWithIAM(service *monitorapp.Service, iam IAMAccess) *Handler {
	return &Handler{service: service, iam: iam}
}

func RegisterRoutes(r gin.IRouter, handler *Handler) { registerRoutes(r.Group(basePath), handler) }

func registerRoutes(group gin.IRouter, handler *Handler) {
	if handler == nil || handler.service == nil || handler.iam == nil && !handler.allowLocal {
		group.GET("", disabled)
		return
	}
	group.GET("", handler.overview)
}

func RegisterRoutesOn(group gin.IRouter, handler *Handler) {
	ops := group.Group("/ops")
	if handler == nil || handler.service == nil || handler.iam == nil && !handler.allowLocal {
		ops.GET("/monitor", disabled)
		return
	}
	ops.GET("/monitor", handler.overview)
}

func (h *Handler) overview(c *gin.Context) {
	if _, err := tenant.RequireContext(c.Request.Context()); err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid tenant context")
		return
	}
	if !h.authorize(c) {
		return
	}
	overview, err := h.service.Overview(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, 40001, "monitor unavailable")
		return
	}
	response.OK(c, overview)
}

func (h *Handler) authorize(c *gin.Context) bool {
	if h == nil {
		response.Error(c, http.StatusServiceUnavailable, 40001, "monitor capability unavailable")
		return false
	}
	value, exists := c.Get("auth_claims")
	claims, ok := value.(authdomain.Claims)
	if h.iam == nil {
		if !h.allowLocal || exists {
			response.Error(c, http.StatusServiceUnavailable, 40001, "monitor capability unavailable")
			return false
		}
		scope, err := tenant.RequireContext(c.Request.Context())
		if err != nil {
			response.Error(c, http.StatusBadRequest, 10000, "invalid tenant context")
			return false
		}
		if !scope.PlatformAdmin {
			response.Error(c, http.StatusForbidden, 30000, "forbidden")
			return false
		}
		return true
	}
	if !exists || !ok || strings.TrimSpace(claims.Subject) == "" {
		response.Error(c, http.StatusUnauthorized, 20000, "unauthenticated")
		return false
	}
	scope, err := tenant.RequireContext(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid tenant context")
		return false
	}
	user, err := h.iam.GetAuthorizationUser(c.Request.Context(), claims.Subject)
	if err != nil || !user.Active {
		response.Error(c, http.StatusForbidden, 30000, "forbidden")
		return false
	}
	effectiveScope, err := scope.BindPrincipal(user.TenantID, user.OrgID)
	if err != nil {
		response.Error(c, http.StatusForbidden, 30000, "forbidden")
		return false
	}
	c.Request = c.Request.WithContext(tenant.WithContext(c.Request.Context(), effectiveScope))
	scope = effectiveScope
	subject, err := h.iam.ResolveSubject(c.Request.Context(), user)
	if err != nil {
		response.Error(c, http.StatusForbidden, 30000, "forbidden")
		return false
	}
	allowed, err := h.iam.Authorize(c.Request.Context(), subject, domain.Request{Domain: scope.TenantID, Method: http.MethodGet, Path: basePath})
	if err != nil || !allowed {
		response.Error(c, http.StatusForbidden, 30000, "forbidden")
		return false
	}
	return true
}

func disabled(c *gin.Context) {
	response.Error(c, http.StatusServiceUnavailable, 40001, "monitor capability unavailable")
}
