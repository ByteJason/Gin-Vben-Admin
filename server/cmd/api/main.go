package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/bootstrap"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/config"
	"github.com/gin-gonic/gin"
)

func main() {
	configPath := flag.String("config", "", "path to the server YAML configuration")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Printf("api configuration error: %v", err)
		os.Exit(2)
	}
	// Keep normal startup quiet; set logging.level=debug when Gin route
	// registration diagnostics are needed during local troubleshooting.
	if strings.EqualFold(strings.TrimSpace(cfg.Logging.Level), "debug") {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	app, err := bootstrap.New(cfg)
	if err != nil {
		log.Printf("api initialization error: %v", err)
		os.Exit(2)
	}
	defer app.Close()

	log.Printf("api listening on %s", cfg.Server.Addr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx); err != nil {
		log.Printf("api stopped with error: %v", err)
		os.Exit(1)
	}
}
