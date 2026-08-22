package settingshttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	settingsapp "example.com/gin-vben-admin/server/internal/application/settings"
	"github.com/gin-gonic/gin"
)

func TestSettingsHandlerReadsUpdatesAndRollsBackVersionedValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := settingsapp.NewMemoryRepository()
	service := settingsapp.NewService(repo, nil, nil, nil)
	handler := NewHandler(service, func(*gin.Context) settingsapp.Actor { return settingsapp.Actor{ID: "admin-1"} })
	r := gin.New()
	RegisterRoutes(r, handler)

	request := httptest.NewRequest(http.MethodPut, "/api/admin/v1/settings/site.name", strings.NewReader(`{"value":"\"Portal\"","expectedVersion":0}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/admin/v1/settings/site.name", nil)
	response = httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Portal") {
		t.Fatalf("get status=%d body=%s", response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/api/admin/v1/settings/site.name/rollback", strings.NewReader(`{"version":1,"expectedVersion":1}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("rollback status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestSettingsHandlerRunsStructuredConnectionTest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := settingsapp.NewService(settingsapp.NewMemoryRepository(), nil, nil, nil)
	handler := NewHandler(service, func(*gin.Context) settingsapp.Actor { return settingsapp.Actor{ID: "admin-1"} })
	r := gin.New()
	RegisterRoutes(r, handler)
	request := httptest.NewRequest(http.MethodPost, "/api/admin/v1/settings/mail.port/test", strings.NewReader(`{"value":1026}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", "req-settings-test")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "req-settings-test") || !strings.Contains(response.Body.String(), `"status":"ok"`) {
		t.Fatalf("test status=%d body=%s", response.Code, response.Body.String())
	}
}
