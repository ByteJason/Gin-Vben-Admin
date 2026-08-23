package audithttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	auditapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/audit"
	"github.com/gin-gonic/gin"
)

func TestAuditHandlerReturnsFilteredRedactedPage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := auditapp.NewMemoryRepository([]auditapp.Event{{
		ID: "event-1", Resource: "settings", Outcome: "success", RequestID: "req-1",
		Details: map[string]any{"apiKey": "secret"}, CreatedAt: time.Now().UTC(),
	}})
	handler := NewHandler(auditapp.NewService(repo))
	r := gin.New()
	RegisterRoutes(r, handler)
	request := httptest.NewRequest(http.MethodGet, "/api/admin/v1/audit/events?resource=settings", nil)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "REDACTED") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
