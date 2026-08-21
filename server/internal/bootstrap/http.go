package bootstrap

import (
	"io/fs"
	"net/http"

	appauth "example.com/gin-vben-admin/server/internal/application/auth"
	iamapp "example.com/gin-vben-admin/server/internal/application/iam"
	installer "example.com/gin-vben-admin/server/internal/application/installer"
	"example.com/gin-vben-admin/server/internal/config"
	"example.com/gin-vben-admin/server/internal/platform/installplatform"
	"example.com/gin-vben-admin/server/internal/platform/webassets"
	authhttp "example.com/gin-vben-admin/server/internal/transport/http/auth"
	"example.com/gin-vben-admin/server/internal/transport/http/health"
	iamhttp "example.com/gin-vben-admin/server/internal/transport/http/iam"
	installhttp "example.com/gin-vben-admin/server/internal/transport/http/install"
	"example.com/gin-vben-admin/server/internal/transport/http/router"
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
	return newHTTPServerWithPlan(cfg, readiness, authService, limiter, iamService, recovery, installStatus, nil, nil)
}

func newHTTPServerWithPlan(cfg config.Config, readiness health.ReadinessChecker, authService appauth.AuthService, limiter appauth.RateLimiter, iamService *iamapp.Service, recovery appauth.AccountRecoveryService, installStatus *installer.StatusService, installPlan installer.PlanProvider, dependencyChecks installhttp.DependencyCheckProvider, applyServices ...*installer.ApplyService) *http.Server {
	var authHandler *authhttp.Handler
	if authService != nil {
		authHandler = authhttp.NewHandler(authService, cfg.Auth, limiter)
		authHandler.SetAccountRecovery(recovery)
		if sessionManager, ok := authService.(appauth.SessionManagementService); ok {
			authHandler.SetSessionManager(sessionManager)
		}
	}
	var iamHandler *iamhttp.Handler
	if iamService != nil {
		iamHandler = iamhttp.NewHandler(iamService, authService)
	}
	var installHandler *installhttp.Handler
	var applyService *installer.ApplyService
	if len(applyServices) > 0 {
		applyService = applyServices[0]
	}
	if installStatus != nil {
		installHandler = installhttp.NewHandlerWithApply(installStatus, installplatform.NewSystemCapabilityProbe(), installPlan, dependencyChecks, applyService)
	}
	var staticAssets []fs.FS
	if assets, available := webassets.Static(); available {
		staticAssets = append(staticAssets, assets)
	}
	return &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           router.NewRouterWithComponents(readiness, authHandler, iamHandler, installHandler, staticAssets...),
		ReadTimeout:       cfg.Server.ReadTimeout,
		ReadHeaderTimeout: cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}
}
