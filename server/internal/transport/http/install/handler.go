package install

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	installer "example.com/gin-vben-admin/server/internal/application/installer"
	"example.com/gin-vben-admin/server/internal/transport/http/response"
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

type Handler struct {
	status       StatusProvider
	capabilities CapabilityProvider
	plan         PlanProvider
	dependencies DependencyCheckProvider
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
		if err := c.ShouldBindJSON(&request); err != nil {
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
}
