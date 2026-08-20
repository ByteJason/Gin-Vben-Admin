package router

import (
	"github.com/gin-gonic/gin"

	"example.com/gin-vben-admin/server/internal/transport/http/admin"
	"example.com/gin-vben-admin/server/internal/transport/http/client"
	"example.com/gin-vben-admin/server/internal/transport/http/health"
	"example.com/gin-vben-admin/server/internal/transport/http/middleware"
)

// NewRouter builds the public HTTP seam without binding to a port or external services.
func NewRouter() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), middleware.RequestID())
	health.RegisterRoutes(r)
	admin.RegisterRoutes(r)
	client.RegisterRoutes(r)
	return r
}
