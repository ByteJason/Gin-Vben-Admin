package settingshttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	settingsapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/settings"
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

func TestObservabilitySettingsAliasOnlyAcceptsKnownObservabilityKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := settingsapp.NewService(settingsapp.NewMemoryRepository(), nil, nil, nil)
	handler := NewHandler(service, func(*gin.Context) settingsapp.Actor { return settingsapp.Actor{ID: "admin-1"} })
	router := gin.New()
	RegisterRoutes(router, handler)

	valid := httptest.NewRequest(http.MethodGet, "/api/admin/v1/observability/settings/observability.metrics.enabled", nil)
	validResponse := httptest.NewRecorder()
	router.ServeHTTP(validResponse, valid)
	if validResponse.Code != http.StatusOK {
		t.Fatalf("valid observability setting status=%d body=%s", validResponse.Code, validResponse.Body.String())
	}

	invalid := httptest.NewRequest(http.MethodGet, "/api/admin/v1/observability/settings/security.jwt_secret", nil)
	invalidResponse := httptest.NewRecorder()
	router.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusNotFound {
		t.Fatalf("non-observability setting status=%d body=%s", invalidResponse.Code, invalidResponse.Body.String())
	}
}

func TestSystemSettingsRoutesUseAtomicModulesAndDoNotExposeFieldActions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := settingsapp.NewService(settingsapp.NewMemoryRepository(), nil, nil, nil)
	handler := NewHandler(service, func(*gin.Context) settingsapp.Actor { return settingsapp.Actor{ID: "admin-1"} })
	router := gin.New()
	protected := router.Group("/api/admin/v1")
	RegisterRoutesOn(protected, handler)

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/admin/v1/settings/modules", nil))
	if list.Code != http.StatusOK || strings.Contains(strings.ToLower(list.Body.String()), "mail") {
		t.Fatalf("module list status=%d body=%s", list.Code, list.Body.String())
	}

	legacyTest := httptest.NewRecorder()
	router.ServeHTTP(legacyTest, httptest.NewRequest(http.MethodPost, "/api/admin/v1/settings/mail.port/test", strings.NewReader(`{"value":1026}`)))
	if legacyTest.Code != http.StatusNotFound {
		t.Fatalf("production mail test route status=%d body=%s", legacyTest.Code, legacyTest.Body.String())
	}

	update := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/admin/v1/settings/modules/basic", strings.NewReader(`{"expectedRevision":0,"values":{"basic.site_name":"Portal"}}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(update, req)
	if update.Code != http.StatusOK || !strings.Contains(update.Body.String(), "saved_and_applied") {
		t.Fatalf("module update status=%d body=%s", update.Code, update.Body.String())
	}
}

func TestModuleRequestsRejectFieldResetAndRequireRevisionForReset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := settingsapp.NewService(settingsapp.NewMemoryRepository(), nil, nil, nil)
	handler := NewHandler(service, func(*gin.Context) settingsapp.Actor { return settingsapp.Actor{ID: "admin-1"} })
	router := gin.New()
	RegisterRoutesOn(router.Group("/api/admin/v1"), handler)

	badSave := httptest.NewRecorder()
	saveRequest := httptest.NewRequest(http.MethodPut, "/api/admin/v1/settings/modules/basic", strings.NewReader(`{"expectedRevision":0,"values":{"basic.site_name":"Portal"},"resetKeys":["basic.site_name"]}`))
	saveRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(badSave, saveRequest)
	if badSave.Code != http.StatusBadRequest {
		t.Fatalf("resetKeys save status=%d body=%s", badSave.Code, badSave.Body.String())
	}

	missingRevision := httptest.NewRecorder()
	resetRequest := httptest.NewRequest(http.MethodPost, "/api/admin/v1/settings/modules/basic/reset", strings.NewReader(`{}`))
	resetRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(missingRevision, resetRequest)
	if missingRevision.Code != http.StatusBadRequest {
		t.Fatalf("missing revision reset status=%d body=%s", missingRevision.Code, missingRevision.Body.String())
	}
}

func TestModuleUpdateRequiresRevisionAndBoundsRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := settingsapp.NewService(settingsapp.NewMemoryRepository(), nil, nil, nil)
	handler := NewHandler(service, func(*gin.Context) settingsapp.Actor { return settingsapp.Actor{ID: "admin-1"} })
	router := gin.New()
	RegisterRoutesOn(router.Group("/api/admin/v1"), handler)

	missingRevision := httptest.NewRecorder()
	missingRequest := httptest.NewRequest(http.MethodPut, "/api/admin/v1/settings/modules/basic", strings.NewReader(`{"values":{"basic.site_name":"Portal"}}`))
	missingRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(missingRevision, missingRequest)
	if missingRevision.Code != http.StatusBadRequest {
		t.Fatalf("missing revision status=%d body=%s", missingRevision.Code, missingRevision.Body.String())
	}

	longID := httptest.NewRecorder()
	longRequest := httptest.NewRequest(http.MethodPut, "/api/admin/v1/settings/modules/basic", strings.NewReader(`{"expectedRevision":0,"values":{},"requestId":"`+strings.Repeat("x", 129)+`"}`))
	longRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(longID, longRequest)
	if longID.Code != http.StatusBadRequest {
		t.Fatalf("long requestId status=%d body=%s", longID.Code, longID.Body.String())
	}
}
