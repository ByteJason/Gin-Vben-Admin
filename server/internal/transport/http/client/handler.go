package client

import (
	"github.com/gin-gonic/gin"

	"example.com/gin-vben-admin/server/internal/transport/http/response"
)

func RegisterRoutes(r gin.IRouter) {
	client := r.Group("/api/client/v1")
	client.GET("/ping", func(c *gin.Context) {
		response.OK(c, gin.H{"service": "client", "status": "ok"})
	})
}
