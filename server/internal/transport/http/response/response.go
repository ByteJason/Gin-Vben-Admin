package response

import "github.com/gin-gonic/gin"

// OK writes the shared response envelope used by the OpenAPI contracts.
// The request ID is copied into both traceId and meta.requestId so callers
// can correlate a response without knowing which transport produced it.
func OK(c *gin.Context, payload any) {
	requestID := requestID(c)
	c.JSON(200, gin.H{
		"code":    0,
		"message": "success",
		"data":    payload,
		"traceId": requestID,
		"meta":    gin.H{"requestId": requestID},
	})
}

func Error(c *gin.Context, status int, code int, message string) {
	requestID := requestID(c)
	c.JSON(status, gin.H{
		"code":    code,
		"message": message,
		"traceId": requestID,
		"meta":    gin.H{"requestId": requestID},
	})
}

func requestID(c *gin.Context) string {
	value, ok := c.Get("request_id")
	if !ok {
		return ""
	}
	requestID, _ := value.(string)
	return requestID
}
