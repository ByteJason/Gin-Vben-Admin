// Package taskshttp exposes the tenant-scoped task definition administration
// seam. Payloads are validated as JSON data and are never executed by HTTP.
package taskshttp

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/application/tasks"
	taskdomain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/task"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/response"
	"github.com/gin-gonic/gin"
)

const basePath = "/api/admin/v1/tasks"

type Handler struct {
	service    *tasks.Service
	runService *tasks.RunService
}

func NewHandler(service *tasks.Service, runServices ...*tasks.RunService) *Handler {
	var runService *tasks.RunService
	if len(runServices) > 0 {
		runService = runServices[0]
	}
	return &Handler{service: service, runService: runService}
}

func RegisterRoutes(r gin.IRouter, handler *Handler) { registerRoutes(r.Group(basePath), handler) }

func RegisterRoutesOn(group gin.IRouter, handler *Handler) {
	registerRoutes(group.Group("/tasks"), handler)
}

type input struct {
	Name              string          `json:"name"`
	Description       string          `json:"description"`
	Payload           json.RawMessage `json:"payload"`
	Type              string          `json:"type"`
	ExecutorType      string          `json:"executorType"`
	MethodKey         string          `json:"methodKey"`
	HTTPConfig        json.RawMessage `json:"http"`
	PayloadSchema     json.RawMessage `json:"payloadSchema"`
	Cron              string          `json:"cron"`
	Timezone          string          `json:"timezone"`
	Enabled           *bool           `json:"enabled"`
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
	group.GET("/runs", handler.allRuns)
	group.GET("/methods", handler.methods)
	group.GET("/preview", handler.preview)
	group.PATCH("/:id", handler.update)
	group.DELETE("/:id", handler.delete)
	group.POST("/:id/run", handler.run)
	group.GET("/:id/runs", handler.listRuns)
	group.GET("/:id/runs/:runId/logs", handler.runLogs)
	group.POST("/:id/runs/:runId/cancel", handler.cancelRun)
	group.POST("/:id/runs/:runId/retry", handler.retryRun)
}

func (h *Handler) list(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	page, size, ok := pagination(c)
	if !ok {
		return
	}
	query := tasks.DefinitionQuery{Page: page, PageSize: size, Name: c.Query("name"), ExecutorType: c.Query("executorType")}
	if value, present := c.GetQuery("enabled"); present {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			writeError(c, tasks.ErrInvalidDefinition)
			return
		}
		query.Enabled = &enabled
	}
	result, err := h.service.ListPage(c.Request.Context(), query)
	if err != nil {
		writeError(c, err)
		return
	}
	for i := range result.Items {
		result.Items[i] = tasks.PublicDefinition(result.Items[i])
	}
	response.OK(c, result)
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
	response.Write(c, http.StatusCreated, 0, "created", tasks.PublicDefinition(created))
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
	response.OK(c, tasks.PublicDefinition(updated))
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
	if h.runService == nil {
		writeError(c, tasks.ErrRunQueueUnavailable)
		return
	}
	run, err := h.runService.Enqueue(c.Request.Context(), definition.ID, request.Payload, request.IdempotencyKey)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Write(c, http.StatusAccepted, 0, "accepted", run)
}

func (h *Handler) listRuns(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	if _, err := h.service.Get(c.Request.Context(), c.Param("id")); err != nil {
		writeError(c, err)
		return
	}
	if h.runService == nil {
		writeError(c, tasks.ErrRunQueueUnavailable)
		return
	}
	page, size, ok := pagination(c)
	if !ok {
		return
	}
	items, err := h.runService.ListPage(c.Request.Context(), tasks.RunQuery{Page: page, PageSize: size, TaskID: c.Param("id")})
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, items)
}

func (h *Handler) runLogs(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	if h.runService == nil {
		writeError(c, tasks.ErrRunQueueUnavailable)
		return
	}
	logs, err := h.runService.Logs(c.Request.Context(), c.Param("id"), c.Param("runId"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, logs)
}

func (h *Handler) cancelRun(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	if h.runService == nil {
		writeError(c, tasks.ErrRunQueueUnavailable)
		return
	}
	run, err := h.runService.Cancel(c.Request.Context(), c.Param("id"), c.Param("runId"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, run)
}

func (h *Handler) retryRun(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	if h.runService == nil {
		writeError(c, tasks.ErrRunQueueUnavailable)
		return
	}
	run, err := h.runService.Retry(c.Request.Context(), c.Param("id"), c.Param("runId"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Write(c, http.StatusAccepted, 0, "accepted", run)
}

func (in input) definition() tasks.TaskDefinition {
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	return tasks.TaskDefinition{
		Name: in.Name, Description: in.Description, Payload: in.Payload, Type: in.Type, ExecutorType: in.ExecutorType, MethodKey: in.MethodKey, HTTPConfig: in.HTTPConfig, PayloadSchema: in.PayloadSchema, Cron: in.Cron,
		Timezone: in.Timezone, Enabled: enabled, Concurrency: in.Concurrency,
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
	case errors.Is(err, tasks.ErrInvalidDefinition), errors.Is(err, tasks.ErrInvalidHTTPPayload), errors.Is(err, taskdomain.ErrInvalidCronExpression):
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "invalid task configuration", "tasks.request.invalid", nil)
	case errors.Is(err, tasks.ErrExecutorNotRegistered):
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "task method is not registered", "tasks.executor.notFound", nil)
	case errors.Is(err, tasks.ErrRunConflict):
		response.ErrorWithMessageKey(c, http.StatusConflict, 10010, "task idempotency conflict", "tasks.run.conflict", nil)
	case errors.Is(err, tasks.ErrConflict):
		response.ErrorWithMessageKey(c, http.StatusConflict, 10010, "task definition already exists", "tasks.definition.conflict", nil)
	case errors.Is(err, tasks.ErrNotFound):
		response.ErrorWithMessageKey(c, http.StatusNotFound, 10001, "task definition not found", "tasks.definition.notFound", nil)
	case errors.Is(err, tasks.ErrRepositoryMissing):
		response.ErrorWithMessageKey(c, http.StatusServiceUnavailable, 40001, "task dependency unavailable", "tasks.dependency.unavailable", nil)
	case errors.Is(err, tasks.ErrInvalidRunPayload):
		response.ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "invalid task payload", "tasks.run.payloadInvalid", nil)
	case errors.Is(err, tasks.ErrRunStateConflict):
		response.ErrorWithMessageKey(c, http.StatusConflict, 10010, "task run state conflict", "tasks.run.stateConflict", nil)
	case errors.Is(err, tasks.ErrRunNotFound):
		response.ErrorWithMessageKey(c, http.StatusNotFound, 10001, "task run not found", "tasks.run.notFound", nil)
	case errors.Is(err, tasks.ErrRunQueueUnavailable):
		response.ErrorWithMessageKey(c, http.StatusServiceUnavailable, 40001, "task queue unavailable", "tasks.run.queueUnavailable", nil)
	default:
		response.ErrorWithMessageKey(c, http.StatusInternalServerError, 50000, "internal error", "error.internal", nil)
	}
}

func disabled(c *gin.Context) {
	response.ErrorWithMessageKey(c, http.StatusServiceUnavailable, 40001, "task capability unavailable", "tasks.capability.unavailable", nil)
}

func pagination(c *gin.Context) (int, int, bool) {
	page, size := 1, 20
	var err error
	if v := c.Query("page"); v != "" {
		page, err = strconv.Atoi(v)
		if err != nil || page < 1 || page > 100000 {
			writeError(c, tasks.ErrInvalidDefinition)
			return 0, 0, false
		}
	}
	if v := c.Query("pageSize"); v != "" {
		size, err = strconv.Atoi(v)
		if err != nil || size < 1 || size > 100 {
			writeError(c, tasks.ErrInvalidDefinition)
			return 0, 0, false
		}
	}
	return page, size, true
}
func (h *Handler) allRuns(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	if h.runService == nil {
		writeError(c, tasks.ErrRunQueueUnavailable)
		return
	}
	page, size, ok := pagination(c)
	if !ok {
		return
	}
	q := tasks.RunQuery{Page: page, PageSize: size, TaskID: c.Query("taskId"), TaskName: c.Query("taskName"), Status: c.Query("status"), TriggerSource: c.Query("triggerSource")}
	for key, target := range map[string]**time.Time{"from": &q.From, "to": &q.To} {
		if value := c.Query(key); value != "" {
			at, err := time.Parse(time.RFC3339, value)
			if err != nil {
				writeError(c, tasks.ErrInvalidDefinition)
				return
			}
			*target = &at
		}
	}
	if q.From != nil && q.To != nil && q.From.After(*q.To) {
		writeError(c, tasks.ErrInvalidDefinition)
		return
	}
	result, err := h.runService.ListPage(c.Request.Context(), q)
	if err != nil {
		writeError(c, err)
		return
	}
	response.OK(c, result)
}
func (h *Handler) methods(c *gin.Context) {
	if scopeOK(c) {
		response.OK(c, h.service.Methods())
	}
}
func (h *Handler) preview(c *gin.Context) {
	if !scopeOK(c) {
		return
	}
	zone := c.DefaultQuery("timezone", "UTC")
	dates, err := taskdomain.NextExecutions(c.Query("cron"), zone, time.Now(), 5)
	if err != nil {
		writeError(c, taskdomain.ErrInvalidCron)
		return
	}
	response.OK(c, dates)
}
