package router

import (
	"github.com/gin-gonic/gin"

	"example.com/gin-vben-admin/server/internal/transport/http/admin"
	authhttp "example.com/gin-vben-admin/server/internal/transport/http/auth"
	"example.com/gin-vben-admin/server/internal/transport/http/client"
	"example.com/gin-vben-admin/server/internal/transport/http/health"
	iamhttp "example.com/gin-vben-admin/server/internal/transport/http/iam"
	installhttp "example.com/gin-vben-admin/server/internal/transport/http/install"
	"example.com/gin-vben-admin/server/internal/transport/http/middleware"
)

// NewRouter builds the public HTTP seam without binding to a port or external services.
func NewRouter(readiness ...health.ReadinessChecker) *gin.Engine {
	var readinessChecker health.ReadinessChecker
	if len(readiness) > 0 {
		readinessChecker = readiness[0]
	}
	return NewRouterWithAuth(readinessChecker, nil)
}

// NewRouterWithAuth is the composition seam used by the running application.
// NewRouter remains a dependency-free compatibility constructor for tests and
// local probes.
func NewRouterWithAuth(readinessChecker health.ReadinessChecker, authHandler *authhttp.Handler, iamHandlers ...*iamhttp.Handler) *gin.Engine {
	var iamHandler *iamhttp.Handler
	if len(iamHandlers) > 0 {
		iamHandler = iamHandlers[0]
	}
	return NewRouterWithComponents(readinessChecker, authHandler, iamHandler, nil)
}

// NewRouterWithComponents is the explicit composition seam for runtime HTTP
// capabilities. Compatibility constructors delegate here without inventing
// infrastructure dependencies.
func NewRouterWithComponents(readinessChecker health.ReadinessChecker, authHandler *authhttp.Handler, iamHandler *iamhttp.Handler, installHandler *installhttp.Handler) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), middleware.RequestID(), middleware.SecurityHeaders())
	health.RegisterRoutes(r, readinessChecker)
	installhttp.RegisterRoutes(r, installHandler)
	admin.RegisterRoutesWithIAM(r, authHandler, iamHandler)
	client.RegisterRoutes(r)
	return r
}
