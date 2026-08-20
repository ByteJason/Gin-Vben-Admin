package bootstrap

import (
	"net/http"

	appauth "example.com/gin-vben-admin/server/internal/application/auth"
	"example.com/gin-vben-admin/server/internal/config"
	authhttp "example.com/gin-vben-admin/server/internal/transport/http/auth"
	"example.com/gin-vben-admin/server/internal/transport/http/health"
	"example.com/gin-vben-admin/server/internal/transport/http/router"
)

func NewHTTPServer(addr string) *http.Server {
	cfg := config.Default()
	cfg.Server.Addr = addr
	return newHTTPServer(cfg, nil, nil, nil)
}

func newHTTPServer(cfg config.Config, readiness health.ReadinessChecker, authService appauth.AuthService, limiter appauth.RateLimiter) *http.Server {
	var authHandler *authhttp.Handler
	if authService != nil {
		authHandler = authhttp.NewHandler(authService, cfg.Auth, limiter)
	}
	return &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           router.NewRouterWithAuth(readiness, authHandler),
		ReadTimeout:       cfg.Server.ReadTimeout,
		ReadHeaderTimeout: cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}
}
