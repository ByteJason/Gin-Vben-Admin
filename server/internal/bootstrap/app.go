package bootstrap

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	auditapp "example.com/gin-vben-admin/server/internal/application/audit"
	appauth "example.com/gin-vben-admin/server/internal/application/auth"
	fileapp "example.com/gin-vben-admin/server/internal/application/file"
	iamapp "example.com/gin-vben-admin/server/internal/application/iam"
	installer "example.com/gin-vben-admin/server/internal/application/installer"
	mailapp "example.com/gin-vben-admin/server/internal/application/mail"
	monitorapp "example.com/gin-vben-admin/server/internal/application/monitor"
	appnotification "example.com/gin-vben-admin/server/internal/application/notification"
	settingsapp "example.com/gin-vben-admin/server/internal/application/settings"
	"example.com/gin-vben-admin/server/internal/config"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
	"example.com/gin-vben-admin/server/internal/platform/auditplatform"
	"example.com/gin-vben-admin/server/internal/platform/authplatform"
	rediscache "example.com/gin-vben-admin/server/internal/platform/cache/redis"
	platformhealth "example.com/gin-vben-admin/server/internal/platform/health"
	"example.com/gin-vben-admin/server/internal/platform/iamplatform"
	"example.com/gin-vben-admin/server/internal/platform/installplatform"
	mailplatform "example.com/gin-vben-admin/server/internal/platform/mail"
	notificationplatform "example.com/gin-vben-admin/server/internal/platform/notification"
	observabilityplatform "example.com/gin-vben-admin/server/internal/platform/observability"
	"example.com/gin-vben-admin/server/internal/platform/persistence/gormdb"
	"example.com/gin-vben-admin/server/internal/platform/settingsplatform"
)

// App is the composition root for the API process. Optional infrastructure is
// constructed only when enabled in configuration; constructors deliberately do
// not perform network probes. Readiness owns the live connectivity checks.
type App struct {
	config             config.Config
	http               *http.Server
	database           *gormdb.Store
	redis              *rediscache.Client
	auth               appauth.AuthService
	iam                *iamapp.Service
	settings           *settingsapp.Service
	audit              *auditapp.Service
	files              *fileapp.Service
	mail               *mailapp.Service
	monitor            *monitorapp.Service
	observability      *observabilityplatform.Manager
	settingsRepository settingsapp.Repository
	install            *installer.StatusService
	apply              *installer.ApplyService
	applyJobs          *installer.ApplyJobService
	readiness          *platformhealth.Checker
	closers            []io.Closer

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
	observability, err := observabilityplatform.NewManager(cfg.Observability)
	if err != nil {
		return nil, fmt.Errorf("configure observability runtime: %w", err)
	}
	app.observability = observability
	app.closers = append(app.closers, observability)
	app.install = installer.NewStatusService(installplatform.NewFileMarkerStore(cfg.Install.MarkerPath()))
	cleanupOnError := func(cause error) (*App, error) {
		_ = closeResources(app.closers)
		return nil, cause
	}
	if cfg.File.Enabled {
		store, fileErr := fileapp.NewLocalStore(cfg.File.Root, cfg.File.BaseURL)
		if fileErr != nil {
			return cleanupOnError(fmt.Errorf("configure local file provider: %w", fileErr))
		}
		maxBytes := cfg.File.MaxBytes
		if maxBytes <= 0 {
			maxBytes = 100 << 20
		}
		app.files = fileapp.NewService(store, fileapp.Config{MaxBytes: maxBytes, AllowedMIMEs: cfg.File.AllowedMIMEs})
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
		if cfg.Mail.Enabled {
			smtpMailer, mailErr := notificationplatform.NewSMTPMailer(appnotification.SMTPConfig{
				Host:     cfg.Mail.Host,
				Port:     cfg.Mail.Port,
				Username: cfg.Mail.Username,
				Password: cfg.Mail.Password,
				From:     cfg.Mail.From,
				StartTLS: cfg.Mail.StartTLS,
			})
			if mailErr != nil {
				return cleanupOnError(errors.New("configure SMTP notification"))
			}
			authService.SetPasswordResetProvider(appauth.NewSMTPPasswordResetProvider(15*time.Minute, appnotification.NewService(smtpMailer)))
		}
		app.auth = authService
		persistentIAM := iamplatform.NewGORMStore(app.database)
		app.iam = iamapp.NewServiceWithRepositories(persistentIAM, persistentIAM, persistentIAM, persistentIAM, persistentIAM, persistentIAM)
		app.iam.SetPasswordHasher(hasher)
		app.iam.SetPermissionCache(iamplatform.NewRedisPermissionCache(app.redis), 30*time.Second)
	}
	if app.database != nil {
		settingsRepository := settingsplatform.NewGORMRepository(app.database)
		app.settingsRepository = settingsRepository
		app.settings = settingsapp.NewService(
			settingsRepository,
			settingsplatform.NewGORMAuditSink(app.database),
			nil,
			nil,
		)
		if runtimeKey := strings.TrimSpace(cfg.Auth.JWTSecret); runtimeKey != "" {
			encryptor, encryptErr := settingsapp.NewEnvelopeEncryptor([]byte(runtimeKey))
			if encryptErr != nil {
				return cleanupOnError(errors.New("configure settings encryption"))
			}
			app.settings.SetEncryptor(encryptor)
		}
		app.audit = auditapp.NewService(auditplatform.NewGORMRepository(app.database))
	}

	// The 1.0 mail service is available in both database-backed and local
	// single-process modes. Account/message bodies are envelope-encrypted and
	// never returned by the transport layer. A configured JWT secret is reused as
	// the runtime key; when auth is disabled, an ephemeral process key keeps the
	// local fixture redacted without persisting a secret.
	mailKey := []byte(strings.TrimSpace(cfg.Auth.JWTSecret))
	if len(mailKey) == 0 {
		var ephemeral [32]byte
		if _, randErr := cryptorand.Read(ephemeral[:]); randErr == nil {
			mailKey = ephemeral[:]
		}
	}
	if len(mailKey) > 0 {
		mailCipher, cipherErr := settingsapp.NewEnvelopeEncryptor(mailKey)
		if cipherErr != nil {
			return cleanupOnError(errors.New("configure mail encryption"))
		}
		var accountRepository mailapp.AccountRepository = mailapp.NewMemoryAccountRepository()
		var messageRepository mailapp.MessageRepository = mailapp.NewMemoryMessageRepository()
		var attemptRepository mailapp.AttemptRepository = mailapp.NewMemoryAttemptRepository()
		if app.database != nil {
			accountRepository = mailplatform.NewGORMAccountRepository(app.database)
			messageRepository = mailplatform.NewGORMMessageRepository(app.database)
			attemptRepository = mailplatform.NewGORMMessageRepository(app.database)
		}
		selection := appnotification.SMTPSelection(strings.TrimSpace(cfg.Mail.Selection))
		if selection == "" {
			selection = appnotification.SMTPSelectionWeightedRandom
		}
		mailService := mailapp.NewService(accountRepository, messageRepository, notificationplatform.NewSMTPAccountProvider(), mailapp.Config{Cipher: mailCipher, Selection: selection, MaxAttempts: 3, RetryDelays: []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}, Cooldown: 30 * time.Second})
		mailService.SetAttemptRepository(attemptRepository)
		app.mail = mailService
	}
	app.monitor = monitorapp.NewService(monitorapp.Config{Version: "1.0.0-dev", Scope: "process", Start: time.Now(), Database: app.database, Redis: app.redis})

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
	var applyService *installer.ApplyService
	if workspaceRoot, err := installWorkspaceRoot(cfg.Install.StateDir); err == nil {
		if inspector, inspectorErr := installplatform.NewFileSystemInspector(workspaceRoot); inspectorErr == nil {
			installPlan = installer.NewPlanService(inspector)
			if _, scriptErr := os.Stat(filepath.Join(workspaceRoot, "scripts", "build.mjs")); scriptErr == nil {
				envStore := installplatform.NewAtomicEnvStore(
					filepath.Join(workspaceRoot, ".env"),
					filepath.Join(cfg.Install.StateDir, ".env-backup"),
				)
				applyService = installer.NewApplyService(
					installplatform.NewFileMarkerStore(cfg.Install.MarkerPath()),
					installPlan,
					installplatform.NewSystemDependencyProbe(),
					installplatform.NewSystemSchemaInstaller(),
					installplatform.NewAssetInstaller(
						workspaceRoot,
						filepath.Join(cfg.Install.StateDir, ".install-backup"),
						installplatform.NewSystemAssetBuilder(workspaceRoot),
						nil,
					),
					installplatform.NewSystemIdentityInstaller(),
					installplatform.NewEnvironmentInstaller(envStore, cfg.Install.StateDir, nil),
					nil,
				)
			}
		}
	}
	app.apply = applyService
	if applyService != nil {
		app.applyJobs = installer.NewApplyJobService(applyService)
		app.closers = append(app.closers, app.applyJobs)
	}
	var captchaProvider appauth.CaptchaProvider
	var captchaRisk appauth.CaptchaRiskStore
	if cfg.Auth.Enabled && app.redis != nil {
		captchaProvider = authplatform.NewRedisCaptchaProvider(app.redis, cfg.Auth.CaptchaKeyPrefix, cfg.Auth.CaptchaChallengeTTL)
		captchaRisk = authplatform.NewRedisCaptchaRiskStore(app.redis, cfg.Auth.CaptchaKeyPrefix)
	}
	app.http = newHTTPServerWithPlanAndCaptchaAndFilesAndAux(cfg, app.readiness, app.auth, limiter, app.iam, recovery, app.install, installPlan, installplatform.NewSystemDependencyProbe(), applyService, app.applyJobs, app.settings, app.audit, captchaProvider, captchaRisk, app.files, app.mail, app.monitor, app.observability)
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

// Settings returns the optional versioned settings service.
func (a *App) Settings() *settingsapp.Service {
	if a == nil {
		return nil
	}
	return a.settings
}

// Audit returns the optional read-side audit query service.
func (a *App) Audit() *auditapp.Service {
	if a == nil {
		return nil
	}
	return a.audit
}

// Files returns the local file-center application service when enabled.
func (a *App) Files() *fileapp.Service {
	if a == nil {
		return nil
	}
	return a.files
}

// Mail returns the tenant-scoped SMTP account and delivery service.
func (a *App) Mail() *mailapp.Service {
	if a == nil {
		return nil
	}
	return a.mail
}

// Monitor returns the read-only platform monitoring service.
func (a *App) Monitor() *monitorapp.Service {
	if a == nil {
		return nil
	}
	return a.monitor
}

// Observability returns the runtime metrics/tracing collector. It is always
// present after New; disabled configurations expose zero collectors.
func (a *App) Observability() *observabilityplatform.Manager {
	if a == nil {
		return nil
	}
	return a.observability
}

// ReloadPersistedObservability applies the database-backed observability
// settings for the configured default tenant. Run invokes the same seam after
// dependency reachability is confirmed; the exported method is also useful to
// controlled restart/operations coordinators and integration tests.
func (a *App) ReloadPersistedObservability(ctx context.Context) error {
	return a.reloadPersistedObservability(ctx)
}

// Installation returns the credential-free installation status service.
func (a *App) Installation() *installer.StatusService {
	if a == nil {
		return nil
	}
	return a.install
}

// InstallationApply returns the configured first-install transaction service.
func (a *App) InstallationApply() *installer.ApplyService {
	if a == nil {
		return nil
	}
	return a.apply
}

// InstallationJobs returns the asynchronous first-install coordinator.
func (a *App) InstallationJobs() *installer.ApplyJobService {
	if a == nil {
		return nil
	}
	return a.applyJobs
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
	if err := a.preparePersistedObservability(ctx); err != nil {
		return errors.Join(fmt.Errorf("load persisted observability configuration: %w", err), a.Close())
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

// preparePersistedObservability reloads settings only after the database is
// reachable. An unavailable dependency must still allow the health server to
// start and report readiness=down; a reachable database with malformed
// persisted settings fails closed before accepting HTTP traffic.
func (a *App) preparePersistedObservability(ctx context.Context) error {
	if a == nil || a.settingsRepository == nil || a.observability == nil {
		return nil
	}
	if a.database != nil {
		probeCtx := ctx
		cancel := func() {}
		if timeout := a.config.Database.PingTimeout; timeout > 0 {
			probeCtx, cancel = context.WithTimeout(ctx, timeout)
		}
		err := a.database.Ping(probeCtx)
		cancel()
		if err != nil {
			return nil
		}
	}
	return a.reloadPersistedObservability(ctx)
}

func (a *App) reloadPersistedObservability(ctx context.Context) error {
	if a == nil || a.settingsRepository == nil || a.observability == nil {
		return nil
	}
	scope, err := tenant.NewContext(a.config.Tenant.DefaultID, "", true)
	if err != nil {
		return fmt.Errorf("configure observability tenant scope: %w", err)
	}
	settingsContext := tenant.WithContext(ctx, scope)
	resolved, err := settingsapp.ResolveObservabilityConfig(
		settingsContext,
		a.settingsRepository,
		a.config.Observability,
		a.config.DynamicObservabilityAllowed,
	)
	if err != nil {
		return err
	}
	if err := a.observability.Reload(resolved); err != nil {
		return fmt.Errorf("reload observability runtime: %w", err)
	}
	a.config.Observability = resolved
	return nil
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
		AddressMap:   cloneStringMap(cfg.AddressMap),
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

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
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
