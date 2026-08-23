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
	monitorapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/monitor"
	settingsapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/settings"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/config"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	domainobs "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/observability"
	observabilityplatform "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/observability"
)

func TestNewBuildsConfiguredHTTPServerAndKeepsDependenciesOptional(t *testing.T) {
	cfg := config.Default()
	cfg.Install.StateDir = t.TempDir()
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
	if app.Readiness() == nil {
		t.Fatal("readiness checker must always be constructed")
	}

	request := httptest.NewRequest(http.MethodGet, "/api/system/install/v1/status", nil)
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
	if body.Code != 0 || body.Data.Installed || body.Data.State != "uninstalled" {
		t.Fatalf("installation status body = %#v", body)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/system/install/v1/capabilities", nil)
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

	request = httptest.NewRequest(http.MethodGet, "/install", nil)
	response = httptest.NewRecorder()
	app.HTTPServer().Handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("ordinary build installation page = %d, want 404", response.Code)
	}
}

func TestNewWiresInstallerPlanAgainstStateDirectoryParent(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{"install", "admin/apps/web-antd", "admin/apps/web-ele", "admin/apps/web-naive"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(relative)), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", relative, err)
		}
	}
	cfg := config.Default()
	cfg.Install.StateDir = filepath.Join(root, "install")
	cfg.Server.Addr = "127.0.0.1:0"

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	request := httptest.NewRequest(http.MethodPost, "/api/system/install/v1/plan", bytes.NewBufferString(`{"selectedUi":"naive","mode":"standalone"}`))
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
	for _, relative := range []string{"install", "admin/apps/web-antd", "admin/apps/web-ele", "admin/apps/web-naive", "scripts"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(relative)), 0o755); err != nil {
			t.Fatalf("MkdirAll(%q) error = %v", relative, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "scripts", "build.mjs"), []byte("// fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Install.StateDir = filepath.Join(root, "install")
	cfg.Server.Addr = "127.0.0.1:0"

	app, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer app.Close()

	request := httptest.NewRequest(http.MethodPost, "/api/system/install/v1/apply", bytes.NewBufferString(`{"selectedUi":"antd"}`))
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

type closeFunc func() error

func (f closeFunc) Close() error { return f() }
