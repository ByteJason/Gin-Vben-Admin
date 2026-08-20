package bootstrap

import (
	"net/http"

	"example.com/gin-vben-admin/server/internal/config"
	"example.com/gin-vben-admin/server/internal/transport/http/health"
	"example.com/gin-vben-admin/server/internal/transport/http/router"
)

func NewHTTPServer(addr string) *http.Server {
	cfg := config.Default()
	cfg.Server.Addr = addr
	return newHTTPServer(cfg, nil)
}

func newHTTPServer(cfg config.Config, readiness health.ReadinessChecker) *http.Server {
	return &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           router.NewRouter(readiness),
		ReadTimeout:       cfg.Server.ReadTimeout,
		ReadHeaderTimeout: cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}
}
