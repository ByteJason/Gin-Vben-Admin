package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
)

// StructuredAccessLog records request duration and correlation fields without
// serializing query strings, bodies, cookies, or authorization headers.
func StructuredAccessLog(logger *slog.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		requestID, _ := c.Get("request_id")
		logger.Info("http.request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(started).Milliseconds(),
			"request_id", requestID,
			"result", resultForStatus(c.Writer.Status()),
		)
	}
}

func resultForStatus(status int) string {
	if status >= 500 {
		return "error"
	}
	if status >= 400 {
		return "rejected"
	}
	return "success"
}
