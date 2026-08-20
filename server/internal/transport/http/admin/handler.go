package admin

import (
	"github.com/gin-gonic/gin"

	authhttp "example.com/gin-vben-admin/server/internal/transport/http/auth"
	"example.com/gin-vben-admin/server/internal/transport/http/response"
)

func RegisterRoutes(r gin.IRouter, authHandlers ...*authhttp.Handler) {
	admin := r.Group("/api/admin/v1")
	admin.GET("/ping", func(c *gin.Context) {
		response.OK(c, gin.H{"service": "admin", "status": "ok"})
	})
	var handler *authhttp.Handler
	if len(authHandlers) > 0 {
		handler = authHandlers[0]
	}
	authhttp.RegisterRoutes(r, handler)
}
