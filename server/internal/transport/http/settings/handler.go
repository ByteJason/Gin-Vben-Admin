// Package settingshttp exposes the versioned settings administration seam.
package settingshttp

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	settingsapp "example.com/gin-vben-admin/server/internal/application/settings"
	authdomain "example.com/gin-vben-admin/server/internal/domain/authdomain"
	"example.com/gin-vben-admin/server/internal/transport/http/response"
	"github.com/gin-gonic/gin"
)

const basePath = "/api/admin/v1/settings"

type ActorResolver func(*gin.Context) settingsapp.Actor

type Handler struct {
	service *settingsapp.Service
	actor   ActorResolver
}

func NewHandler(service *settingsapp.Service, resolvers ...ActorResolver) *Handler {
	var resolver ActorResolver
	if len(resolvers) > 0 {
		resolver = resolvers[0]
	}
	return &Handler{service: service, actor: resolver}
}

func RegisterRoutes(r gin.IRouter, handler *Handler) {
	group := r.Group(basePath)
	registerRoutes(group, handler)
}

// RegisterRoutesOn mounts settings routes below an already-prefixed router
// group. The application composition root uses this seam to attach the
// shared admin authentication middleware without duplicating /api/admin/v1.
func RegisterRoutesOn(group gin.IRouter, handler *Handler) {
	registerRoutes(group.Group("/settings"), handler)
}

func registerRoutes(group gin.IRouter, handler *Handler) {
	if handler == nil || handler.service == nil {
		group.GET("/*path", disabled)
		group.PUT("/*path", disabled)
		group.POST("/*path", disabled)
		return
	}
	group.GET("", handler.listDefinitions)
	group.GET("/", handler.listDefinitions)
	group.GET("/:key", handler.get)
	group.GET("/:key/history", handler.history)
	group.PUT("/:key", handler.update)
	group.POST("/:key/rollback", handler.rollback)
}

type updateRequest struct {
	Value           json.RawMessage `json:"value"`
	ExpectedVersion int64           `json:"expectedVersion"`
}

type rollbackRequest struct {
	Version         int64 `json:"version"`
	ExpectedVersion int64 `json:"expectedVersion"`
}

func (h *Handler) actorFor(c *gin.Context) settingsapp.Actor {
	if h != nil && h.actor != nil {
		return h.actor(c)
	}
	if value, ok := c.Get("auth_claims"); ok {
		if claims, ok := value.(authdomain.Claims); ok {
			return settingsapp.Actor{ID: claims.Subject}
		}
	}
	return settingsapp.Actor{}
}

func (h *Handler) listDefinitions(c *gin.Context) {
	items, err := h.service.Definitions(c.Request.Context(), h.actorFor(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, items)
}

func (h *Handler) get(c *gin.Context) {
	setting, err := h.service.Get(c.Request.Context(), h.actorFor(c), c.Param("key"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, setting)
}

func (h *Handler) history(c *gin.Context) {
	items, err := h.service.History(c.Request.Context(), h.actorFor(c), c.Param("key"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, items)
}

func (h *Handler) update(c *gin.Context) {
	var request updateRequest
	if err := c.ShouldBindJSON(&request); err != nil || len(request.Value) == 0 {
		response.Error(c, http.StatusBadRequest, 10000, "invalid request")
		return
	}
	setting, err := h.service.Update(c.Request.Context(), h.actorFor(c), settingsapp.UpdateInput{Key: c.Param("key"), Value: request.Value, ExpectedVersion: request.ExpectedVersion})
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, setting)
}

func (h *Handler) rollback(c *gin.Context) {
	var request rollbackRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Version <= 0 {
		response.Error(c, http.StatusBadRequest, 10000, "invalid request")
		return
	}
	setting, err := h.service.Rollback(c.Request.Context(), h.actorFor(c), settingsapp.RollbackInput{Key: c.Param("key"), Version: request.Version, ExpectedVersion: request.ExpectedVersion})
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, setting)
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, settingsapp.ErrPermissionDenied):
		response.Error(c, http.StatusForbidden, 30000, "forbidden")
	case errors.Is(err, settingsapp.ErrSettingNotFound):
		response.Error(c, http.StatusNotFound, 10001, "setting not found")
	case errors.Is(err, settingsapp.ErrVersionConflict):
		response.Error(c, http.StatusConflict, 10010, "version conflict")
	case errors.Is(err, settingsapp.ErrInvalidSetting):
		response.Error(c, http.StatusBadRequest, 10000, "invalid setting")
	case strings.Contains(err.Error(), "repository unavailable"):
		response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
	default:
		response.Error(c, http.StatusInternalServerError, 50000, "internal error")
	}
}

func disabled(c *gin.Context) {
	response.Error(c, http.StatusServiceUnavailable, 40001, "dependency unavailable")
}
