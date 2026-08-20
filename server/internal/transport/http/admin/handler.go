package admin

import (
	"github.com/gin-gonic/gin"

	authhttp "example.com/gin-vben-admin/server/internal/transport/http/auth"
	iamhttp "example.com/gin-vben-admin/server/internal/transport/http/iam"
	"example.com/gin-vben-admin/server/internal/transport/http/response"
)

func RegisterRoutes(r gin.IRouter, authHandlers ...*authhttp.Handler) {
	var handler *authhttp.Handler
	if len(authHandlers) > 0 {
		handler = authHandlers[0]
	}
	RegisterRoutesWithIAM(r, handler, nil)
}

func RegisterRoutesWithIAM(r gin.IRouter, authHandler *authhttp.Handler, iamHandler *iamhttp.Handler) {
	admin := r.Group("/api/admin/v1")
	admin.GET("/ping", func(c *gin.Context) {
		response.OK(c, gin.H{"service": "admin", "status": "ok"})
	})
	authhttp.RegisterRoutes(r, authHandler)
	iamhttp.RegisterRoutes(r, iamHandler)
}
