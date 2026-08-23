package install

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	platformi18n "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/i18n"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/response"
)

type StatusProvider interface {
	Status(context.Context) (installer.Status, error)
}

type CapabilityProvider interface {
	Probe(context.Context) (installer.Capabilities, error)
}

type PlanProvider interface {
	Plan(context.Context, installer.PlanRequest) (installer.Plan, error)
}

type DependencyCheckProvider interface {
	CheckDatabase(context.Context, installer.DatabaseConnection) (installer.DependencyCheck, error)
	CheckRedis(context.Context, installer.RedisConnection) (installer.DependencyCheck, error)
}

type ApplyProvider interface {
	Apply(context.Context, installer.ApplyRequest) (installer.ApplyResult, error)
}

type JobProvider interface {
	Start(context.Context, installer.ApplyRequest) (installer.ApplyJob, error)
	Progress(context.Context, string) (installer.ApplyJob, error)
	Retry(context.Context, string, installer.ApplyRequest) (installer.ApplyJob, error)
}

// RollbackProvider is deliberately separate from JobProvider so existing
// integrations can keep the progress/retry seam while opting into explicit
// recovery only when they can prove a transaction receipt.
type RollbackProvider interface {
	Rollback(context.Context, string, bool) (installer.RollbackResult, error)
}

type Handler struct {
	status       StatusProvider
	capabilities CapabilityProvider
	plan         PlanProvider
	dependencies DependencyCheckProvider
	apply        ApplyProvider
	jobs         JobProvider
	rollback     RollbackProvider
}

func NewHandler(status StatusProvider, capabilities ...CapabilityProvider) *Handler {
	var capability CapabilityProvider
	if len(capabilities) > 0 {
		capability = capabilities[0]
	}
	return NewHandlerWithComponents(status, capability, nil)
}

func NewHandlerWithComponents(status StatusProvider, capabilities CapabilityProvider, plan PlanProvider, dependencies ...DependencyCheckProvider) *Handler {
	var dependencyChecks DependencyCheckProvider
	if len(dependencies) > 0 {
		dependencyChecks = dependencies[0]
	}
	return &Handler{status: status, capabilities: capabilities, plan: plan, dependencies: dependencyChecks}
}

func NewHandlerWithApply(status StatusProvider, capabilities CapabilityProvider, plan PlanProvider, dependencies DependencyCheckProvider, apply ApplyProvider) *Handler {
	return &Handler{status: status, capabilities: capabilities, plan: plan, dependencies: dependencies, apply: apply}
}

func NewHandlerWithApplyAndJobs(status StatusProvider, capabilities CapabilityProvider, plan PlanProvider, dependencies DependencyCheckProvider, apply ApplyProvider, jobs JobProvider) *Handler {
	var rollback RollbackProvider
	if candidate, ok := jobs.(RollbackProvider); ok {
		rollback = candidate
	}
	return &Handler{status: status, capabilities: capabilities, plan: plan, dependencies: dependencies, apply: apply, jobs: jobs, rollback: rollback}
}

func RegisterRoutes(router gin.IRouter, handler *Handler) {
	group := router.Group("/api/system/install/v1")
	group.GET("/status", func(c *gin.Context) {
		if handler == nil || handler.status == nil {
			response.Error(c, http.StatusServiceUnavailable, 40001, "installation service unavailable")
			return
		}
		status, err := handler.status.Status(c.Request.Context())
		if err != nil {
			response.Error(c, http.StatusInternalServerError, 50000, "installation state unavailable")
			return
		}
		response.OK(c, status)
	})
	group.GET("/capabilities", func(c *gin.Context) {
		if handler == nil || handler.capabilities == nil {
			response.Error(c, http.StatusServiceUnavailable, 40001, "installation service unavailable")
			return
		}
		capabilities, err := handler.capabilities.Probe(c.Request.Context())
		if err != nil {
			response.Error(c, http.StatusInternalServerError, 50000, "installation capabilities unavailable")
			return
		}
		response.OK(c, capabilities)
	})
	group.POST("/plan", func(c *gin.Context) {
		if handler == nil || handler.plan == nil {
			response.Error(c, http.StatusServiceUnavailable, 40001, "installation service unavailable")
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
		var request installer.PlanRequest
		if !decodePlanRequest(c, &request) {
			response.Error(c, http.StatusBadRequest, 10000, "invalid installation plan")
			return
		}
		plan, err := handler.plan.Plan(c.Request.Context(), request)
		if err != nil {
			// Validation and filesystem details remain server-side. The public
			// response is intentionally stable and never carries absolute paths,
			// credentials, or OS error text.
			response.Error(c, http.StatusBadRequest, 10000, "invalid installation plan")
			return
		}
		response.OK(c, plan)
	})
	group.POST("/check/database", func(c *gin.Context) {
		if handler == nil || handler.dependencies == nil {
			response.Error(c, http.StatusServiceUnavailable, 40001, "installation service unavailable")
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
		var request installer.DatabaseConnection
		if err := c.ShouldBindJSON(&request); err != nil {
			response.Error(c, http.StatusBadRequest, 10000, "invalid database connection")
			return
		}
		result, err := handler.dependencies.CheckDatabase(c.Request.Context(), request)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 10000, "invalid database connection")
			return
		}
		response.OK(c, result)
	})
	group.POST("/check/redis", func(c *gin.Context) {
		if handler == nil || handler.dependencies == nil {
			response.Error(c, http.StatusServiceUnavailable, 40001, "installation service unavailable")
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
		var request installer.RedisConnection
		if err := c.ShouldBindJSON(&request); err != nil {
			response.Error(c, http.StatusBadRequest, 10000, "invalid redis connection")
			return
		}
		result, err := handler.dependencies.CheckRedis(c.Request.Context(), request)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 10000, "invalid redis connection")
			return
		}
		response.OK(c, result)
	})
	group.POST("/apply", func(c *gin.Context) {
		if handler == nil || (handler.apply == nil && handler.jobs == nil) {
			response.Error(c, http.StatusServiceUnavailable, 40001, "installation service unavailable")
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
		var request installer.ApplyRequest
		if !decodeApplyRequest(c, &request) {
			response.Error(c, http.StatusBadRequest, 10000, "invalid installation request")
			return
		}
		if handler.jobs != nil {
			job, err := handler.jobs.Start(c.Request.Context(), request)
			if err != nil {
				writeJobError(c, err)
				return
			}
			response.Write(c, http.StatusAccepted, 0, "accepted", job)
			return
		}
		result, err := handler.apply.Apply(c.Request.Context(), request)
		if err != nil {
			writeApplyError(c, err)
			return
		}
		response.OK(c, result)
	})
	group.GET("/progress/:id", func(c *gin.Context) {
		if handler == nil || handler.jobs == nil {
			response.Error(c, http.StatusServiceUnavailable, 40001, "installation service unavailable")
			return
		}
		job, err := handler.jobs.Progress(c.Request.Context(), c.Param("id"))
		if err != nil {
			writeJobError(c, err)
			return
		}
		response.OK(c, job)
	})
	group.POST("/retry/:id", func(c *gin.Context) {
		if handler == nil || handler.jobs == nil {
			response.Error(c, http.StatusServiceUnavailable, 40001, "installation service unavailable")
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
		var request installer.ApplyRequest
		if !decodeApplyRequest(c, &request) {
			response.Error(c, http.StatusBadRequest, 10000, "invalid installation request")
			return
		}
		job, err := handler.jobs.Retry(c.Request.Context(), c.Param("id"), request)
		if err != nil {
			writeJobError(c, err)
			return
		}
		response.Write(c, http.StatusAccepted, 0, "accepted", job)
	})
	group.POST("/rollback/:id", func(c *gin.Context) {
		if handler == nil || handler.rollback == nil {
			response.Error(c, http.StatusServiceUnavailable, 40001, "installation service unavailable")
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
		var request installer.RollbackRequest
		if !decodeRollbackRequest(c, &request) {
			response.Error(c, http.StatusBadRequest, 10000, "invalid rollback request")
			return
		}
		if !request.ConfirmRollback {
			response.Error(c, http.StatusBadRequest, 10000, "rollback confirmation is required")
			return
		}
		result, err := handler.rollback.Rollback(c.Request.Context(), c.Param("id"), request.ConfirmRollback)
		if err != nil {
			writeRollbackError(c, err)
			return
		}
		response.OK(c, result)
	})
}

func decodeApplyRequest(c *gin.Context, request *installer.ApplyRequest) bool {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(request) != nil || !jsonDocumentEnded(decoder) {
		return false
	}
	if strings.TrimSpace(request.Locale) == "" {
		request.Locale = platformi18n.SuggestLocale(c.GetHeader("Accept-Language"))
	}
	if strings.TrimSpace(request.LocaleMode) == "" {
		request.LocaleMode = string(platformi18n.ModeSingle)
	}
	return true
}

func decodePlanRequest(c *gin.Context, request *installer.PlanRequest) bool {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(request) == nil && jsonDocumentEnded(decoder)
}

func decodeRollbackRequest(c *gin.Context, request *installer.RollbackRequest) bool {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(request) == nil && jsonDocumentEnded(decoder)
}

func jsonDocumentEnded(decoder *json.Decoder) bool {
	var extra any
	return errors.Is(decoder.Decode(&extra), io.EOF)
}

func writeApplyError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, installer.ErrAlreadyInstalled):
		response.Error(c, http.StatusConflict, 10006, "installation already completed")
	case errors.Is(err, installer.ErrApplyBusy):
		response.Error(c, http.StatusConflict, 10007, "installation already running")
	case errors.Is(err, installer.ErrInvalidApply):
		response.Error(c, http.StatusBadRequest, 10000, "invalid installation request")
	case errors.Is(err, installer.ErrPreflightFailed):
		response.Error(c, http.StatusUnprocessableEntity, 10001, "installation preflight failed")
	default:
		response.Error(c, http.StatusInternalServerError, 50000, "installation failed")
	}
}

func writeJobError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, installer.ErrApplyBusy):
		response.Error(c, http.StatusConflict, 10007, "installation already running")
	case errors.Is(err, installer.ErrInvalidApply):
		response.Error(c, http.StatusBadRequest, 10000, "invalid installation request")
	case errors.Is(err, installer.ErrJobNotFound):
		response.Error(c, http.StatusNotFound, 30000, "installation job not found")
	case errors.Is(err, installer.ErrAlreadyInstalled):
		response.Error(c, http.StatusConflict, 10006, "installation already completed")
	default:
		response.Error(c, http.StatusInternalServerError, 50000, "installation job unavailable")
	}
}

func writeRollbackError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, installer.ErrJobNotFound):
		response.Error(c, http.StatusNotFound, 30000, "installation job not found")
	case errors.Is(err, installer.ErrRollbackConfirmationRequired):
		response.Error(c, http.StatusBadRequest, 10000, "rollback confirmation is required")
	case errors.Is(err, installer.ErrRollbackUnavailable):
		response.Error(c, http.StatusConflict, 10009, "installation rollback is unavailable")
	case errors.Is(err, installer.ErrApplyBusy):
		response.Error(c, http.StatusConflict, 10007, "installation already running")
	default:
		response.Error(c, http.StatusInternalServerError, 50000, "installation rollback failed")
	}
}
