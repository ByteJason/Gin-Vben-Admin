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

type Handler struct {
	status       StatusProvider
	capabilities CapabilityProvider
}

func NewHandler(status StatusProvider, capabilities ...CapabilityProvider) *Handler {
	handler := &Handler{status: status}
	if len(capabilities) > 0 {
		handler.capabilities = capabilities[0]
	}
	return handler
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
}
