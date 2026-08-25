// Package dashboardhttp exposes the authenticated management summary.
package dashboardhttp

import (
	"context"
	"net/http"
	"strings"

	dashboardapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/dashboard"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/response"
	"github.com/gin-gonic/gin"
)

const (
	basePath               = "/api/admin/v1/dashboard/summary"
	PermissionOverviewRead = "dashboard:overview:read"
)

type IAMAccess interface {
	GetAuthorizationUser(context.Context, string) (domain.User, error)
	ResolveSubject(context.Context, domain.User) (domain.Subject, error)
	Authorize(context.Context, domain.Subject, domain.Request) (bool, error)
}

type Handler struct {
	service *dashboardapp.Service
	iam     IAMAccess
}

func NewHandler(service *dashboardapp.Service) *Handler { return &Handler{service: service} }

func NewHandlerWithIAM(service *dashboardapp.Service, iam IAMAccess) *Handler {
	return &Handler{service: service, iam: iam}
}

func RegisterRoutes(router gin.IRouter, handler *Handler) {
	group := router.Group("/api/admin/v1/dashboard")
	registerRoutes(group, handler)
}

func RegisterRoutesOn(group gin.IRouter, handler *Handler) {
	dashboard := group.Group("/dashboard")
	registerRoutes(dashboard, handler)
}

func registerRoutes(group gin.IRouter, handler *Handler) {
	if handler == nil || handler.service == nil || handler.iam == nil {
		group.GET("/summary", disabled)
		return
	}
	group.GET("/summary", handler.summary)
}

func (h *Handler) summary(c *gin.Context) {
	value, exists := c.Get("auth_claims")
	claims, ok := value.(authdomain.Claims)
	if !exists || !ok || strings.TrimSpace(claims.Subject) == "" {
		response.Error(c, http.StatusUnauthorized, 20000, "unauthenticated")
		return
	}
	if _, err := tenant.RequireContext(c.Request.Context()); err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid tenant context")
		return
	}
	if !h.authorize(c, claims.Subject) {
		return
	}
	summary, err := h.service.Summary(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, 40001, "dashboard unavailable")
		return
	}
	response.OK(c, summary)
}

func (h *Handler) authorize(c *gin.Context, userID string) bool {
	if h == nil || h.iam == nil {
		response.Error(c, http.StatusServiceUnavailable, 40001, "dashboard capability unavailable")
		return false
	}
	scope, err := tenant.RequireContext(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid tenant context")
		return false
	}
	user, err := h.iam.GetAuthorizationUser(c.Request.Context(), userID)
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
	response.Error(c, http.StatusServiceUnavailable, 40001, "dashboard capability unavailable")
}
