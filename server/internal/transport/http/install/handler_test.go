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
	"time"

	"github.com/gin-gonic/gin"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
)

func TestStatusEndpointReturnsCredentialFreeInstallationState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterRoutes(router, NewHandler(statusProviderStub{status: installer.Status{
		State:            installer.StatePristine,
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
	if body.Code != 0 || body.Data.Installed || body.Data.State != installer.StatePristine {
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
			Action: installer.ActionKeep,
			Permission: installer.PathPermission{
				CanRead: true, CanWrite: true, CanCreate: true, CanRename: true, CanDelete: true,
			},
		}},
	}
	provider := &planProviderStub{plan: want}
	RegisterRoutes(router, NewHandlerWithComponents(statusProviderStub{}, capabilityProviderStub{}, provider))

	request := httptest.NewRequest(http.MethodPost, "/api/system/install/v1/plan", bytes.NewBufferString(`{"mode":"embedded"}`))
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
	if provider.request.Mode != "embedded" {
		t.Fatalf("provider request = %#v", provider.request)
	}
	for _, forbidden := range []string{"/private/", "c:\\users", "password", "secret", "dsn"} {
		if strings.Contains(strings.ToLower(response.Body.String()), forbidden) {
			t.Fatalf("plan response leaked %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestPlanEndpointAcceptsOnlyModeAndRejectsLegacySelection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &planProviderStub{plan: installer.Plan{SelectedUI: "antd", Mode: "embedded"}}
	router := gin.New()
	RegisterRoutes(router, NewHandlerWithComponents(statusProviderStub{}, nil, provider))

	request := httptest.NewRequest(http.MethodPost, "/api/system/install/v1/plan", bytes.NewBufferString(`{"mode":"embedded"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || provider.request.Mode != "embedded" {
		t.Fatalf("mode-only plan = %d %s; request=%#v", response.Code, response.Body.String(), provider.request)
	}

	legacy := httptest.NewRecorder()
	legacyRequest := httptest.NewRequest(http.MethodPost, "/api/system/install/v1/plan", bytes.NewBufferString(`{"mode":"embedded","selectedUi":"antd"}`))
	legacyRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(legacy, legacyRequest)
	if legacy.Code != http.StatusBadRequest {
		t.Fatalf("legacy selection status = %d, want 400; body=%s", legacy.Code, legacy.Body.String())
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
		{name: "provider", body: `{"mode":"embedded"}`, provider: &planProviderStub{err: errors.New("/private/root password=fixture")}, status: http.StatusBadRequest},
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

func TestApplyEndpointReturnsCredentialFreeResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &applyProviderStub{result: installer.ApplyResult{
		State: "installed", SelectedUI: "antd", Mode: "embedded", InstalledAt: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
		Steps: []installer.ApplyStep{{ID: "lock", Status: installer.StepCompleted}},
	}}
	router := gin.New()
	RegisterRoutes(router, NewHandlerWithApply(statusProviderStub{}, nil, nil, nil, provider))

	body := `{"mode":"embedded","database":{"driver":"mysql","mode":"single","host":"db","port":3306,"database":"app","username":"root","password":"database-secret"},"redis":{"mode":"single","addr":"redis:6379","password":"redis-secret"},"admin":{"username":"admin","password":"administrator-secret"}}`
	request := httptest.NewRequest(http.MethodPost, "/api/system/install/v1/apply", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	if provider.request.Admin.Password != "administrator-secret" || provider.request.Database.Password != "database-secret" {
		t.Fatalf("provider did not receive installation credentials")
	}
	for _, forbidden := range []string{"administrator-secret", "database-secret", "redis-secret", "password", "dsn"} {
		if strings.Contains(strings.ToLower(response.Body.String()), forbidden) {
			t.Fatalf("apply response leaked %q: %s", forbidden, response.Body.String())
		}
	}
	var envelope struct {
		Code int                   `json:"code"`
		Data installer.ApplyResult `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Code != 0 || envelope.Data.State != "installed" || len(envelope.Data.Steps) != 1 {
		t.Fatalf("unexpected apply response: %#v", envelope)
	}
}

func TestApplyEndpointDerivesLocaleSuggestionFromAcceptLanguage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &applyProviderStub{result: installer.ApplyResult{State: "installed", SelectedUI: "antd", Mode: "embedded"}}
	router := gin.New()
	RegisterRoutes(router, NewHandlerWithApply(statusProviderStub{}, nil, nil, nil, provider))
	body := `{"mode":"embedded","database":{"driver":"mysql","mode":"single","host":"db","port":3306,"database":"app","username":"root","password":"database-secret"},"redis":{"mode":"single","addr":"redis:6379"},"admin":{"username":"admin","password":"administrator-secret"}}`
	request := httptest.NewRequest(http.MethodPost, "/api/system/install/v1/apply", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept-Language", "en-GB,en;q=0.8")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	if provider.request.Locale != "en-US" || provider.request.LocaleMode != "single" {
		t.Fatalf("derived locale policy = locale:%q mode:%q", provider.request.Locale, provider.request.LocaleMode)
	}
}

func TestApplyEndpointMapsStableErrorsWithoutLeakingCause(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, testCase := range []struct {
		name   string
		err    error
		status int
		code   int
	}{
		{name: "installed", err: installer.ErrAlreadyInstalled, status: http.StatusConflict, code: 10006},
		{name: "busy", err: installer.ErrApplyBusy, status: http.StatusConflict, code: 10007},
		{name: "invalid", err: installer.ErrInvalidApply, status: http.StatusBadRequest, code: 10000},
		{name: "preflight", err: installer.ErrPreflightFailed, status: http.StatusUnprocessableEntity, code: 10001},
		{name: "internal", err: errors.New("password=must-not-leak"), status: http.StatusInternalServerError, code: 50000},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			router := gin.New()
			RegisterRoutes(router, NewHandlerWithApply(statusProviderStub{}, nil, nil, nil, &applyProviderStub{err: testCase.err}))
			request := httptest.NewRequest(http.MethodPost, "/api/system/install/v1/apply", bytes.NewBufferString(`{"mode":"embedded"}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != testCase.status {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, testCase.status, response.Body.String())
			}
			var envelope struct {
				Code int `json:"code"`
			}
			if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if envelope.Code != testCase.code {
				t.Fatalf("code = %d, want %d", envelope.Code, testCase.code)
			}
			if strings.Contains(strings.ToLower(response.Body.String()), "must-not-leak") || strings.Contains(strings.ToLower(response.Body.String()), "password") {
				t.Fatalf("response leaked apply error: %s", response.Body.String())
			}
		})
	}
}

func TestApplyEndpointRejectsUnknownFieldsBeforeCallingService(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{
		`{"mode":"embedded","selectedUi":"antd"}`,
		`{"mode":"embedded","confirmCleanup":true}`,
		`{"mode":"embedded","unexpected":true}`,
	} {
		provider := &applyProviderStub{}
		router := gin.New()
		RegisterRoutes(router, NewHandlerWithApply(statusProviderStub{}, nil, nil, nil, provider))
		request := httptest.NewRequest(http.MethodPost, "/api/system/install/v1/apply", bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status = %d, want 400; response=%s", body, response.Code, response.Body.String())
		}
		if provider.calls != 0 {
			t.Fatalf("body=%s apply service calls = %d, want 0", body, provider.calls)
		}
	}
}

func TestAsyncApplyEndpointReturnsCredentialFreeJob(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jobProvider := &jobProviderStub{job: installer.ApplyJob{ID: "install-job-1", State: installer.JobRunning, CurrentStep: "queued"}}
	router := gin.New()
	RegisterRoutes(router, NewHandlerWithApplyAndJobs(statusProviderStub{}, nil, nil, nil, nil, jobProvider))
	body := `{"mode":"embedded","database":{"driver":"mysql","mode":"single","dsn":"user:secret@tcp(db:3306)/app"},"redis":{"mode":"single","addr":"redis:6379"},"admin":{"username":"admin","password":"administrator-secret"}}`
	request := httptest.NewRequest(http.MethodPost, "/api/system/install/v1/apply", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(strings.ToLower(response.Body.String()), "secret") || strings.Contains(strings.ToLower(response.Body.String()), "password") {
		t.Fatalf("async apply response leaked credentials: %s", response.Body.String())
	}
	if jobProvider.request.Admin.Password != "administrator-secret" {
		t.Fatalf("job provider did not receive admin credential")
	}
}

func TestInstallationProgressAndRetryEndpointsUseJobProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jobProvider := &jobProviderStub{job: installer.ApplyJob{
		ID: "install-job-1", State: installer.JobFailed, CanRetry: true,
		ErrorCode: 50000, ErrorKey: "internal_error", FailureStep: "schema",
		FailureReason: "permission_denied", FailureOperation: "apply", DatabaseCode: "42501",
	}}
	router := gin.New()
	RegisterRoutes(router, NewHandlerWithApplyAndJobs(statusProviderStub{}, nil, nil, nil, nil, jobProvider))

	progress := httptest.NewRecorder()
	router.ServeHTTP(progress, httptest.NewRequest(http.MethodGet, "/api/system/install/v1/progress/install-job-1", nil))
	if progress.Code != http.StatusOK ||
		!strings.Contains(progress.Body.String(), `"state":"failed"`) ||
		!strings.Contains(progress.Body.String(), `"failureStep":"schema"`) ||
		!strings.Contains(progress.Body.String(), `"failureReason":"permission_denied"`) ||
		!strings.Contains(progress.Body.String(), `"failureOperation":"apply"`) ||
		!strings.Contains(progress.Body.String(), `"databaseCode":"42501"`) {
		t.Fatalf("progress response = %d %s", progress.Code, progress.Body.String())
	}

	body := `{"mode":"embedded","database":{"driver":"mysql","mode":"single","dsn":"user:secret@tcp(db:3306)/app"},"redis":{"mode":"single","addr":"redis:6379"},"admin":{"username":"admin","password":"administrator-secret"}}`
	retry := httptest.NewRecorder()
	retryRequest := httptest.NewRequest(http.MethodPost, "/api/system/install/v1/retry/install-job-1", bytes.NewBufferString(body))
	retryRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(retry, retryRequest)
	if retry.Code != http.StatusAccepted || jobProvider.retryID != "install-job-1" {
		t.Fatalf("retry response = %d %s; id=%q", retry.Code, retry.Body.String(), jobProvider.retryID)
	}
}

func TestInstallationProgressExposesNavigationConflictResource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jobProvider := &jobProviderStub{job: installer.ApplyJob{
		ID: "install-job-seed", State: installer.JobFailed, CanRetry: true,
		ErrorCode: 50000, ErrorKey: "internal_error", FailureStep: "identity",
		FailureReason: "navigation_seed_conflict", FailureOperation: "apply",
		FailureResourceKind: "permission", FailureResourceID: "iam:users:read",
	}}
	router := gin.New()
	RegisterRoutes(router, NewHandlerWithApplyAndJobs(statusProviderStub{}, nil, nil, nil, nil, jobProvider))

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/system/install/v1/progress/install-job-seed", nil))
	body := response.Body.String()
	if response.Code != http.StatusOK ||
		!strings.Contains(body, `"failureReason":"navigation_seed_conflict"`) ||
		!strings.Contains(body, `"failureResourceKind":"permission"`) ||
		!strings.Contains(body, `"failureResourceId":"iam:users:read"`) {
		t.Fatalf("progress response = %d %s", response.Code, body)
	}
}

func TestInstallationRollbackEndpointRequiresConfirmationAndUsesProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &rollbackProviderStub{result: installer.RollbackResult{
		JobID: "install-job-1", State: installer.JobFailed, CurrentStep: "rolled_back", CanRetry: true, RolledBack: true,
	}}
	router := gin.New()
	RegisterRoutes(router, NewHandlerWithApplyAndJobs(statusProviderStub{}, nil, nil, nil, nil, provider))

	missing := httptest.NewRecorder()
	missingRequest := httptest.NewRequest(http.MethodPost, "/api/system/install/v1/rollback/install-job-1", bytes.NewBufferString(`{"confirmRollback":false}`))
	missingRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(missing, missingRequest)
	if missing.Code != http.StatusBadRequest || provider.calls != 0 {
		t.Fatalf("unconfirmed rollback = %d %s; calls=%d", missing.Code, missing.Body.String(), provider.calls)
	}

	confirmed := httptest.NewRecorder()
	confirmedRequest := httptest.NewRequest(http.MethodPost, "/api/system/install/v1/rollback/install-job-1", bytes.NewBufferString(`{"confirmRollback":true}`))
	confirmedRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(confirmed, confirmedRequest)
	if confirmed.Code != http.StatusOK || provider.id != "install-job-1" || !provider.confirmed {
		t.Fatalf("confirmed rollback = %d %s; id=%q confirmed=%v", confirmed.Code, confirmed.Body.String(), provider.id, provider.confirmed)
	}
}

func TestUIPreparationEndpointAcceptsConfirmedSelectionAndRejectsUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &uiPreparationProviderStub{job: installer.UIPreparationJob{
		ID: "ui-prepare-job-1", Action: installer.UIPreparationActionPrepare,
		State: installer.UIPreparationJobQueued, SelectedUI: "ele",
		CurrentStep: "queued", Progress: 0,
	}}
	handler := NewHandler(statusProviderStub{})
	handler.SetUIPreparationProvider(provider)
	router := gin.New()
	RegisterRoutes(router, handler)

	accepted := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/system/install/v1/ui/prepare", bytes.NewBufferString(`{"selectedUi":"ele","confirmCleanup":true}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(accepted, request)
	if accepted.Code != http.StatusAccepted || provider.prepareCalls != 1 || provider.prepareRequest.SelectedUI != "ele" || !provider.prepareRequest.ConfirmCleanup {
		t.Fatalf("prepare response = %d %s; calls=%d request=%#v", accepted.Code, accepted.Body.String(), provider.prepareCalls, provider.prepareRequest)
	}
	for _, expected := range []string{`"id":"ui-prepare-job-1"`, `"action":"prepare"`, `"state":"queued"`, `"selectedUi":"ele"`, `"currentStep":"queued"`} {
		if !strings.Contains(accepted.Body.String(), expected) {
			t.Fatalf("prepare response missing %s: %s", expected, accepted.Body.String())
		}
	}

	rejected := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/system/install/v1/ui/prepare", bytes.NewBufferString(`{"selectedUi":"antd","confirmCleanup":true,"command":"sh -c fixture"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rejected, request)
	if rejected.Code != http.StatusBadRequest || provider.prepareCalls != 1 {
		t.Fatalf("unknown-field response = %d %s; calls=%d", rejected.Code, rejected.Body.String(), provider.prepareCalls)
	}
}

func TestUIPreparationEndpointsRequireConfirmationAndMapStableConflicts(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		providerErr error
		wantStatus  int
		wantMessage string
	}{
		{name: "confirmation", body: `{"selectedUi":"antd","confirmCleanup":false}`, wantStatus: http.StatusBadRequest},
		{name: "invalid ui", body: `{"selectedUi":"unknown","confirmCleanup":true}`, providerErr: installer.ErrUIPreparationInvalid, wantStatus: http.StatusBadRequest},
		{name: "busy", body: `{"selectedUi":"antd","confirmCleanup":true}`, providerErr: installer.ErrUIPreparationConflict, wantStatus: http.StatusConflict},
		{name: "installed", body: `{"selectedUi":"antd","confirmCleanup":true}`, providerErr: installer.ErrUIPreparationInstalled, wantStatus: http.StatusConflict},
		{name: "service closed", body: `{"selectedUi":"antd","confirmCleanup":true}`, providerErr: installer.ErrUIPreparationServiceClosed, wantStatus: http.StatusServiceUnavailable, wantMessage: "installation service unavailable"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			provider := &uiPreparationProviderStub{err: testCase.providerErr}
			handler := NewHandler(statusProviderStub{})
			handler.SetUIPreparationProvider(provider)
			router := gin.New()
			RegisterRoutes(router, handler)

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/system/install/v1/ui/prepare", bytes.NewBufferString(testCase.body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(response, request)
			if response.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, testCase.wantStatus, response.Body.String())
			}
			if testCase.wantMessage != "" && !strings.Contains(response.Body.String(), `"message":"`+testCase.wantMessage+`"`) {
				t.Fatalf("body = %s, want message %q", response.Body.String(), testCase.wantMessage)
			}
			if strings.Contains(response.Body.String(), "sh -c") || strings.Contains(response.Body.String(), "private") {
				t.Fatalf("response leaks process detail: %s", response.Body.String())
			}
		})
	}
}

func TestUIPreparationProgressAndResetUseDedicatedProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &uiPreparationProviderStub{job: installer.UIPreparationJob{
		ID: "ui-prepare-job-2", Action: installer.UIPreparationActionReset,
		State: installer.UIPreparationJobRunning, CurrentStep: "reset",
		Progress: 55,
	}}
	handler := NewHandler(statusProviderStub{})
	handler.SetUIPreparationProvider(provider)
	router := gin.New()
	RegisterRoutes(router, handler)

	progress := httptest.NewRecorder()
	router.ServeHTTP(progress, httptest.NewRequest(http.MethodGet, "/api/system/install/v1/ui/progress/ui-prepare-job-2", nil))
	if progress.Code != http.StatusOK || provider.progressID != "ui-prepare-job-2" || !strings.Contains(progress.Body.String(), `"progress":55`) {
		t.Fatalf("progress response = %d %s; id=%q", progress.Code, progress.Body.String(), provider.progressID)
	}

	reset := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/system/install/v1/ui/reset", bytes.NewBufferString(`{"confirmReset":true}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(reset, request)
	if reset.Code != http.StatusAccepted || provider.resetCalls != 1 || !provider.resetRequest.ConfirmReset {
		t.Fatalf("reset response = %d %s; calls=%d request=%#v", reset.Code, reset.Body.String(), provider.resetCalls, provider.resetRequest)
	}
}

func TestUIPreparationProgressReturnsSafeStructuredFailureDiagnostic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &uiPreparationProviderStub{job: installer.UIPreparationJob{
		ID: "ui-prepare-job-failed", Action: installer.UIPreparationActionPrepare,
		State: installer.UIPreparationJobFailed, CurrentStep: "failed", Progress: 10,
		ErrorKey: "ui_preflight_failed", FailureStep: "preflight",
		FailureReason: "preflight_failed", FailureScope: "admin_apps",
		FailureOperation: "cross_directory_rename",
	}}
	handler := NewHandler(statusProviderStub{})
	handler.SetUIPreparationProvider(provider)
	router := gin.New()
	RegisterRoutes(router, handler)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/system/install/v1/ui/progress/ui-prepare-job-failed", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("progress response = %d %s", response.Code, response.Body.String())
	}
	for _, expected := range []string{
		`"errorKey":"ui_preflight_failed"`, `"failureStep":"preflight"`,
		`"failureReason":"preflight_failed"`, `"failureScope":"admin_apps"`,
		`"failureOperation":"cross_directory_rename"`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("progress response missing %s: %s", expected, response.Body.String())
		}
	}
	if strings.Contains(response.Body.String(), "dependency-install.log") {
		t.Fatalf("preflight response exposed irrelevant dependency log: %s", response.Body.String())
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

type applyProviderStub struct {
	result  installer.ApplyResult
	err     error
	request installer.ApplyRequest
	calls   int
}

type jobProviderStub struct {
	job     installer.ApplyJob
	err     error
	request installer.ApplyRequest
	retryID string
}

type rollbackProviderStub struct {
	result    installer.RollbackResult
	id        string
	confirmed bool
	calls     int
}

type uiPreparationProviderStub struct {
	job            installer.UIPreparationJob
	err            error
	prepareRequest installer.UIPrepareRequest
	resetRequest   installer.UIResetRequest
	prepareCalls   int
	resetCalls     int
	progressID     string
}

func (s *uiPreparationProviderStub) StartPrepare(_ context.Context, request installer.UIPrepareRequest) (installer.UIPreparationJob, error) {
	s.prepareCalls++
	s.prepareRequest = request
	return s.job, s.err
}

func (s *uiPreparationProviderStub) StartReset(_ context.Context, request installer.UIResetRequest) (installer.UIPreparationJob, error) {
	s.resetCalls++
	s.resetRequest = request
	return s.job, s.err
}

func (s *uiPreparationProviderStub) Progress(_ context.Context, id string) (installer.UIPreparationJob, error) {
	s.progressID = id
	return s.job, s.err
}

func (s *rollbackProviderStub) Start(_ context.Context, request installer.ApplyRequest) (installer.ApplyJob, error) {
	return installer.ApplyJob{}, nil
}

func (s *rollbackProviderStub) Progress(context.Context, string) (installer.ApplyJob, error) {
	return installer.ApplyJob{}, nil
}

func (s *rollbackProviderStub) Retry(context.Context, string, installer.ApplyRequest) (installer.ApplyJob, error) {
	return installer.ApplyJob{}, nil
}

func (s *rollbackProviderStub) Rollback(_ context.Context, id string, confirmed bool) (installer.RollbackResult, error) {
	s.calls++
	s.id = id
	s.confirmed = confirmed
	return s.result, nil
}

func (s *jobProviderStub) Start(_ context.Context, request installer.ApplyRequest) (installer.ApplyJob, error) {
	s.request = request
	return s.job, s.err
}

func (s *jobProviderStub) Progress(context.Context, string) (installer.ApplyJob, error) {
	return s.job, s.err
}

func (s *jobProviderStub) Retry(_ context.Context, id string, request installer.ApplyRequest) (installer.ApplyJob, error) {
	s.retryID = id
	s.request = request
	return s.job, s.err
}

func (s *applyProviderStub) Apply(_ context.Context, request installer.ApplyRequest) (installer.ApplyResult, error) {
	s.calls++
	s.request = request
	return s.result, s.err
}

func (s *dependencyProviderStub) CheckDatabase(_ context.Context, request installer.DatabaseConnection) (installer.DependencyCheck, error) {
	s.databaseRequest = request
	return s.database, s.databaseErr
}

func (s *dependencyProviderStub) CheckRedis(_ context.Context, request installer.RedisConnection) (installer.DependencyCheck, error) {
	s.redisRequest = request
	return s.redis, s.redisErr
}
