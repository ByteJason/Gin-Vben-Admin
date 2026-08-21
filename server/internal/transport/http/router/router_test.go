package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	settingsapp "example.com/gin-vben-admin/server/internal/application/settings"
	adminhttp "example.com/gin-vben-admin/server/internal/transport/http/admin"
	settingshttp "example.com/gin-vben-admin/server/internal/transport/http/settings"
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
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != item.status {
			t.Fatalf("GET %s status = %d, want %d; body=%s", item.path, response.Code, item.status, response.Body.String())
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
