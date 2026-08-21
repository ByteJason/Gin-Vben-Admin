package install

import (
	"bytes"
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

func TestPlanEndpointReturnsAllowlistedPermissionSummary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	want := installer.Plan{
		SelectedUI:      "antd",
		Mode:            "embedded",
		CanCleanup:      true,
		CanBuild:        true,
		CanWriteEnv:     true,
		RequiresRestart: true,
		Entries: []installer.PlanEntry{{
			Path:   "admin/apps/web-ele",
			Action: installer.ActionRemove,
			Permission: installer.PathPermission{
				CanRead: true, CanWrite: true, CanCreate: true, CanRename: true, CanDelete: true,
			},
		}},
	}
	provider := &planProviderStub{plan: want}
	RegisterRoutes(router, NewHandlerWithComponents(statusProviderStub{}, capabilityProviderStub{}, provider))

	request := httptest.NewRequest(http.MethodPost, "/api/system/install/v1/plan", bytes.NewBufferString(`{"selectedUi":"antd","mode":"embedded"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Code int            `json:"code"`
		Data installer.Plan `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != 0 || body.Data.SelectedUI != want.SelectedUI || len(body.Data.Entries) != 1 {
		t.Fatalf("unexpected plan response: %#v", body)
	}
	if provider.request.SelectedUI != "antd" || provider.request.Mode != "embedded" {
		t.Fatalf("provider request = %#v", provider.request)
	}
	for _, forbidden := range []string{"/private/", "c:\\users", "password", "secret", "dsn"} {
		if strings.Contains(strings.ToLower(response.Body.String()), forbidden) {
			t.Fatalf("plan response leaked %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestPlanEndpointRejectsMalformedRequestAndHidesProviderFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name     string
		body     string
		provider *planProviderStub
		status   int
	}{
		{name: "malformed", body: `{"selectedUi":`, provider: &planProviderStub{}, status: http.StatusBadRequest},
		{name: "provider", body: `{"selectedUi":"antd","mode":"embedded"}`, provider: &planProviderStub{err: errors.New("/private/root password=fixture")}, status: http.StatusBadRequest},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			router := gin.New()
			RegisterRoutes(router, NewHandlerWithComponents(statusProviderStub{}, capabilityProviderStub{}, testCase.provider))
			request := httptest.NewRequest(http.MethodPost, "/api/system/install/v1/plan", bytes.NewBufferString(testCase.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != testCase.status {
				t.Fatalf("status code = %d, want %d; body=%s", response.Code, testCase.status, response.Body.String())
			}
			if strings.Contains(strings.ToLower(response.Body.String()), "private") || strings.Contains(strings.ToLower(response.Body.String()), "password") {
				t.Fatalf("plan error leaked internal cause: %s", response.Body.String())
			}
		})
	}
}

func TestDependencyCheckEndpointsReturnCredentialFreeResults(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &dependencyProviderStub{
		database: installer.DependencyCheck{Kind: "database", Driver: "mysql", Mode: "single", OK: true, Reason: "reachable", LatencyMS: 2},
		redis:    installer.DependencyCheck{Kind: "redis", Mode: "single", OK: true, Reason: "reachable", LatencyMS: 1},
	}
	router := gin.New()
	RegisterRoutes(router, NewHandlerWithComponents(statusProviderStub{}, capabilityProviderStub{}, nil, provider))
	requests := []struct {
		path string
		body string
	}{
		{path: "/api/system/install/v1/check/database", body: `{"driver":"mysql","mode":"single","host":"db","port":3306,"database":"app","username":"root","password":"secret"}`},
		{path: "/api/system/install/v1/check/redis", body: `{"mode":"single","addr":"redis:6379","password":"secret"}`},
	}
	for _, item := range requests {
		request := httptest.NewRequest(http.MethodPost, item.path, bytes.NewBufferString(item.body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want 200; body=%s", item.path, response.Code, response.Body.String())
		}
		if strings.Contains(strings.ToLower(response.Body.String()), "secret") || strings.Contains(strings.ToLower(response.Body.String()), "password") {
			t.Fatalf("%s response leaked credentials: %s", item.path, response.Body.String())
		}
	}
	if provider.databaseRequest.Password != "secret" || provider.redisRequest.Password != "secret" {
		t.Fatalf("provider requests lost credentials before probe: db=%#v redis=%#v", provider.databaseRequest, provider.redisRequest)
	}
}

func TestDependencyCheckEndpointsHideProbeErrorsAndRejectMalformedJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &dependencyProviderStub{databaseErr: errors.New("dsn=postgres://user:secret@host/app"), redisErr: errors.New("password=secret")}
	router := gin.New()
	RegisterRoutes(router, NewHandlerWithComponents(statusProviderStub{}, nil, nil, provider))
	for _, item := range []struct {
		path string
		body string
	}{
		{path: "/api/system/install/v1/check/database", body: `{"driver":"mysql"`},
		{path: "/api/system/install/v1/check/redis", body: `{"mode":"single","addr":"redis:6379"}`},
	} {
		request := httptest.NewRequest(http.MethodPost, item.path, bytes.NewBufferString(item.body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400; body=%s", item.path, response.Code, response.Body.String())
		}
		if strings.Contains(strings.ToLower(response.Body.String()), "secret") || strings.Contains(strings.ToLower(response.Body.String()), "postgres://") {
			t.Fatalf("%s response leaked probe error: %s", item.path, response.Body.String())
		}
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

type planProviderStub struct {
	plan    installer.Plan
	err     error
	request installer.PlanRequest
}

func (s *planProviderStub) Plan(_ context.Context, request installer.PlanRequest) (installer.Plan, error) {
	s.request = request
	return s.plan, s.err
}

type dependencyProviderStub struct {
	database        installer.DependencyCheck
	redis           installer.DependencyCheck
	databaseErr     error
	redisErr        error
	databaseRequest installer.DatabaseConnection
	redisRequest    installer.RedisConnection
}

func (s *dependencyProviderStub) CheckDatabase(_ context.Context, request installer.DatabaseConnection) (installer.DependencyCheck, error) {
	s.databaseRequest = request
	return s.database, s.databaseErr
}

func (s *dependencyProviderStub) CheckRedis(_ context.Context, request installer.RedisConnection) (installer.DependencyCheck, error) {
	s.redisRequest = request
	return s.redis, s.redisErr
}
