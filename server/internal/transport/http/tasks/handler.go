// Package taskshttp exposes the tenant-scoped task definition administration
// seam. Payloads are validated as JSON data and are never executed by HTTP.
package taskshttp

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"example.com/gin-vben-admin/server/internal/application/tasks"
	taskdomain "example.com/gin-vben-admin/server/internal/domain/task"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
	"example.com/gin-vben-admin/server/internal/transport/http/response"
	"github.com/gin-gonic/gin"
)

const basePath = "/api/admin/v1/tasks"

type Handler struct{ service *tasks.Service }

func NewHandler(service *tasks.Service) *Handler { return &Handler{service: service} }

func RegisterRoutes(r gin.IRouter, handler *Handler) { registerRoutes(r.Group(basePath), handler) }

func RegisterRoutesOn(group gin.IRouter, handler *Handler) {
	registerRoutes(group.Group("/tasks"), handler)
}

type input struct {
	Name              string          `json:"name"`
	Type              string          `json:"type"`
	PayloadSchema     json.RawMessage `json:"payloadSchema"`
	Cron              string          `json:"cron"`
	Timezone          string          `json:"timezone"`
	Enabled           bool            `json:"enabled"`
	Concurrency       int             `json:"concurrency"`
	ConcurrencyPolicy string          `json:"concurrencyPolicy"`
	TimeoutSeconds    int             `json:"timeoutSeconds"`
	MaxAttempts       int             `json:"maxAttempts"`
	IdempotencyKey    string          `json:"idempotencyKey"`
}

type runInput struct {
	Confirm        bool            `json:"confirm"`
	Payload        json.RawMessage `json:"payload"`
	IdempotencyKey string          `json:"idempotencyKey"`
}

func registerRoutes(group gin.IRouter, handler *Handler) {
	if handler == nil || handler.service == nil {
		for _, method := range []string{"GET", "POST", "PATCH", "DELETE"} {
			group.Handle(method, "/*path", disabled)
		}
		return
	}
	group.GET("", handler.list)
	group.GET("/", handler.list)
	group.POST("", handler.create)
	group.PATCH("/:id", handler.update)
	group.DELETE("/:id", handler.delete)
	group.POST("/:id/run", handler.run)
	group.GET("/:id/runs", handler.runs)
}

func (h *Handler) list(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	items, err := h.service.List(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, items)
}

func (h *Handler) create(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	var request input
	if err := c.ShouldBindJSON(&request); err != nil {
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "invalid task request", "tasks.request.invalid", nil)
		return
	}
	created, err := h.service.Create(c.Request.Context(), request.definition())
	if err != nil {
		writeError(c, err)
		return
	}
	response.Write(c, http.StatusCreated, 0, "created", created)
}

func (h *Handler) update(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	var request input
	if err := c.ShouldBindJSON(&request); err != nil {
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "invalid task request", "tasks.request.invalid", nil)
		return
	}
	updated, err := h.service.Update(c.Request.Context(), c.Param("id"), request.definition())
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, updated)
}

func (h *Handler) delete(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	if err := h.service.Delete(c.Request.Context(), c.Param("id")); err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, nil)
}

func (h *Handler) run(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	var request runInput
	if err := c.ShouldBindJSON(&request); err != nil || !request.Confirm {
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "manual task run requires confirmation", "tasks.manual.confirmationRequired", nil)
		return
	}
	definition, err := h.service.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	// The first B1.3 slice records the confirmed intent. Queue/worker execution
	// is attached by the next run-record slice, keeping this endpoint explicit
	// and preventing an unregistered payload from being executed.
	response.Write(c, http.StatusAccepted, 0, "accepted", gin.H{
		"taskId":         definition.ID,
		"status":         "pending",
		"idempotencyKey": strings.TrimSpace(request.IdempotencyKey),
	})
}

func (h *Handler) runs(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	if _, err := h.service.Get(c.Request.Context(), c.Param("id")); err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, []any{})
}

func (in input) definition() tasks.TaskDefinition {
	return tasks.TaskDefinition{
		Name: in.Name, Type: in.Type, PayloadSchema: in.PayloadSchema, Cron: in.Cron,
		Timezone: in.Timezone, Enabled: in.Enabled, Concurrency: in.Concurrency,
		ConcurrencyPolicy: in.ConcurrencyPolicy, TimeoutSeconds: in.TimeoutSeconds,
		MaxAttempts: in.MaxAttempts, IdempotencyKey: in.IdempotencyKey,
	}
}

func scopeOK(c *gin.Context) bool {
	if _, err := tenant.RequireContext(c.Request.Context()); err != nil {
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "invalid tenant context", "tenant.context.invalid", nil)
		return false
	}
	return true
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, taskdomain.ErrInvalidType), errors.Is(err, taskdomain.ErrInvalidPayloadSchema), errors.Is(err, taskdomain.ErrInvalidCron), errors.Is(err, taskdomain.ErrInvalidTimezone), errors.Is(err, taskdomain.ErrInvalidConcurrency):
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "invalid task request", "tasks.request.invalid", nil)
	case errors.Is(err, tasks.ErrConflict):
		response.ErrorWithMessageKey(c, http.StatusConflict, 10010, "task definition already exists", "tasks.definition.conflict", nil)
	case errors.Is(err, tasks.ErrNotFound):
		response.ErrorWithMessageKey(c, http.StatusNotFound, 10001, "task definition not found", "tasks.definition.notFound", nil)
	case errors.Is(err, tasks.ErrRepositoryMissing):
		response.ErrorWithMessageKey(c, http.StatusServiceUnavailable, 40001, "task dependency unavailable", "tasks.dependency.unavailable", nil)
	default:
		response.ErrorWithMessageKey(c, http.StatusInternalServerError, 50000, "internal error", "error.internal", nil)
	}
}

func disabled(c *gin.Context) {
	response.ErrorWithMessageKey(c, http.StatusServiceUnavailable, 40001, "task capability unavailable", "tasks.capability.unavailable", nil)
}
