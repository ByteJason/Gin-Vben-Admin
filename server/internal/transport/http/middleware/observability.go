package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// HTTPRecorder receives completed request measurements. Implementations must
// keep recording non-blocking for the request goroutine.
type HTTPRecorder interface {
	RecordHTTP(method, route string, status int, duration time.Duration, requestID ...string)
}

// ObservabilityRuntime combines request recording with the optional metrics
// exposition endpoint. Keeping this interface in transport avoids importing a
// platform exporter into the router package.
type ObservabilityRuntime interface {
	HTTPRecorder
	ServeMetrics(http.ResponseWriter, *http.Request)
}

// Observability records route-level measurements after handlers complete. It
// intentionally uses Gin's normalized route instead of raw URLs to avoid
// high-cardinality labels and never records request bodies or credentials.
func Observability(recorder HTTPRecorder) gin.HandlerFunc {
	if recorder == nil {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}
		requestID, _ := c.Get("request_id")
		requestIDText, _ := requestID.(string)
		recorder.RecordHTTP(c.Request.Method, route, c.Writer.Status(), time.Since(started), requestIDText)
	}
}
