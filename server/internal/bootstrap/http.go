package bootstrap

import (
	"net/http"

	appauth "example.com/gin-vben-admin/server/internal/application/auth"
	iamapp "example.com/gin-vben-admin/server/internal/application/iam"
	installer "example.com/gin-vben-admin/server/internal/application/installer"
	"example.com/gin-vben-admin/server/internal/config"
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
	if len(installStatuses) > 0 && installStatuses[0] != nil {
		installHandler = installhttp.NewHandler(installStatuses[0])
	}
	return &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           router.NewRouterWithComponents(readiness, authHandler, iamHandler, installHandler),
		ReadTimeout:       cfg.Server.ReadTimeout,
		ReadHeaderTimeout: cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}
}
