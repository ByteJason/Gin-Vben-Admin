package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appauth "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/auth"
	iamapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/iam"
	monitorapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/monitor"
	settingsapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/settings"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/config"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
	domainobs "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/observability"
	observabilityplatform "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/observability"
)

func newLocalInstallerRequest(method, target string, body io.Reader) *http.Request {
	request := httptest.NewRequest(method, target, body)
	request.RemoteAddr = "127.0.0.1:49152"
	request.Host = "127.0.0.1:8080"
	return request
}

func isolateBootstrapRuntime(t *testing.T, cfg *config.Config) {
	t.Helper()
	root := t.TempDir()
	cfg.Install.StateDir = filepath.Join(root, ".runtime", "install")
	cfg.Install.WorkspaceRoot = root
	cfg.File.Root = filepath.Join(root, ".runtime", "files")
}

func TestNewBuildsConfiguredHTTPServerAndKeepsDependenciesOptional(t *testing.T) {
	cfg := config.Default()
	workspaceRoot := t.TempDir()
	cfg.Install.StateDir = filepath.Join(workspaceRoot, "admin", "apps", "install")
	cfg.Install.WorkspaceRoot = workspaceRoot
	cfg.Server.Addr = "127.0.0.1:0"
	cfg.Server.ReadTimeout = 123 * time.Millisecond
	cfg.Server.WriteTimeout = 234 * time.Millisecond
	cfg.Server.IdleTimeout = 345 * time.Millisecond
	cfg.Server.ShutdownTimeout = 456 * time.Millisecond

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	if app.HTTPServer() == nil {
		t.Fatal("HTTPServer() returned nil")
	}
	server := app.HTTPServer()
	if server.Addr != cfg.Server.Addr {
		t.Fatalf("server addr = %q, want %q", server.Addr, cfg.Server.Addr)
	}
	if server.ReadTimeout != cfg.Server.ReadTimeout || server.WriteTimeout != cfg.Server.WriteTimeout || server.IdleTimeout != cfg.Server.IdleTimeout {
		t.Fatalf("server timeouts = (%s, %s, %s), want (%s, %s, %s)", server.ReadTimeout, server.WriteTimeout, server.IdleTimeout, cfg.Server.ReadTimeout, cfg.Server.WriteTimeout, cfg.Server.IdleTimeout)
	}
	if app.Database() != nil || app.Redis() != nil {
		t.Fatal("disabled dependencies must not be constructed")
	}
	if app.Settings() == nil {
		t.Fatal("local single-node fixture should expose in-process settings for branding")
	}
	if app.Readiness() == nil {
		t.Fatal("readiness checker must always be constructed")
	}

	request := newLocalInstallerRequest(http.MethodGet, "/api/system/install/v1/status", nil)
	response := httptest.NewRecorder()
	app.HTTPServer().Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("installation status = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			Installed bool   `json:"installed"`
			State     string `json:"state"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode installation status: %v", err)
	}
	if body.Code != 0 || body.Data.Installed || body.Data.State != "pristine" {
		t.Fatalf("installation status body = %#v", body)
	}

	request = newLocalInstallerRequest(http.MethodGet, "/api/system/install/v1/capabilities", nil)
	response = httptest.NewRecorder()
	app.HTTPServer().Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("installation capabilities = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	var capabilitiesBody struct {
		Code int `json:"code"`
		Data struct {
			Platform struct {
				OS   string `json:"os"`
				Arch string `json:"arch"`
			} `json:"platform"`
			Tools []struct {
				ID string `json:"id"`
			} `json:"tools"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&capabilitiesBody); err != nil {
		t.Fatalf("decode installation capabilities: %v", err)
	}
	if capabilitiesBody.Code != 0 || capabilitiesBody.Data.Platform.OS == "" || capabilitiesBody.Data.Platform.Arch == "" || len(capabilitiesBody.Data.Tools) != 4 {
		t.Fatalf("installation capabilities body = %#v", capabilitiesBody)
	}

	request = newLocalInstallerRequest(http.MethodGet, "/install", nil)
	response = httptest.NewRecorder()
	app.HTTPServer().Handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("ordinary build installation page = %d, want 404", response.Code)
	}
}

func TestLocalSettingsEndpointUsesServerOwnedActorForBranding(t *testing.T) {
	cfg := config.Default()
	cfg.Auth.Enabled = false
	cfg.Database.Enabled = false
	cfg.Redis.Enabled = false
	service := settingsapp.NewService(settingsapp.NewMemoryRepository(), nil, nil, nil)
	server := newHTTPServerWithPlanAndCaptcha(
		cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, service, nil, nil, nil,
	)
	body := strings.NewReader(`{"value":{"logoResourceId":"asset-1"},"expectedVersion":0}`)
	request := httptest.NewRequest(http.MethodPut, "/api/admin/v1/settings/branding", body)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `logoResourceId`) {
		t.Fatalf("branding update status=%d body=%s", response.Code, response.Body.String())
	}
	get := httptest.NewRequest(http.MethodGet, "/api/admin/v1/settings/branding", nil)
	getResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK || !strings.Contains(getResponse.Body.String(), `logoResourceId`) {
		t.Fatalf("branding get status=%d body=%s", getResponse.Code, getResponse.Body.String())
	}
}

func TestAuthDisabledMultiTenantSettingsStillRequiresActor(t *testing.T) {
	cfg := config.Default()
	cfg.Auth.Enabled = false
	cfg.Tenant.Enabled = true
	cfg.Tenant.Mode = "multi"
	cfg.Database.Enabled = false
	cfg.Redis.Enabled = false
	service := settingsapp.NewService(settingsapp.NewMemoryRepository(), nil, nil, nil)
	server := newHTTPServerWithPlanAndCaptcha(
		cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, service, nil, nil, nil,
	)
	body := strings.NewReader(`{"value":{"logoResourceId":"asset-1"},"expectedVersion":0}`)
	request := httptest.NewRequest(http.MethodPut, "/api/admin/v1/settings/branding", body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(cfg.Tenant.TenantHeader, "tenant-a")
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":30000`) {
		t.Fatalf("multi-tenant settings status=%d body=%s, want forbidden", response.Code, response.Body.String())
	}
}

func TestNewReconcilesStaleApplyLeaseBeforePristineInstaller(t *testing.T) {
	cfg := config.Default()
	isolateBootstrapRuntime(t, &cfg)
	cfg.Server.Addr = "127.0.0.1:0"
	if err := os.MkdirAll(cfg.Install.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	applyLease := filepath.Join(cfg.Install.StateDir, "apply.lock")
	stale := `{"schema":1,"pid":99999999,"createdAt":"2026-08-23T00:00:00Z"}` + "\n"
	if err := os.WriteFile(applyLease, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	if _, err := os.Lstat(applyLease); !os.IsNotExist(err) {
		t.Fatalf("stale apply lease remains after startup: %v", err)
	}
}

func TestNewReconcilesDeadApplyLeaseButPreservesDeadAdminLeaseForNodeRecovery(t *testing.T) {
	cfg := config.Default()
	isolateBootstrapRuntime(t, &cfg)
	cfg.Server.Addr = "127.0.0.1:0"
	if err := os.MkdirAll(cfg.Install.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	applyLease := filepath.Join(cfg.Install.StateDir, "apply.lock")
	if err := os.WriteFile(applyLease, []byte(`{"schema":1,"pid":99999999,"createdAt":"2026-08-23T00:00:00Z"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	adminLease := filepath.Join(cfg.Install.StateDir, "admin-init.lock")
	adminBytes := []byte(`{"schema":2,"owner":"admin-init","id":"12345678-1234-1234-1234-123456789abc","pid":99999999,"pidStartToken":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","createdAt":"2026-08-24T00:00:00.000Z"}` + "\n")
	if err := os.WriteFile(adminLease, adminBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	if _, err := os.Lstat(applyLease); !os.IsNotExist(err) {
		t.Fatalf("dead apply lease remains after startup: %v", err)
	}
	if got, err := os.ReadFile(adminLease); err != nil || string(got) != string(adminBytes) {
		t.Fatalf("dead admin lease changed during Go startup: %q error=%v", got, err)
	}
}

func TestNewServesInstallerSourceFromConfiguredWorkspaceBeforeUISelection(t *testing.T) {
	workspaceRoot := t.TempDir()
	installerRoot := filepath.Join(workspaceRoot, "admin", "apps", "install", "src")
	if err := os.MkdirAll(installerRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll(installer source) error = %v", err)
	}
	for name, contents := range map[string]string{
		"index.html": "<h1>source installer</h1>",
		"app.js":     "console.log('source installer')",
		"styles.css": "body { color: green; }",
	} {
		if err := os.WriteFile(filepath.Join(installerRoot, name), []byte(contents), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", name, err)
		}
	}

	cfg := config.Default()
	cfg.Install.StateDir = filepath.Join(workspaceRoot, ".runtime", "install")
	cfg.Install.WorkspaceRoot = workspaceRoot
	cfg.Server.Addr = "127.0.0.1:0"

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	for _, item := range []struct {
		method      string
		path        string
		contentType string
		body        string
	}{
		{method: http.MethodGet, path: "/install", contentType: "text/html; charset=utf-8", body: "source installer"},
		{method: http.MethodHead, path: "/install", contentType: "text/html; charset=utf-8"},
		{method: http.MethodGet, path: "/install/app.js", contentType: "text/javascript; charset=utf-8", body: "console.log"},
		{method: http.MethodHead, path: "/install/app.js", contentType: "text/javascript; charset=utf-8"},
		{method: http.MethodGet, path: "/install/styles.css", contentType: "text/css; charset=utf-8", body: "color: green"},
		{method: http.MethodHead, path: "/install/styles.css", contentType: "text/css; charset=utf-8"},
	} {
		t.Run(item.method+" "+item.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			app.HTTPServer().Handler.ServeHTTP(response, newLocalInstallerRequest(item.method, item.path, nil))
			if response.Code != http.StatusOK {
				t.Fatalf("%s %s status = %d, want 200; body=%s", item.method, item.path, response.Code, response.Body.String())
			}
			if got := response.Header().Get("Content-Type"); got != item.contentType {
				t.Fatalf("%s %s content-type = %q, want %q", item.method, item.path, got, item.contentType)
			}
			if item.body != "" && !strings.Contains(response.Body.String(), item.body) {
				t.Fatalf("%s %s body = %q, want substring %q", item.method, item.path, response.Body.String(), item.body)
			}
		})
	}

	statusResponse := httptest.NewRecorder()
	app.HTTPServer().Handler.ServeHTTP(statusResponse, newLocalInstallerRequest(http.MethodGet, "/api/system/install/v1/status", nil))
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("installation status = %d, want 200; body=%s", statusResponse.Code, statusResponse.Body.String())
	}

	// The source-mode composition root must attach the UI preparation service.
	// An invalid enum is rejected before any pnpm process can start; an
	// unattached service would return 503 instead.
	prepareRequest := newLocalInstallerRequest(
		http.MethodPost,
		"/api/system/install/v1/ui/prepare",
		bytes.NewBufferString(`{"selectedUi":"unknown","confirmCleanup":true}`),
	)
	prepareRequest.Header.Set("Content-Type", "application/json")
	prepareResponse := httptest.NewRecorder()
	app.HTTPServer().Handler.ServeHTTP(prepareResponse, prepareRequest)
	if prepareResponse.Code != http.StatusBadRequest {
		t.Fatalf("invalid UI prepare = %d, want 400 from wired service; body=%s", prepareResponse.Code, prepareResponse.Body.String())
	}
}

func TestInstallerHTTPPortUsesConfiguredPortAndSafeEphemeralFallback(t *testing.T) {
	tests := []struct {
		address string
		want    int
	}{
		{address: ":9090", want: 9090},
		{address: "127.0.0.1:8081", want: 8081},
		{address: "[::1]:8082", want: 8082},
		{address: "127.0.0.1:0", want: 8080},
		{address: "malformed", want: 8080},
	}
	for _, test := range tests {
		if got := installerHTTPPort(test.address); got != test.want {
			t.Errorf("installerHTTPPort(%q) = %d, want %d", test.address, got, test.want)
		}
	}
}

func TestNewWiresInstallerPlanAgainstConfiguredWorkspaceRoot(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{"admin/apps/install", "admin/apps/web-naive"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(relative)), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", relative, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "admin", ".ui-profile.json"), []byte(`{"schema":1,"selectedUi":"naive","packageName":"@vben/web-naive","appDirectory":"apps/web-naive"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Install.StateDir = filepath.Join(root, "admin", "apps", "install")
	cfg.Install.WorkspaceRoot = root
	cfg.Server.Addr = "127.0.0.1:0"

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	request := newLocalInstallerRequest(http.MethodPost, "/api/system/install/v1/plan", bytes.NewBufferString(`{"mode":"standalone"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	app.HTTPServer().Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("installation plan = %d, want 200; body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			SelectedUI  string `json:"selectedUi"`
			Mode        string `json:"mode"`
			CanCleanup  bool   `json:"canCleanup"`
			CanBuild    bool   `json:"canBuild"`
			CanWriteEnv bool   `json:"canWriteEnv"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode installation plan: %v", err)
	}
	if body.Code != 0 || body.Data.SelectedUI != "naive" || body.Data.Mode != "standalone" || !body.Data.CanCleanup || !body.Data.CanBuild || !body.Data.CanWriteEnv {
		t.Fatalf("installation plan body = %#v", body)
	}
}

func TestNewWiresApplyServiceForSourceWorkspace(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{"admin/apps/install", "admin/apps/web-antd", "scripts"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(relative)), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", relative, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "admin", ".ui-profile.json"), []byte(`{"schema":1,"selectedUi":"antd","packageName":"@vben/web-antd","appDirectory":"apps/web-antd"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scripts", "build.mjs"), []byte("// fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Install.StateDir = filepath.Join(root, "admin", "apps", "install")
	cfg.Install.WorkspaceRoot = root
	cfg.Server.Addr = "127.0.0.1:0"

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	request := newLocalInstallerRequest(http.MethodPost, "/api/system/install/v1/apply", bytes.NewBufferString(`{"selectedUi":"antd"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	app.HTTPServer().Handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("apply status = %d, want 400 from configured service; body=%s", response.Code, response.Body.String())
	}
}

func TestDependencyOptionsMapEverySupportedTopology(t *testing.T) {
	db := config.DatabaseConfig{
		Enabled:         true,
		Driver:          "postgres",
		Mode:            "read_write",
		DSN:             "ignored",
		PrimaryDSN:      "primary",
		ReplicaDSNs:     []string{"replica-a", "replica-b"},
		ReadPolicy:      "round_robin",
		MaxOpenConns:    11,
		MaxIdleConns:    7,
		ConnMaxLifetime: 2 * time.Hour,
		ConnMaxIdleTime: 3 * time.Minute,
	}
	options, err := databaseOptions(db)
	if err != nil {
		t.Fatalf("databaseOptions() error = %v", err)
	}
	if string(options.Mode) != db.Mode || options.Driver != db.Driver || options.PrimaryDSN != db.PrimaryDSN || len(options.ReplicaDSNs) != 2 || string(options.ReadPolicy) != db.ReadPolicy {
		t.Fatalf("database options did not preserve topology: %+v", options)
	}

	redis := config.RedisConfig{
		Enabled:      true,
		Mode:         "sentinel",
		Addrs:        []string{"sentinel-a:26379", "sentinel-b:26379"},
		MasterName:   "mymaster",
		Username:     "user",
		Password:     "secret",
		DB:           2,
		Namespace:    "app:v1",
		DialTimeout:  2 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 4 * time.Second,
	}
	redisOptions, err := redisOptions(redis)
	if err != nil {
		t.Fatalf("redisOptions() error = %v", err)
	}
	if redisOptions.Mode != redis.Mode || redisOptions.MasterName != redis.MasterName || len(redisOptions.Addrs) != 2 || redisOptions.Password != redis.Password {
		t.Fatalf("redis options did not preserve topology: %+v", redisOptions)
	}
}

func TestNewConstructsEnabledDependenciesWithoutProbingNetwork(t *testing.T) {
	cfg := config.Default()
	isolateBootstrapRuntime(t, &cfg)
	cfg.Server.Addr = "127.0.0.1:0"
	cfg.Database.Enabled = true
	cfg.Database.Driver = "mysql"
	cfg.Database.Mode = "single"
	cfg.Database.DSN = "user:secret@tcp(127.0.0.1:1)/test?parseTime=true"
	cfg.Redis.Enabled = true
	cfg.Redis.Mode = "single"
	cfg.Redis.Addr = "127.0.0.1:1"
	cfg.Database.PingTimeout = 20 * time.Millisecond
	cfg.Redis.PingTimeout = 20 * time.Millisecond

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() should not require live dependencies: %v", err)
	}
	defer app.Close()
	if app.Database() == nil || app.Redis() == nil {
		t.Fatal("enabled dependencies were not constructed")
	}

	result := app.Readiness().Check(context.Background())
	if result.Ready {
		t.Fatal("readiness unexpectedly succeeded against test endpoints")
	}
	if result.Checks["database"] != "down" || result.Checks["redis"] != "down" {
		t.Fatalf("unexpected safe readiness checks: %+v", result.Checks)
	}
}

func TestNewWiresAuthOnlyWhenDatabaseAndRedisAreEnabled(t *testing.T) {
	cfg := config.Default()
	isolateBootstrapRuntime(t, &cfg)
	cfg.Server.Addr = "127.0.0.1:0"
	cfg.Auth.Enabled = true
	cfg.Auth.JWTSecret = "01234567890123456789012345678901"
	cfg.Database.Enabled = true
	cfg.Database.Driver = "mysql"
	cfg.Database.Mode = "single"
	cfg.Database.DSN = "root:root@tcp(127.0.0.1:1)/auth_test?parseTime=true"
	cfg.Redis.Enabled = true
	cfg.Redis.Mode = "single"
	cfg.Redis.Addr = "127.0.0.1:1"

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()
	if app.Auth() == nil {
		t.Fatal("auth service should be wired when auth and dependencies are enabled")
	}
}

func TestHTTPCompositionWiresDeviceSessionRoutes(t *testing.T) {
	cfg := config.Default()
	cfg.Auth.Enabled = true
	service := &bootstrapAuthSessionFake{}
	server := newHTTPServer(cfg, nil, service, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/auth/sessions", nil)
	req.Header.Set("Authorization", "Bearer access")
	res := httptest.NewRecorder()
	server.Handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("device session route status = %d, want 200; body=%s", res.Code, res.Body.String())
	}
}

func TestHTTPCompositionWiresCaptchaProviderAndRiskStore(t *testing.T) {
	cfg := config.Default()
	cfg.Auth.Enabled = true
	service := &bootstrapAuthSessionFake{}
	provider := &bootstrapCaptchaProvider{}
	risk := &bootstrapCaptchaRiskStore{}
	server := newHTTPServerWithPlanAndCaptcha(cfg, nil, service, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, provider, risk)

	request := httptest.NewRequest(http.MethodGet, "/api/admin/v1/auth/captcha", nil)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"kind":"image"`) {
		t.Fatalf("captcha route status = %d, body=%s", response.Code, response.Body.String())
	}
}

func TestHTTPCompositionAppliesConfiguredTenantPolicy(t *testing.T) {
	cfg := config.Default()
	cfg.Tenant.Mode = "multi"
	cfg.Auth.Enabled = true
	service := &bootstrapAuthSessionFake{}
	server := newHTTPServer(cfg, nil, service, nil, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/auth/sessions", nil)
	req.Header.Set("Authorization", "Bearer access")
	res := httptest.NewRecorder()
	server.Handler.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest || !strings.Contains(res.Body.String(), `"code":10000`) {
		t.Fatalf("missing tenant status = %d, want 400; body=%s", res.Code, res.Body.String())
	}
}

func TestHTTPCompositionAllowsLocalSingleNodeMonitorWithoutAuth(t *testing.T) {
	cfg := config.Default()
	cfg.Auth.Enabled = false
	server := newHTTPServerWithPlanAndCaptchaAndFilesAndAux(
		cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, monitorapp.NewService(monitorapp.Config{Version: "fixture"}), nil,
	)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/ops/monitor", nil)
	res := httptest.NewRecorder()
	server.Handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"scope":"process"`) {
		t.Fatalf("monitor status = %d, body=%s", res.Code, res.Body.String())
	}
}

func TestHTTPCompositionWiresIAMProtectedMonitorAndDashboardSummary(t *testing.T) {
	cfg := config.Default()
	cfg.Auth.Enabled = true
	store := iamapp.NewMemoryStore()
	if err := store.SaveUser(context.Background(), domain.User{ID: "1", Username: "admin", TenantID: "default", Active: true, RoleIDs: []string{"role-ops"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRole(context.Background(), domain.Role{ID: "role-ops", Name: "Operations", TenantID: "default", Active: true}); err != nil {
		t.Fatal(err)
	}
	for _, policy := range []domain.Policy{
		{RoleID: "role-ops", PermissionID: "ops:monitor:read", Domain: "default", Method: http.MethodGet, Path: "/api/admin/v1/ops/monitor", Effect: domain.EffectAllow},
		{RoleID: "role-ops", PermissionID: "dashboard:overview:read", Domain: "default", Method: http.MethodGet, Path: "/api/admin/v1/dashboard/summary", Effect: domain.EffectAllow},
	} {
		if err := store.SavePolicy(context.Background(), policy); err != nil {
			t.Fatal(err)
		}
	}
	server := newHTTPServerWithPlanAndCaptchaAndFilesAndAux(
		cfg, nil, &bootstrapAuthSessionFake{}, nil, iamapp.NewService(store), nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, monitorapp.NewService(monitorapp.Config{Version: "fixture"}), nil,
	)
	for _, path := range []string{"/api/admin/v1/ops/monitor", "/api/admin/v1/dashboard/summary"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Authorization", "Bearer access")
		response := httptest.NewRecorder()
		server.Handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

type bootstrapAuthSessionFake struct{}

type bootstrapCaptchaProvider struct{}

func (*bootstrapCaptchaProvider) Issue(context.Context) (appauth.CaptchaChallenge, error) {
	return appauth.CaptchaChallenge{ID: "fixture", Kind: "image", Payload: "data:image/svg+xml;base64,fixture", ExpiresIn: 120}, nil
}
func (*bootstrapCaptchaProvider) Verify(context.Context, string, string) error { return nil }

type bootstrapCaptchaRiskStore struct{}

func (*bootstrapCaptchaRiskStore) Requires(context.Context, string, int, time.Duration) (bool, error) {
	return false, nil
}
func (*bootstrapCaptchaRiskStore) RecordFailure(context.Context, string, time.Duration) error {
	return nil
}
func (*bootstrapCaptchaRiskStore) Reset(context.Context, string) error { return nil }

func (*bootstrapAuthSessionFake) Login(context.Context, string, string) (authdomain.TokenPair, error) {
	return authdomain.TokenPair{}, nil
}
func (*bootstrapAuthSessionFake) Refresh(context.Context, string) (authdomain.TokenPair, error) {
	return authdomain.TokenPair{}, nil
}
func (*bootstrapAuthSessionFake) Logout(context.Context, string) error { return nil }
func (*bootstrapAuthSessionFake) VerifyAccess(string) (authdomain.Claims, error) {
	return authdomain.Claims{Subject: "1", Type: authdomain.AccessToken}, nil
}
func (*bootstrapAuthSessionFake) ListSessions(context.Context, string) ([]authdomain.Session, error) {
	return []authdomain.Session{}, nil
}
func (*bootstrapAuthSessionFake) RevokeSession(context.Context, string, string) error  { return nil }
func (*bootstrapAuthSessionFake) LogoutWithRefreshToken(context.Context, string) error { return nil }

var _ appauth.AuthService = (*bootstrapAuthSessionFake)(nil)
var _ appauth.SessionManagementService = (*bootstrapAuthSessionFake)(nil)

func TestNewRejectsAuthWithoutDurableDependencies(t *testing.T) {
	cfg := config.Default()
	isolateBootstrapRuntime(t, &cfg)
	cfg.Auth.Enabled = true
	cfg.Auth.JWTSecret = "01234567890123456789012345678901"
	if _, err := New(cfg); err == nil {
		t.Fatal("New() error = nil, want auth dependency requirement")
	}
}

func TestCloseResourcesIsReverseOrderAndContinuesAfterErrors(t *testing.T) {
	var order []string
	resources := []closer{
		closeFunc(func() error { order = append(order, "database"); return errors.New("db close") }),
		closeFunc(func() error { order = append(order, "redis"); return nil }),
	}
	if err := closeResources(resources); err == nil {
		t.Fatal("closeResources() error = nil, want aggregated error")
	}
	if got, want := order, []string{"redis", "database"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("close order = %v, want %v", got, want)
	}
}

func TestReadinessTimeoutUsesEnabledDependencyBudgets(t *testing.T) {
	cfg := config.Default()
	if got, want := readinessTimeout(cfg), 2*time.Second; got != want {
		t.Fatalf("readinessTimeout(disabled) = %s, want %s", got, want)
	}
	cfg.Database.Enabled = true
	cfg.Database.PingTimeout = 20 * time.Millisecond
	cfg.Redis.Enabled = true
	cfg.Redis.PingTimeout = 40 * time.Millisecond
	if got, want := readinessTimeout(cfg), 40*time.Millisecond; got != want {
		t.Fatalf("readinessTimeout(enabled) = %s, want %s", got, want)
	}
}

func TestShutdownClosesHTTPServerAndDependencies(t *testing.T) {
	cfg := config.Default()
	isolateBootstrapRuntime(t, &cfg)
	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown() error = %v", err)
	}
}

func TestHTTPServerUsesConfiguredHandler(t *testing.T) {
	cfg := config.Default()
	isolateBootstrapRuntime(t, &cfg)
	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()
	if app.HTTPServer().Handler == nil {
		t.Fatal("HTTP server handler is nil")
	}
	if app.HTTPServer().ReadHeaderTimeout != cfg.Server.ReadTimeout {
		t.Fatalf("read header timeout = %s, want %s", app.HTTPServer().ReadHeaderTimeout, cfg.Server.ReadTimeout)
	}
	_ = http.MethodGet
}

func TestRunClosesDependenciesWhenListenFails(t *testing.T) {
	cfg := config.Default()
	closed := false
	app := &App{
		config: cfg,
		http:   &http.Server{Addr: "127.0.0.1:-1"},
		closers: []io.Closer{closeFunc(func() error {
			closed = true
			return nil
		})},
	}

	if err := app.Run(context.Background()); err == nil {
		t.Fatal("Run() error = nil, want listen error")
	}
	if !closed {
		t.Fatal("Run() did not close dependencies after listen failure")
	}
}

func TestReloadPersistedObservabilityUsesDefaultTenantSettings(t *testing.T) {
	repository := settingsapp.NewMemoryRepository()
	for key, value := range map[string]string{
		"observability.metrics.enabled":  `true`,
		"observability.metrics.endpoint": `"http://127.0.0.1:8080/metrics"`,
	} {
		if _, err := repository.Append(context.Background(), settingsapp.StoredSetting{Key: key, RawValue: []byte(value)}); err != nil {
			t.Fatalf("Append(%q) error = %v", key, err)
		}
	}
	cfg := config.Default()
	manager, err := observabilityplatform.NewManager(domainobs.DefaultConfig())
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	app := &App{config: cfg, observability: manager, settingsRepository: repository}

	if err := app.reloadPersistedObservability(context.Background()); err != nil {
		t.Fatalf("reloadPersistedObservability() error = %v", err)
	}
	if got := manager.CollectorCount(); got != 1 {
		t.Fatalf("CollectorCount() = %d, want 1", got)
	}
	if !app.Config().Observability.MetricsEnabled || app.Config().Observability.MetricsEndpoint != "http://127.0.0.1:8080/metrics" {
		t.Fatalf("effective observability config = %#v", app.Config().Observability.SafeSummary())
	}
}

func TestReloadPersistedSettingsHydratesImmutableSnapshot(t *testing.T) {
	repository := settingsapp.NewMemoryRepository()
	if _, err := repository.Append(context.Background(), settingsapp.StoredSetting{Key: "basic.site_name", RawValue: []byte(`"persisted"`), Source: settingsapp.SourceDatabase}); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	store := settingsapp.NewRuntimeSnapshotStore()
	service := settingsapp.NewService(repository, nil, nil, nil)
	service.SetRuntimeSnapshotStore(store)
	cfg := config.Default()
	app := &App{config: cfg, settings: service, settingsRepository: repository}
	if err := app.ReloadPersistedSettings(context.Background()); err != nil {
		t.Fatalf("ReloadPersistedSettings() error = %v", err)
	}
	// Bootstrap hydrates the default tenant partition; request-scoped reads must
	// use the same partition rather than the legacy process-wide slot.
	scopeKey := cfg.Tenant.DefaultID + "\x00"
	value, ok := store.ValueFor(scopeKey, "basic.site_name")
	if !ok || string(value) != `"persisted"` {
		t.Fatalf("snapshot value = %s, present=%v", value, ok)
	}
	if source := store.SourceFor(scopeKey, "basic.site_name"); source != settingsapp.SourceDatabase {
		t.Fatalf("snapshot source = %q, want database", source)
	}
}

type closeFunc func() error

func (f closeFunc) Close() error { return f() }
