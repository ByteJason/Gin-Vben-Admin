package health

import (
	"context"

	"github.com/gin-gonic/gin"

	platformhealth "example.com/gin-vben-admin/server/internal/platform/health"
	"example.com/gin-vben-admin/server/internal/transport/http/response"
)

type ReadinessChecker interface {
	Check(context.Context) platformhealth.Result
}

func RegisterRoutes(r gin.IRouter, readiness ReadinessChecker) {
	if readiness == nil {
		readiness = platformhealth.NewChecker(0)
	}
	r.GET("/health/live", func(c *gin.Context) {
		response.OK(c, gin.H{"status": "ok", "check": "live"})
	})
	r.GET("/health/ready", func(c *gin.Context) {
		result := readiness.Check(c.Request.Context())
		if !result.Ready {
			response.ErrorWithData(c, 503, 40001, "dependency unavailable", gin.H{
				"status": "unavailable",
				"checks": result.Checks,
			})
			return
		}
		response.OK(c, gin.H{"status": "ready", "checks": result.Checks})
	})
}
