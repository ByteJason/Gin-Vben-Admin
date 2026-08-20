package install

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	installer "example.com/gin-vben-admin/server/internal/application/installer"
)

func TestStatusEndpointReturnsCredentialFreeInstallationState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router, NewHandler(statusProviderStub{status: installer.Status{
		State:            installer.StateUninstalled,
		Installed:        false,
		SchemaVersion:    1,
		InstallerVersion: "0.4.0-dev",
	}}))

	request := httptest.NewRequest(http.MethodGet, "/api/system/install/v1/status", nil)
	request.Header.Set("X-Request-ID", "REQ-install-status")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Code int              `json:"code"`
		Data installer.Status `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != 0 || body.Data.Installed || body.Data.State != installer.StateUninstalled {
		t.Fatalf("unexpected status response: %#v", body)
	}
	for _, forbidden := range []string{"password", "secret", "dsn", "token"} {
		if strings.Contains(strings.ToLower(response.Body.String()), forbidden) {
			t.Fatalf("status response contains credential field %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestStatusEndpointMapsUnreadableMarkerWithoutLeakingCause(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router, NewHandler(statusProviderStub{err: errors.New("fixture password=do-not-leak")}))

	request := httptest.NewRequest(http.MethodGet, "/api/system/install/v1/status", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status code = %d, want 500; body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(strings.ToLower(response.Body.String()), "do-not-leak") || strings.Contains(strings.ToLower(response.Body.String()), "password") {
		t.Fatalf("status response leaked internal marker error: %s", response.Body.String())
	}
	var body struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != 50000 {
		t.Fatalf("error code = %d, want 50000", body.Code)
	}
}

func TestCapabilitiesEndpointReturnsOnlyAllowlistedRuntimeFacts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router, NewHandler(statusProviderStub{}, capabilityProviderStub{capabilities: installer.Capabilities{
		Platform: installer.PlatformCapability{OS: "windows", Arch: "amd64"},
		Tools: []installer.ToolCapability{
			{ID: "go", Available: true, Version: "go1.24.6"},
			{ID: "docker", Available: false, Reason: "not_available"},
		},
	}}))

	request := httptest.NewRequest(http.MethodGet, "/api/system/install/v1/capabilities", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Code int                    `json:"code"`
		Data installer.Capabilities `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != 0 || body.Data.Platform.OS != "windows" || len(body.Data.Tools) != 2 {
		t.Fatalf("unexpected capabilities response: %#v", body)
	}
	for _, forbidden := range []string{"/private/", "c:\\users", "password", "secret"} {
		if strings.Contains(strings.ToLower(response.Body.String()), forbidden) {
			t.Fatalf("capabilities response leaked %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestCapabilitiesEndpointHidesProbeFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router, NewHandler(statusProviderStub{}, capabilityProviderStub{err: errors.New("/private/tool password fixture")}))

	request := httptest.NewRequest(http.MethodGet, "/api/system/install/v1/capabilities", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status code = %d, want 500; body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(strings.ToLower(response.Body.String()), "private") || strings.Contains(strings.ToLower(response.Body.String()), "password") {
		t.Fatalf("capabilities error leaked internal cause: %s", response.Body.String())
	}
}

type statusProviderStub struct {
	status installer.Status
	err    error
}

func (s statusProviderStub) Status(context.Context) (installer.Status, error) {
	return s.status, s.err
}

type capabilityProviderStub struct {
	capabilities installer.Capabilities
	err          error
}

func (s capabilityProviderStub) Probe(context.Context) (installer.Capabilities, error) {
	return s.capabilities, s.err
}
