package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	settingsapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/settings"
	domainobs "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/observability"
	observabilityplatform "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/observability"
	adminhttp "github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/admin"
	settingshttp "github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/settings"
	"github.com/gin-gonic/gin"
)

func TestPublicHTTPSeam(t *testing.T) {
	r := NewRouter()

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{name: "live", path: "/health/live", wantStatus: http.StatusOK},
		{name: "ready", path: "/health/ready", wantStatus: http.StatusOK},
		{name: "admin route", path: "/api/admin/v1/ping", wantStatus: http.StatusOK},
		{name: "client route", path: "/api/client/v1/ping", wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Fatalf("GET %s status = %d, want %d; body=%s", tt.path, w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

func TestRequestIDMiddleware(t *testing.T) {
	r := NewRouter()

	for _, incoming := range []string{"", "REQ-test-123"} {
		req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
		if incoming != "" {
			req.Header.Set("X-Request-ID", incoming)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		got := w.Header().Get("X-Request-ID")
		if got == "" {
			t.Fatal("response X-Request-ID is empty")
		}
		if incoming != "" && got != incoming {
			t.Fatalf("response X-Request-ID = %q, want incoming %q", got, incoming)
		}
	}
}

func TestResponseEnvelopeCarriesRequestMetadata(t *testing.T) {
	r := NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	req.Header.Set("X-Request-ID", "REQ-envelope-1")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		TraceID string `json:"traceId"`
		Meta    struct {
			RequestID string `json:"requestId"`
		} `json:"meta"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != 0 || body.Message != "success" {
		t.Fatalf("unexpected envelope: %+v", body)
	}
	if body.TraceID != "REQ-envelope-1" || body.Meta.RequestID != "REQ-envelope-1" {
		t.Fatalf("request metadata not propagated: %+v", body)
	}
}

func TestRouterMountsInstallerBundleWithoutCatchingAPIRoutes(t *testing.T) {
	assets := fstest.MapFS{
		"install/index.html": &fstest.MapFile{Data: []byte("<h1>installer</h1>")},
	}
	router := NewRouterWithComponents(nil, nil, nil, nil, assets)

	for _, item := range []struct {
		path   string
		status int
	}{
		{path: "/install", status: http.StatusOK},
		{path: "/api/admin/v1/ping", status: http.StatusOK},
		{path: "/api/client/v1/ping", status: http.StatusOK},
	} {
		request := httptest.NewRequest(http.MethodGet, item.path, nil)
		request.RemoteAddr = "127.0.0.1:43210"
		request.Host = "127.0.0.1:8080"
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != item.status {
			t.Fatalf("GET %s status = %d, want %d; body=%s", item.path, response.Code, item.status, response.Body.String())
		}
	}
}

func TestRouterRestrictsInstallerPageAndAPIToLoopbackRequests(t *testing.T) {
	assets := fstest.MapFS{
		"install/index.html": &fstest.MapFile{Data: []byte("<h1>installer</h1>")},
	}
	router := NewRouterWithComponents(nil, nil, nil, nil, assets)

	for _, path := range []string{"/install", "/api/system/install/v1/status"} {
		remote := httptest.NewRequest(http.MethodGet, path, nil)
		remote.RemoteAddr = "192.0.2.30:43210"
		remote.Host = "127.0.0.1:8080"
		remoteResponse := httptest.NewRecorder()
		router.ServeHTTP(remoteResponse, remote)
		if remoteResponse.Code != http.StatusForbidden {
			t.Fatalf("remote GET %s status = %d, want 403; body=%s", path, remoteResponse.Code, remoteResponse.Body.String())
		}

		local := httptest.NewRequest(http.MethodGet, path, nil)
		local.RemoteAddr = "127.0.0.1:43210"
		local.Host = "127.0.0.1:8080"
		localResponse := httptest.NewRecorder()
		router.ServeHTTP(localResponse, local)
		if path == "/install" && localResponse.Code != http.StatusOK {
			t.Fatalf("local GET %s status = %d, want 200; body=%s", path, localResponse.Code, localResponse.Body.String())
		}
		if path != "/install" && localResponse.Code == http.StatusForbidden {
			t.Fatalf("local GET %s was rejected as non-loopback", path)
		}
	}
}

func TestRouterMountsSettingsCapabilityWhenProvided(t *testing.T) {
	service := settingsapp.NewService(settingsapp.NewMemoryRepository(), nil, nil, nil)
	handler := settingshttp.NewHandler(service, func(*gin.Context) settingsapp.Actor { return settingsapp.Actor{ID: "admin"} })
	router := NewRouterWithRuntime(nil, nil, nil, nil, nil, nil, adminhttp.AuxiliaryRoutes{Settings: handler})
	request := httptest.NewRequest(http.MethodGet, "/api/admin/v1/settings/site.name", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("settings route status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRouterMountsMetricsAndRecordsRequestsWhenObservabilityEnabled(t *testing.T) {
	runtime, err := observabilityplatform.NewRuntime(observabilityConfigForTest())
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	r := NewRouterWithRuntimeAndObservability(nil, nil, nil, nil, nil, nil, runtime)
	request := httptest.NewRequest(http.MethodGet, "/health/live", nil)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", response.Code, response.Body.String())
	}
	metricsRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsResponse := httptest.NewRecorder()
	r.ServeHTTP(metricsResponse, metricsRequest)
	if metricsResponse.Code != http.StatusOK || !strings.Contains(metricsResponse.Body.String(), `route="/health/live"`) {
		t.Fatalf("metrics status=%d body=%s", metricsResponse.Code, metricsResponse.Body.String())
	}
}

func observabilityConfigForTest() domainobs.Config {
	return domainobs.Config{MetricsEnabled: true, MetricsEndpoint: "http://127.0.0.1:9090/metrics", SampleRate: 1, TLSVerify: true, OTLPProtocol: "http/protobuf"}
}
