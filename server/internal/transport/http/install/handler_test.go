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

type statusProviderStub struct {
	status installer.Status
	err    error
}

func (s statusProviderStub) Status(context.Context) (installer.Status, error) {
	return s.status, s.err
}
