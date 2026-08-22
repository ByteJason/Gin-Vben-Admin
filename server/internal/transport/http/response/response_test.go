package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestErrorWithMessageKeyEmitsStableKeyAndParameters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/error", func(c *gin.Context) {
		ErrorWithMessageKey(c, http.StatusBadRequest, 10000, "invalid request", "dictionary.invalid", map[string]any{"field": "code"})
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/error", nil))
	var body struct {
		Errors []struct {
			MessageKey string         `json:"messageKey"`
			Params     map[string]any `json:"params"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Errors) != 1 || body.Errors[0].MessageKey != "dictionary.invalid" || body.Errors[0].Params["field"] != "code" {
		t.Fatalf("error details = %#v", body.Errors)
	}
}
