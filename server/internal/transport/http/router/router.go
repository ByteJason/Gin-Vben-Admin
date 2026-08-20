package router

import (
	"github.com/gin-gonic/gin"

	"example.com/gin-vben-admin/server/internal/transport/http/admin"
	"example.com/gin-vben-admin/server/internal/transport/http/client"
	"example.com/gin-vben-admin/server/internal/transport/http/health"
	"example.com/gin-vben-admin/server/internal/transport/http/middleware"
)

// NewRouter builds the public HTTP seam without binding to a port or external services.
func NewRouter(readiness ...health.ReadinessChecker) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), middleware.RequestID())
	var readinessChecker health.ReadinessChecker
	if len(readiness) > 0 {
		readinessChecker = readiness[0]
	}
	health.RegisterRoutes(r, readinessChecker)
	admin.RegisterRoutes(r)
	client.RegisterRoutes(r)
	return r
}
