// Package monitorhttp exposes the platform-admin read-only runtime snapshot.
package monitorhttp

import (
	"errors"
	"net/http"

	monitorapp "example.com/gin-vben-admin/server/internal/application/monitor"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
	"example.com/gin-vben-admin/server/internal/transport/http/response"
	"github.com/gin-gonic/gin"
)

const basePath = "/api/admin/v1/ops/monitor"

type Handler struct{ service *monitorapp.Service }

func NewHandler(service *monitorapp.Service) *Handler { return &Handler{service: service} }

func RegisterRoutes(r gin.IRouter, handler *Handler) { registerRoutes(r.Group(basePath), handler) }

func registerRoutes(group gin.IRouter, handler *Handler) {
	if handler == nil || handler.service == nil {
		group.GET("", disabled)
		return
	}
	group.GET("", handler.overview)
}

func RegisterRoutesOn(group gin.IRouter, handler *Handler) {
	ops := group.Group("/ops")
	if handler == nil || handler.service == nil {
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
	overview, err := h.service.Overview(c.Request.Context())
	if err != nil {
		if errors.Is(err, monitorapp.ErrPermissionDenied) {
			response.Error(c, http.StatusForbidden, 30000, "forbidden")
			return
		}
		response.Error(c, http.StatusServiceUnavailable, 40001, "monitor unavailable")
		return
	}
	response.OK(c, overview)
}

func disabled(c *gin.Context) {
	response.Error(c, http.StatusServiceUnavailable, 40001, "monitor capability unavailable")
}
