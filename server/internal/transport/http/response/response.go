package response

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// OK writes the shared response envelope used by the OpenAPI contracts.
// The request ID is copied into both traceId and meta.requestId so callers
// can correlate a response without knowing which transport produced it.
func OK(c *gin.Context, payload any) {
	Write(c, 200, 0, "success", payload)
}

// Write emits the shared envelope with an explicit HTTP status. It is used by
// asynchronous operations that return 202 while preserving the same contract.
func Write(c *gin.Context, status int, code int, message string, payload any) {
	requestID := requestID(c)
	c.JSON(status, gin.H{
		"code":    code,
		"message": message,
		"data":    payload,
		"traceId": requestID,
		"meta":    gin.H{"requestId": requestID},
	})
}

func Error(c *gin.Context, status int, code int, message string) {
	ErrorWithMessageKey(c, status, code, message, "error.code."+strconv.Itoa(code), nil)
}

func ErrorWithData(c *gin.Context, status int, code int, message string, data any) {
	ErrorWithMessageKeyAndData(c, status, code, message, "error.code."+strconv.Itoa(code), nil, data)
}

// ErrorWithMessageKey emits a stable, locale-independent diagnostic key and
// optional interpolation parameters while preserving the legacy message text
// for clients that have not migrated to client-side translation.
func ErrorWithMessageKey(c *gin.Context, status int, code int, message, messageKey string, params map[string]any) {
	ErrorWithMessageKeyAndData(c, status, code, message, messageKey, params, nil)
}

func ErrorWithMessageKeyAndData(c *gin.Context, status int, code int, message, messageKey string, params map[string]any, data any) {
	requestID := requestID(c)
	body := gin.H{
		"code":    code,
		"message": message,
		"data":    data,
		"traceId": requestID,
		"meta":    gin.H{"requestId": requestID},
	}
	if messageKey != "" {
		detail := gin.H{"messageKey": messageKey}
		if params != nil {
			detail["params"] = params
		}
		body["errors"] = []gin.H{detail}
	}
	c.JSON(status, body)
}

func requestID(c *gin.Context) string {
	value, ok := c.Get("request_id")
	if !ok {
		return ""
	}
	requestID, _ := value.(string)
	return requestID
}
