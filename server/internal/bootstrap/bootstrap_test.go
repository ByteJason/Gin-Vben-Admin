package bootstrap

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	appauth "example.com/gin-vben-admin/server/internal/application/auth"
	"example.com/gin-vben-admin/server/internal/config"
	"example.com/gin-vben-admin/server/internal/domain/authdomain"
)

func TestNewBuildsConfiguredHTTPServerAndKeepsDependenciesOptional(t *testing.T) {
	cfg := config.Default()
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

type bootstrapAuthSessionFake struct{}

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

type closeFunc func() error

func (f closeFunc) Close() error { return f() }
