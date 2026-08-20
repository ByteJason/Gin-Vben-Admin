package middleware

import "github.com/gin-gonic/gin"

// SecurityHeaders adds conservative headers suitable for JSON API responses.
// CORS is intentionally not opened globally; deployments should configure a
// same-origin reverse proxy or an explicit allow-list at the edge.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		c.Next()
	}
}
