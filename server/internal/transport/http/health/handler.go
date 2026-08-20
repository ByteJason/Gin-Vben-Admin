package health

import (
	"github.com/gin-gonic/gin"

	"example.com/gin-vben-admin/server/internal/transport/http/response"
)

func RegisterRoutes(r gin.IRouter) {
	r.GET("/health/live", func(c *gin.Context) {
		response.OK(c, gin.H{"status": "ok", "check": "live"})
	})
	r.GET("/health/ready", func(c *gin.Context) {
		response.OK(c, gin.H{"status": "ok", "check": "ready"})
	})
}
