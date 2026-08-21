package admin

import (
	"github.com/gin-gonic/gin"

	audithttp "example.com/gin-vben-admin/server/internal/transport/http/audit"
	authhttp "example.com/gin-vben-admin/server/internal/transport/http/auth"
	iamhttp "example.com/gin-vben-admin/server/internal/transport/http/iam"
	"example.com/gin-vben-admin/server/internal/transport/http/response"
	settingshttp "example.com/gin-vben-admin/server/internal/transport/http/settings"
)

// AuxiliaryRoutes contains optional B6 management capabilities. Keeping this
// container optional preserves the dependency-free router seam used by health
// and contract tests.
type AuxiliaryRoutes struct {
	Settings *settingshttp.Handler
	Audit    *audithttp.Handler
}

func RegisterRoutes(r gin.IRouter, authHandlers ...*authhttp.Handler) {
	var handler *authhttp.Handler
	if len(authHandlers) > 0 {
		handler = authHandlers[0]
	}
	RegisterRoutesWithIAM(r, handler, nil)
}

func RegisterRoutesWithIAM(r gin.IRouter, authHandler *authhttp.Handler, iamHandler *iamhttp.Handler, auxiliary ...AuxiliaryRoutes) {
	admin := r.Group("/api/admin/v1")
	admin.GET("/ping", func(c *gin.Context) {
		response.OK(c, gin.H{"service": "admin", "status": "ok"})
	})
	authhttp.RegisterRoutes(r, authHandler)
	iamhttp.RegisterRoutes(r, iamHandler)
	if len(auxiliary) == 0 {
		return
	}
	capabilities := auxiliary[0]
	if authHandler != nil && authHandler.Service() != nil {
		protected := r.Group("/api/admin/v1", authhttp.Middleware(authHandler.Service()))
		settingshttp.RegisterRoutes(protected, capabilities.Settings)
		audithttp.RegisterRoutes(protected, capabilities.Audit)
		return
	}
	settingshttp.RegisterRoutes(r, capabilities.Settings)
	audithttp.RegisterRoutes(r, capabilities.Audit)
}
