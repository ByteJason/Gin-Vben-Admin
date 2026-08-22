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

func TestApplyEndpointReturnsCredentialFreeResult(t *testing.T) {
	gin.SetMode(gin.TestMode)
	provider := &applyProviderStub{result: installer.ApplyResult{
		State: "installed", SelectedUI: "antd", Mode: "embedded", InstalledAt: time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC),
		Steps: []installer.ApplyStep{{ID: "lock", Status: installer.StepCompleted}},
	}}
	router := gin.New()
	RegisterRoutes(router, NewHandlerWithApply(statusProviderStub{}, nil, nil, nil, provider))

	body := `{"selectedUi":"antd","mode":"embedded","confirmCleanup":true,"database":{"driver":"mysql","mode":"single","host":"db","port":3306,"database":"app","username":"root","password":"database-secret"},"redis":{"mode":"single","addr":"redis:6379","password":"redis-secret"},"admin":{"username":"admin","password":"administrator-secret"}}`
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
	body := `{"selectedUi":"antd","mode":"embedded","confirmCleanup":true,"database":{"driver":"mysql","mode":"single","host":"db","port":3306,"database":"app","username":"root","password":"database-secret"},"redis":{"mode":"single","addr":"redis:6379"},"admin":{"username":"admin","password":"administrator-secret"}}`
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
			request := httptest.NewRequest(http.MethodPost, "/api/system/install/v1/apply", bytes.NewBufferString(`{"selectedUi":"antd"}`))
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
	provider := &applyProviderStub{}
	router := gin.New()
	RegisterRoutes(router, NewHandlerWithApply(statusProviderStub{}, nil, nil, nil, provider))
	request := httptest.NewRequest(http.MethodPost, "/api/system/install/v1/apply", bytes.NewBufferString(`{"selectedUi":"antd","unexpected":true}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", response.Code, response.Body.String())
	}
	if provider.calls != 0 {
		t.Fatalf("apply service calls = %d, want 0", provider.calls)
	}
}

func TestAsyncApplyEndpointReturnsCredentialFreeJob(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jobProvider := &jobProviderStub{job: installer.ApplyJob{ID: "install-job-1", State: installer.JobRunning, CurrentStep: "queued"}}
	router := gin.New()
	RegisterRoutes(router, NewHandlerWithApplyAndJobs(statusProviderStub{}, nil, nil, nil, nil, jobProvider))
	body := `{"selectedUi":"antd","mode":"embedded","confirmCleanup":true,"database":{"driver":"mysql","mode":"single","dsn":"user:secret@tcp(db:3306)/app"},"redis":{"mode":"single","addr":"redis:6379"},"admin":{"username":"admin","password":"administrator-secret"}}`
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
	jobProvider := &jobProviderStub{job: installer.ApplyJob{ID: "install-job-1", State: installer.JobFailed, CanRetry: true, ErrorCode: 50000}}
	router := gin.New()
	RegisterRoutes(router, NewHandlerWithApplyAndJobs(statusProviderStub{}, nil, nil, nil, nil, jobProvider))

	progress := httptest.NewRecorder()
	router.ServeHTTP(progress, httptest.NewRequest(http.MethodGet, "/api/system/install/v1/progress/install-job-1", nil))
	if progress.Code != http.StatusOK || !strings.Contains(progress.Body.String(), `"state":"failed"`) {
		t.Fatalf("progress response = %d %s", progress.Code, progress.Body.String())
	}

	body := `{"selectedUi":"antd","mode":"embedded","confirmCleanup":true,"database":{"driver":"mysql","mode":"single","dsn":"user:secret@tcp(db:3306)/app"},"redis":{"mode":"single","addr":"redis:6379"},"admin":{"username":"admin","password":"administrator-secret"}}`
	retry := httptest.NewRecorder()
	retryRequest := httptest.NewRequest(http.MethodPost, "/api/system/install/v1/retry/install-job-1", bytes.NewBufferString(body))
	retryRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(retry, retryRequest)
	if retry.Code != http.StatusAccepted || jobProvider.retryID != "install-job-1" {
		t.Fatalf("retry response = %d %s; id=%q", retry.Code, retry.Body.String(), jobProvider.retryID)
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
