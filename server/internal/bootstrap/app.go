package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	appauth "example.com/gin-vben-admin/server/internal/application/auth"
	iamapp "example.com/gin-vben-admin/server/internal/application/iam"
	installer "example.com/gin-vben-admin/server/internal/application/installer"
	"example.com/gin-vben-admin/server/internal/config"
	"example.com/gin-vben-admin/server/internal/platform/authplatform"
	rediscache "example.com/gin-vben-admin/server/internal/platform/cache/redis"
	platformhealth "example.com/gin-vben-admin/server/internal/platform/health"
	"example.com/gin-vben-admin/server/internal/platform/iamplatform"
	"example.com/gin-vben-admin/server/internal/platform/installplatform"
	"example.com/gin-vben-admin/server/internal/platform/persistence/gormdb"
)

// App is the composition root for the API process. Optional infrastructure is
// constructed only when enabled in configuration; constructors deliberately do
// not perform network probes. Readiness owns the live connectivity checks.
type App struct {
	config    config.Config
	http      *http.Server
	database  *gormdb.Store
	redis     *rediscache.Client
	auth      appauth.AuthService
	iam       *iamapp.Service
	install   *installer.StatusService
	readiness *platformhealth.Checker
	closers   []io.Closer

	shutdownOnce sync.Once
	shutdownErr  error
}

// New builds the API application from a validated configuration. It never
// applies database migrations and does not require database or Redis to be
// reachable at startup.
func New(cfg config.Config) (*App, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate runtime configuration: %w", err)
	}

	app := &App{config: cfg}
	app.install = installer.NewStatusService(installplatform.NewFileMarkerStore(cfg.Install.MarkerPath()))
	cleanupOnError := func(cause error) (*App, error) {
		_ = closeResources(app.closers)
		return nil, cause
	}

	dependencies := make([]platformhealth.Dependency, 0, 2)
	if cfg.Database.Enabled {
		options, err := databaseOptions(cfg.Database)
		if err != nil {
			return cleanupOnError(fmt.Errorf("configure database dependency: %w", err))
		}
		store, err := gormdb.Open(options)
		if err != nil {
			// Do not include a DSN or driver error payload in the public error.
			return cleanupOnError(errors.New("initialize database dependency"))
		}
		app.database = store
		app.closers = append(app.closers, store)
		dependencies = append(dependencies, timedDependency{dependency: store, timeout: cfg.Database.PingTimeout})
	}

	if cfg.Redis.Enabled {
		options, err := redisOptions(cfg.Redis)
		if err != nil {
			return cleanupOnError(fmt.Errorf("configure redis dependency: %w", err))
		}
		client, err := rediscache.New(options)
		if err != nil {
			// Redis constructors validate topology locally; no credential-bearing
			// error is returned to the process boundary.
			return cleanupOnError(errors.New("initialize redis dependency"))
		}
		app.redis = client
		app.closers = append(app.closers, client)
		dependencies = append(dependencies, timedDependency{dependency: client, timeout: cfg.Redis.PingTimeout})
	}

	if cfg.Auth.Enabled {
		if app.database == nil || app.redis == nil {
			return cleanupOnError(errors.New("auth requires enabled database and redis dependencies"))
		}
		users := authplatform.NewGORMUserRepository(app.database)
		hasher := authplatform.BcryptHasher{Cost: cfg.Auth.BcryptCost}
		tokens := authplatform.NewJWTServiceWithOptions(
			[]byte(cfg.Auth.JWTSecret),
			cfg.Auth.AccessTTL,
			cfg.Auth.RefreshTTL,
			cfg.Auth.Issuer,
			cfg.Auth.Audience,
		)
		sessions := authplatform.NewRedisSessionStore(app.redis)
		attempts := authplatform.NewRedisLoginAttemptStore(app.redis, cfg.Auth.LockoutThreshold, cfg.Auth.LockoutDuration)
		authService := appauth.NewService(users, hasher, tokens, sessions, attempts)
		authService.SetAccountProvisioner(users)
		durableSessions := authplatform.NewGORMSessionStore(app.database)
		authService.SetSessionJournal(durableSessions)
		authService.SetSessionQuery(durableSessions)
		authService.SetAuditSink(authplatform.NewGORMAuditSink(app.database))
		app.auth = authService
		persistentIAM := iamplatform.NewGORMStore(app.database)
		app.iam = iamapp.NewServiceWithRepositories(persistentIAM, persistentIAM, persistentIAM, persistentIAM, persistentIAM, persistentIAM)
		app.iam.SetPermissionCache(iamplatform.NewRedisPermissionCache(app.redis), 30*time.Second)
	}

	app.readiness = platformhealth.NewChecker(readinessTimeout(cfg), dependencies...)
	var limiter appauth.RateLimiter
	if cfg.Auth.Enabled {
		limiter = authplatform.NewRedisRateLimiter(app.redis)
	}
	var recovery appauth.AccountRecoveryService
	if candidate, ok := app.auth.(appauth.AccountRecoveryService); ok {
		recovery = candidate
	}
	var installPlan installer.PlanProvider
	if workspaceRoot, err := installWorkspaceRoot(cfg.Install.StateDir); err == nil {
		if inspector, inspectorErr := installplatform.NewFileSystemInspector(workspaceRoot); inspectorErr == nil {
			installPlan = installer.NewPlanService(inspector)
		}
	}
	app.http = newHTTPServerWithPlan(cfg, app.readiness, app.auth, limiter, app.iam, recovery, app.install, installPlan)
	return app, nil
}

func installWorkspaceRoot(stateDir string) (string, error) {
	abs, err := filepath.Abs(stateDir)
	if err != nil {
		return "", err
	}
	return filepath.Dir(abs), nil
}

// Config returns a copy of the validated runtime configuration.
func (a *App) Config() config.Config {
	if a == nil {
		return config.Config{}
	}
	return a.config
}

// HTTPServer returns the configured server. Call Run to own its
// lifecycle; callers must not call ListenAndServe directly on the returned
// value while using App.Run.
func (a *App) HTTPServer() *http.Server {
	if a == nil || a.http == nil {
		return nil
	}
	return a.http
}

// Database returns the optional GORM store, or nil when database.enabled is
// false.
func (a *App) Database() *gormdb.Store {
	if a == nil {
		return nil
	}
	return a.database
}

// Redis returns the optional Redis client, or nil when redis.enabled is false.
func (a *App) Redis() *rediscache.Client {
	if a == nil {
		return nil
	}
	return a.redis
}

// Auth returns the configured authentication service, or nil when authentication
// is disabled.
func (a *App) Auth() appauth.AuthService {
	if a == nil {
		return nil
	}
	return a.auth
}

// IAM returns the database-backed management authorization service, or nil
// when authentication and its persistence dependencies are disabled.
func (a *App) IAM() *iamapp.Service {
	if a == nil {
		return nil
	}
	return a.iam
}

// Installation returns the credential-free installation status service.
func (a *App) Installation() *installer.StatusService {
	if a == nil {
		return nil
	}
	return a.install
}

// Readiness returns the checker injected into the HTTP health routes.
func (a *App) Readiness() *platformhealth.Checker {
	if a == nil {
		return nil
	}
	return a.readiness
}

// Run serves HTTP until ctx is canceled or the listener fails. Cancellation
// performs a graceful HTTP shutdown followed by reverse-order dependency close.
func (a *App) Run(ctx context.Context) error {
	if a == nil || a.HTTPServer() == nil {
		return errors.New("api application is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	listener, err := net.Listen("tcp", a.HTTPServer().Addr)
	if err != nil {
		return errors.Join(fmt.Errorf("listen HTTP server: %w", err), a.Close())
	}
	errCh := make(chan error, 1)
	go func() {
		err := a.HTTPServer().Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), a.config.Server.ShutdownTimeout)
		defer cancel()
		return a.Shutdown(shutdownCtx)
	case err := <-errCh:
		return errors.Join(err, a.Close())
	}
}

// Shutdown gracefully stops HTTP and then closes dependencies. It is safe to
// call repeatedly; the first result is returned to all callers.
func (a *App) Shutdown(ctx context.Context) error {
	if a == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	a.shutdownOnce.Do(func() {
		var errs []error
		if a.HTTPServer() != nil {
			if err := a.HTTPServer().Shutdown(ctx); err != nil {
				errs = append(errs, fmt.Errorf("shutdown HTTP server: %w", err))
			}
		}
		if err := closeResources(a.closers); err != nil {
			errs = append(errs, err)
		}
		a.shutdownErr = errors.Join(errs...)
	})
	return a.shutdownErr
}

// Close force-closes the HTTP listener and then closes dependencies. Normal
// process termination should prefer Shutdown so in-flight requests can finish.
func (a *App) Close() error {
	if a == nil {
		return nil
	}
	a.shutdownOnce.Do(func() {
		var errs []error
		if a.HTTPServer() != nil {
			if err := a.HTTPServer().Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errs = append(errs, fmt.Errorf("close HTTP server: %w", err))
			}
		}
		if err := closeResources(a.closers); err != nil {
			errs = append(errs, err)
		}
		a.shutdownErr = errors.Join(errs...)
	})
	return a.shutdownErr
}

type closer = io.Closer

type timedDependency struct {
	dependency platformhealth.Dependency
	timeout    time.Duration
}

func (d timedDependency) Name() string { return d.dependency.Name() }

func (d timedDependency) Ping(ctx context.Context) error {
	if d.timeout <= 0 {
		return d.dependency.Ping(ctx)
	}
	checkCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	return d.dependency.Ping(checkCtx)
}

func closeResources(resources []closer) error {
	var errs []error
	for index := len(resources) - 1; index >= 0; index-- {
		if resources[index] == nil {
			continue
		}
		if err := resources[index].Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func databaseOptions(cfg config.DatabaseConfig) (gormdb.Options, error) {
	options := gormdb.Options{
		Driver:          cfg.Driver,
		Mode:            gormdb.Mode(cfg.Mode),
		DSN:             cfg.DSN,
		PrimaryDSN:      cfg.PrimaryDSN,
		ReplicaDSNs:     append([]string(nil), cfg.ReplicaDSNs...),
		ReadPolicy:      gormdb.ReadPolicy(cfg.ReadPolicy),
		MaxOpenConns:    cfg.MaxOpenConns,
		MaxIdleConns:    cfg.MaxIdleConns,
		ConnMaxLifetime: cfg.ConnMaxLifetime,
		ConnMaxIdleTime: cfg.ConnMaxIdleTime,
	}
	return options, nil
}

func redisOptions(cfg config.RedisConfig) (rediscache.Config, error) {
	options := rediscache.Config{
		Mode:         cfg.Mode,
		Addr:         cfg.Addr,
		Addrs:        append([]string(nil), cfg.Addrs...),
		MasterName:   cfg.MasterName,
		Username:     cfg.Username,
		Password:     cfg.Password,
		DB:           cfg.DB,
		Namespace:    cfg.Namespace,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}
	if options.Mode == "" {
		options.Mode = rediscache.ModeSingle
	}
	return options, nil
}

func readinessTimeout(cfg config.Config) time.Duration {
	const fallback = 2 * time.Second
	var timeout time.Duration
	if cfg.Database.Enabled && cfg.Database.PingTimeout > timeout {
		timeout = cfg.Database.PingTimeout
	}
	if cfg.Redis.Enabled && cfg.Redis.PingTimeout > timeout {
		timeout = cfg.Redis.PingTimeout
	}
	if timeout <= 0 {
		return fallback
	}
	return timeout
}
