package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http/httptest"
	"testing"

	observabilityplatform "example.com/gin-vben-admin/server/internal/platform/observability"
	"github.com/gin-gonic/gin"
)

func TestStructuredAccessLogOmitsSensitiveRequestMaterial(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	logger := observabilityplatform.NewJSONLogger(&output, slog.LevelInfo, "api", "test")
	r := gin.New()
	r.Use(RequestID(), StructuredAccessLog(logger))
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	req := httptest.NewRequest("GET", "/health?token=SECRET", nil)
	req.Header.Set("Authorization", "Bearer SECRET")
	req.Header.Set(RequestIDHeader, "req-test")
	r.ServeHTTP(httptest.NewRecorder(), req)
	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatalf("JSON log error = %v", err)
	}
	if record["path"] != "/health" || record["request_id"] != "req-test" || record["result"] != "success" {
		t.Fatalf("record = %#v", record)
	}
	if bytes.Contains(output.Bytes(), []byte("SECRET")) || bytes.Contains(output.Bytes(), []byte("token=")) {
		t.Fatalf("sensitive request material leaked: %s", output.Bytes())
	}
}
