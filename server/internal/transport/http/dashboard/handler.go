// Package dashboardhttp exposes the authenticated management summary.
package dashboardhttp

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	dashboardapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/dashboard"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/response"
	"github.com/gin-gonic/gin"
)

const (
	basePath               = "/api/admin/v1/dashboard/summary"
	overviewPath           = "/api/admin/v1/dashboard/overview"
	PermissionOverviewRead = "dashboard:overview:read"
)

type IAMAccess interface {
	GetAuthorizationUser(context.Context, string) (domain.User, error)
	ResolveSubject(context.Context, domain.User) (domain.Subject, error)
	Authorize(context.Context, domain.Subject, domain.Request) (bool, error)
}

type Handler struct {
	service      *dashboardapp.Service
	iam          IAMAccess
	localFixture bool
}

func NewHandler(service *dashboardapp.Service) *Handler {
	return &Handler{service: service, localFixture: true}
}

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
	if handler == nil || handler.service == nil || (handler.iam == nil && !handler.localFixture) {
		group.GET("/summary", disabled)
		group.GET("/overview", disabled)
		return
	}
	group.GET("/summary", handler.summary)
	group.GET("/overview", handler.overview)
}

func (h *Handler) summary(c *gin.Context) {
	if h.localFixture {
		h.serveLocal(c, false)
		return
	}
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
	if !h.authorize(c, claims.Subject, basePath) {
		return
	}
	summary, err := h.service.Summary(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, 40001, "dashboard unavailable")
		return
	}
	response.OK(c, summary)
}

func (h *Handler) overview(c *gin.Context) {
	if h.localFixture {
		h.serveLocal(c, true)
		return
	}
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
	if !h.authorize(c, claims.Subject, overviewPath) {
		return
	}
	query, err := parseOverviewQuery(c)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid dashboard overview query")
		return
	}
	overview, err := h.service.Overview(c.Request.Context(), query)
	if err != nil {
		if errors.Is(err, dashboardapp.ErrInvalidOverviewQuery) {
			response.Error(c, http.StatusBadRequest, 10000, "invalid dashboard overview query")
			return
		}
		response.Error(c, http.StatusServiceUnavailable, 40001, "dashboard unavailable")
		return
	}
	response.OK(c, overview)
}

func (h *Handler) serveLocal(c *gin.Context, overview bool) {
	if _, err := tenant.RequireContext(c.Request.Context()); err != nil {
		response.Error(c, http.StatusBadRequest, 10000, "invalid tenant context")
		return
	}
	if overview {
		query, err := parseOverviewQuery(c)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 10000, "invalid dashboard overview query")
			return
		}
		value, err := h.service.Overview(c.Request.Context(), query)
		if err != nil {
			response.Error(c, http.StatusServiceUnavailable, 40001, "dashboard unavailable")
			return
		}
		response.OK(c, value)
		return
	}
	value, err := h.service.Summary(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, 40001, "dashboard unavailable")
		return
	}
	response.OK(c, value)
}

func (h *Handler) authorize(c *gin.Context, userID, requestPath string) bool {
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
	allowed, err := h.iam.Authorize(c.Request.Context(), subject, domain.Request{Domain: scope.TenantID, Method: http.MethodGet, Path: requestPath})
	// Existing installations seed the access code against /summary. Let that
	// exact legacy grant cover the canonical overview while the policy seed is
	// migrated; no unauthenticated or cross-tenant fallback is introduced.
	if requestPath == overviewPath && (err != nil || !allowed) {
		allowed, err = h.iam.Authorize(c.Request.Context(), subject, domain.Request{Domain: scope.TenantID, Method: http.MethodGet, Path: basePath})
	}
	if err != nil || !allowed {
		response.Error(c, http.StatusForbidden, 30000, "forbidden")
		return false
	}
	return true
}

func parseOverviewQuery(c *gin.Context) (dashboardapp.OverviewQuery, error) {
	query := dashboardapp.OverviewQuery{
		Preset:      dashboardapp.Preset(strings.TrimSpace(c.Query("preset"))),
		Timezone:    strings.TrimSpace(c.Query("timezone")),
		Granularity: strings.TrimSpace(c.Query("granularity")),
	}
	location := time.UTC
	if query.Timezone != "" {
		var err error
		location, err = time.LoadLocation(query.Timezone)
		if err != nil {
			return dashboardapp.OverviewQuery{}, err
		}
	}
	var err error
	if query.From, err = parseOverviewInstant(c.Query("from"), location); err != nil {
		return dashboardapp.OverviewQuery{}, err
	}
	if query.To, err = parseOverviewInstant(c.Query("to"), location); err != nil {
		return dashboardapp.OverviewQuery{}, err
	}
	return query, nil
}

func parseOverviewInstant(raw string, location *time.Location) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		value, err = time.ParseInLocation("2006-01-02", raw, location)
		if err != nil {
			return nil, err
		}
	}
	return &value, nil
}

func disabled(c *gin.Context) {
	response.Error(c, http.StatusServiceUnavailable, 40001, "dashboard capability unavailable")
}
