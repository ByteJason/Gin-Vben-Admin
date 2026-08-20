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

type Handler struct {
	status StatusProvider
}

func NewHandler(status StatusProvider) *Handler {
	return &Handler{status: status}
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
}
