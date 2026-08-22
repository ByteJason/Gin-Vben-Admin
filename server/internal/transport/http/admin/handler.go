package admin

import (
	"github.com/gin-gonic/gin"

	audithttp "example.com/gin-vben-admin/server/internal/transport/http/audit"
	authhttp "example.com/gin-vben-admin/server/internal/transport/http/auth"
	filehttp "example.com/gin-vben-admin/server/internal/transport/http/file"
	iamhttp "example.com/gin-vben-admin/server/internal/transport/http/iam"
	httpmiddleware "example.com/gin-vben-admin/server/internal/transport/http/middleware"
	"example.com/gin-vben-admin/server/internal/transport/http/response"
	settingshttp "example.com/gin-vben-admin/server/internal/transport/http/settings"
)

// AuxiliaryRoutes contains optional B6 management capabilities. Keeping this
// container optional preserves the dependency-free router seam used by health
// and contract tests.
type AuxiliaryRoutes struct {
	Settings     *settingshttp.Handler
	Audit        *audithttp.Handler
	Files        *filehttp.Handler
	TenantPolicy *httpmiddleware.TenantPolicy
}

func RegisterRoutes(r gin.IRouter, authHandlers ...*authhttp.Handler) {
	var handler *authhttp.Handler
	if len(authHandlers) > 0 {
		handler = authHandlers[0]
	}
	RegisterRoutesWithIAM(r, handler, nil)
}

func RegisterRoutesWithIAM(r gin.IRouter, authHandler *authhttp.Handler, iamHandler *iamhttp.Handler, auxiliary ...AuxiliaryRoutes) {
	policy := httpmiddleware.TenantPolicy{Mode: "single", DefaultTenantID: "default"}
	if len(auxiliary) > 0 && auxiliary[0].TenantPolicy != nil {
		policy = *auxiliary[0].TenantPolicy
	}
	admin := r.Group("/api/admin/v1")
	admin.GET("/ping", func(c *gin.Context) {
		response.OK(c, gin.H{"service": "admin", "status": "ok"})
	})
	authhttp.RegisterRoutes(r, authHandler, policy)
	iamhttp.RegisterRoutes(r, iamHandler, policy)
	if len(auxiliary) == 0 {
		return
	}
	capabilities := auxiliary[0]
	if authHandler != nil && authHandler.Service() != nil {
		protected := r.Group("/api/admin/v1", authhttp.Middleware(authHandler.Service()), httpmiddleware.TenantContext(policy))
		settingshttp.RegisterRoutesOn(protected, capabilities.Settings)
		audithttp.RegisterRoutesOn(protected, capabilities.Audit)
		filehttp.RegisterRoutesOn(protected, capabilities.Files)
		return
	}
	settingshttp.RegisterRoutes(r, capabilities.Settings)
	audithttp.RegisterRoutes(r, capabilities.Audit)
	filehttp.RegisterRoutes(r, capabilities.Files)
}
