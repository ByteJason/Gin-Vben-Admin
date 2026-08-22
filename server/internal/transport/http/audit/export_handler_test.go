package audithttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	auditapp "example.com/gin-vben-admin/server/internal/application/audit"
	"github.com/gin-gonic/gin"
)

func TestAuditHandlerExportsCSVWithRedactionAndCategory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := auditapp.NewMemoryRepository([]auditapp.Event{{
		ID: "event-1", Resource: "auth", Action: "login", Outcome: "failure",
		Details: map[string]any{"password": "secret"}, CreatedAt: time.Now().UTC(),
	}})
	r := gin.New()
	RegisterRoutes(r, NewHandler(auditapp.NewService(repo)))
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/audit/events/export?format=csv&category=login", nil)
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Header().Get("Content-Type"), "text/csv") || !strings.Contains(resp.Header().Get("Content-Disposition"), "audit-events.csv") {
		t.Fatalf("status=%d headers=%v body=%s", resp.Code, resp.Header(), resp.Body.String())
	}
	if strings.Contains(resp.Body.String(), "secret") || !strings.Contains(resp.Body.String(), "REDACTED") {
		t.Fatalf("export leaked data: %s", resp.Body.String())
	}
}

func TestAuditHandlerRetentionDryRunIsStructured(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := auditapp.NewMemoryRepository([]auditapp.Event{{
		ID: "old", Resource: "system", Action: "cleanup", Outcome: "success", CreatedAt: time.Now().UTC().Add(-181 * 24 * time.Hour),
	}})
	r := gin.New()
	RegisterRoutes(r, NewHandler(auditapp.NewService(repo)))
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/audit/retention/dry-run?days=180", nil)
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "matchingCount") || !strings.Contains(resp.Body.String(), "retentionDays") {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}
