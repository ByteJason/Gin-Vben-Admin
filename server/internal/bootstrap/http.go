package bootstrap

import (
	"io/fs"
	"net/http"

	auditapp "example.com/gin-vben-admin/server/internal/application/audit"
	appauth "example.com/gin-vben-admin/server/internal/application/auth"
	fileapp "example.com/gin-vben-admin/server/internal/application/file"
	iamapp "example.com/gin-vben-admin/server/internal/application/iam"
	installer "example.com/gin-vben-admin/server/internal/application/installer"
	mailapp "example.com/gin-vben-admin/server/internal/application/mail"
	monitorapp "example.com/gin-vben-admin/server/internal/application/monitor"
	settingsapp "example.com/gin-vben-admin/server/internal/application/settings"
	"example.com/gin-vben-admin/server/internal/config"
	"example.com/gin-vben-admin/server/internal/platform/installplatform"
	"example.com/gin-vben-admin/server/internal/platform/webassets"
	adminhttp "example.com/gin-vben-admin/server/internal/transport/http/admin"
	audithttp "example.com/gin-vben-admin/server/internal/transport/http/audit"
	authhttp "example.com/gin-vben-admin/server/internal/transport/http/auth"
	filehttp "example.com/gin-vben-admin/server/internal/transport/http/file"
	"example.com/gin-vben-admin/server/internal/transport/http/health"
	iamhttp "example.com/gin-vben-admin/server/internal/transport/http/iam"
	installhttp "example.com/gin-vben-admin/server/internal/transport/http/install"
	mailhttp "example.com/gin-vben-admin/server/internal/transport/http/mail"
	httpmiddleware "example.com/gin-vben-admin/server/internal/transport/http/middleware"
	monitorhttp "example.com/gin-vben-admin/server/internal/transport/http/monitor"
	"example.com/gin-vben-admin/server/internal/transport/http/router"
	settingshttp "example.com/gin-vben-admin/server/internal/transport/http/settings"
	"github.com/gin-gonic/gin"
)

func NewHTTPServer(addr string) *http.Server {
	cfg := config.Default()
	cfg.Server.Addr = addr
	return newHTTPServer(cfg, nil, nil, nil, nil, nil)
}

func newHTTPServer(cfg config.Config, readiness health.ReadinessChecker, authService appauth.AuthService, limiter appauth.RateLimiter, iamService *iamapp.Service, recovery appauth.AccountRecoveryService, installStatuses ...*installer.StatusService) *http.Server {
	var installStatus *installer.StatusService
	if len(installStatuses) > 0 {
		installStatus = installStatuses[0]
	}
	return newHTTPServerWithPlan(cfg, readiness, authService, limiter, iamService, recovery, installStatus, nil, nil, nil, nil, nil, nil)
}

func newHTTPServerWithPlan(cfg config.Config, readiness health.ReadinessChecker, authService appauth.AuthService, limiter appauth.RateLimiter, iamService *iamapp.Service, recovery appauth.AccountRecoveryService, installStatus *installer.StatusService, installPlan installer.PlanProvider, dependencyChecks installhttp.DependencyCheckProvider, applyService *installer.ApplyService, jobService *installer.ApplyJobService, settingsService *settingsapp.Service, auditService *auditapp.Service, observations ...httpmiddleware.ObservabilityRuntime) *http.Server {
	return newHTTPServerWithPlanAndCaptcha(cfg, readiness, authService, limiter, iamService, recovery, installStatus, installPlan, dependencyChecks, applyService, jobService, settingsService, auditService, nil, nil, observations...)
}

func newHTTPServerWithPlanAndCaptcha(cfg config.Config, readiness health.ReadinessChecker, authService appauth.AuthService, limiter appauth.RateLimiter, iamService *iamapp.Service, recovery appauth.AccountRecoveryService, installStatus *installer.StatusService, installPlan installer.PlanProvider, dependencyChecks installhttp.DependencyCheckProvider, applyService *installer.ApplyService, jobService *installer.ApplyJobService, settingsService *settingsapp.Service, auditService *auditapp.Service, captchaProvider appauth.CaptchaProvider, captchaRisk appauth.CaptchaRiskStore, observations ...httpmiddleware.ObservabilityRuntime) *http.Server {
	return newHTTPServerWithPlanAndCaptchaAndFiles(cfg, readiness, authService, limiter, iamService, recovery, installStatus, installPlan, dependencyChecks, applyService, jobService, settingsService, auditService, captchaProvider, captchaRisk, localFileService(cfg), observations...)
}

func newHTTPServerWithPlanAndCaptchaAndFiles(cfg config.Config, readiness health.ReadinessChecker, authService appauth.AuthService, limiter appauth.RateLimiter, iamService *iamapp.Service, recovery appauth.AccountRecoveryService, installStatus *installer.StatusService, installPlan installer.PlanProvider, dependencyChecks installhttp.DependencyCheckProvider, applyService *installer.ApplyService, jobService *installer.ApplyJobService, settingsService *settingsapp.Service, auditService *auditapp.Service, captchaProvider appauth.CaptchaProvider, captchaRisk appauth.CaptchaRiskStore, fileService *fileapp.Service, observations ...httpmiddleware.ObservabilityRuntime) *http.Server {
	return newHTTPServerWithPlanAndCaptchaAndFilesAndAux(cfg, readiness, authService, limiter, iamService, recovery, installStatus, installPlan, dependencyChecks, applyService, jobService, settingsService, auditService, captchaProvider, captchaRisk, fileService, nil, nil, observations...)
}

func newHTTPServerWithPlanAndCaptchaAndFilesAndAux(cfg config.Config, readiness health.ReadinessChecker, authService appauth.AuthService, limiter appauth.RateLimiter, iamService *iamapp.Service, recovery appauth.AccountRecoveryService, installStatus *installer.StatusService, installPlan installer.PlanProvider, dependencyChecks installhttp.DependencyCheckProvider, applyService *installer.ApplyService, jobService *installer.ApplyJobService, settingsService *settingsapp.Service, auditService *auditapp.Service, captchaProvider appauth.CaptchaProvider, captchaRisk appauth.CaptchaRiskStore, fileService *fileapp.Service, mailService *mailapp.Service, monitorService *monitorapp.Service, observations ...httpmiddleware.ObservabilityRuntime) *http.Server {
	var authHandler *authhttp.Handler
	if authService != nil {
		authHandler = authhttp.NewHandler(authService, cfg.Auth, limiter)
		authHandler.SetCaptchaProvider(captchaProvider)
		authHandler.SetCaptchaRiskStore(captchaRisk)
		authHandler.SetAccountRecovery(recovery)
		if sessionManager, ok := authService.(appauth.SessionManagementService); ok {
			authHandler.SetSessionManager(sessionManager)
		}
	}
	var iamHandler *iamhttp.Handler
	if iamService != nil {
		iamHandler = iamhttp.NewHandlerWithAudit(iamService, authService, auditService)
	}
	var installHandler *installhttp.Handler
	if installStatus != nil {
		installHandler = installhttp.NewHandlerWithApplyAndJobs(installStatus, installplatform.NewSystemCapabilityProbe(), installPlan, dependencyChecks, applyService, jobService)
	}
	var auxiliary adminhttp.AuxiliaryRoutes
	if settingsService != nil {
		auxiliary.Settings = settingshttp.NewHandler(settingsService)
	}
	if auditService != nil {
		auxiliary.Audit = audithttp.NewHandler(auditService)
	}
	if fileService != nil {
		auxiliary.Files = filehttp.NewHandler(fileService)
	}
	if mailService != nil {
		auxiliary.Mail = mailhttp.NewHandler(mailService)
	}
	if monitorService != nil {
		auxiliary.Monitor = monitorhttp.NewHandler(monitorService)
	}
	tenantPolicy := httpmiddleware.TenantPolicy{
		Mode:                  cfg.Tenant.Mode,
		DefaultTenantID:       cfg.Tenant.DefaultID,
		TenantHeader:          cfg.Tenant.TenantHeader,
		OrganizationHeader:    cfg.Tenant.OrganizationHeader,
		PlatformAdminSubjects: cfg.Tenant.PlatformAdminSubjects,
	}
	if !cfg.Tenant.Enabled {
		tenantPolicy = httpmiddleware.TenantPolicy{Mode: "single", DefaultTenantID: "default", PlatformAdminSubjects: cfg.Tenant.PlatformAdminSubjects}
	}
	// The dependency-free single-node fixture can run without an auth service;
	// in that explicitly local mode every request is already inside the process
	// boundary, so the read-only monitor remains usable. Auth-enabled runs use
	// the verified-subject allowlist above and never infer this from a header.
	if !cfg.Auth.Enabled && tenantPolicy.Mode == "single" {
		tenantPolicy.IsPlatformAdmin = func(*gin.Context) bool { return true }
	}
	auxiliary.TenantPolicy = &tenantPolicy
	var staticAssets []fs.FS
	if assets, available := webassets.Static(); available {
		staticAssets = append(staticAssets, assets)
	}
	var observation httpmiddleware.ObservabilityRuntime
	if len(observations) > 0 {
		observation = observations[0]
	}
	return &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           router.NewRouterWithRuntimeAndObservability(readiness, authHandler, iamHandler, installHandler, installStatus, firstStaticAsset(staticAssets), observation, auxiliary),
		ReadTimeout:       cfg.Server.ReadTimeout,
		ReadHeaderTimeout: cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}
}

func localFileService(cfg config.Config) *fileapp.Service {
	if !cfg.File.Enabled {
		return nil
	}
	store, err := fileapp.NewLocalStore(cfg.File.Root, cfg.File.BaseURL)
	if err != nil {
		return nil
	}
	maxBytes := cfg.File.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 100 << 20
	}
	return fileapp.NewService(store, fileapp.Config{MaxBytes: maxBytes, AllowedMIMEs: cfg.File.AllowedMIMEs})
}

func firstStaticAsset(assets []fs.FS) fs.FS {
	if len(assets) == 0 {
		return nil
	}
	return assets[0]
}
