package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"example.com/gin-vben-admin/server/internal/bootstrap"
	"example.com/gin-vben-admin/server/internal/config"
)

func main() {
	configPath := flag.String("config", "", "path to the server YAML configuration")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Printf("api configuration error: %v", err)
		os.Exit(2)
	}
	app, err := bootstrap.New(cfg)
	if err != nil {
		log.Printf("api initialization error: %v", err)
		os.Exit(2)
	}
	defer app.Close()

	summary := cfg.SafeSummary()
	log.Printf("api listening on %s (database_enabled=%t database_mode=%s redis_enabled=%t redis_mode=%s)",
		summary.Server.Addr,
		summary.Database.Enabled,
		summary.Database.Mode,
		summary.Redis.Enabled,
		summary.Redis.Mode,
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx); err != nil {
		log.Printf("api stopped with error: %v", err)
		os.Exit(1)
	}
}
