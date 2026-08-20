package bootstrap

import (
	"net/http"

	appauth "example.com/gin-vben-admin/server/internal/application/auth"
	iamapp "example.com/gin-vben-admin/server/internal/application/iam"
	"example.com/gin-vben-admin/server/internal/config"
	authhttp "example.com/gin-vben-admin/server/internal/transport/http/auth"
	"example.com/gin-vben-admin/server/internal/transport/http/health"
	iamhttp "example.com/gin-vben-admin/server/internal/transport/http/iam"
	"example.com/gin-vben-admin/server/internal/transport/http/router"
)

func NewHTTPServer(addr string) *http.Server {
	cfg := config.Default()
	cfg.Server.Addr = addr
	return newHTTPServer(cfg, nil, nil, nil, nil, nil)
}

func newHTTPServer(cfg config.Config, readiness health.ReadinessChecker, authService appauth.AuthService, limiter appauth.RateLimiter, iamService *iamapp.Service, recovery appauth.AccountRecoveryService) *http.Server {
	var authHandler *authhttp.Handler
	if authService != nil {
		authHandler = authhttp.NewHandler(authService, cfg.Auth, limiter)
		authHandler.SetAccountRecovery(recovery)
	}
	var iamHandler *iamhttp.Handler
	if iamService != nil {
		iamHandler = iamhttp.NewHandler(iamService, authService)
	}
	return &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           router.NewRouterWithAuth(readiness, authHandler, iamHandler),
		ReadTimeout:       cfg.Server.ReadTimeout,
		ReadHeaderTimeout: cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}
}
