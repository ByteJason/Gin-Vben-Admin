package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/bootstrap"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/config"
)

const loopbackInstallerURL = "http://127.0.0.1"

func main() {
	os.Exit(run(processContext, os.Args[1:], os.Stdout, os.Stderr))
}

func processContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func run(contextFactory func() (context.Context, context.CancelFunc), args []string, output, errorOutput io.Writer) int {
	command, err := parseCommand(args)
	if err != nil {
		fmt.Fprintf(errorOutput, "INIT_RUNTIME_ERROR=ARGUMENT_INVALID\n%v\n", err)
		return 2
	}
	assets, err := loadInstallerAssets(command.assets)
	if err != nil {
		fmt.Fprintf(errorOutput, "INIT_RUNTIME_ERROR=ASSETS_INVALID\n%v\n", err)
		return 2
	}
	cfg, err := config.Load(command.configPath)
	if err != nil {
		fmt.Fprintf(errorOutput, "INIT_RUNTIME_ERROR=CONFIG_INVALID\n%v\n", err)
		return 2
	}
	cfg.Server.Addr = command.addr
	app, err := bootstrap.New(cfg)
	if err != nil {
		fmt.Fprintf(errorOutput, "INIT_RUNTIME_ERROR=BOOTSTRAP_FAILED\n%v\n", err)
		return 2
	}
	defer app.Close()

	server := app.HTTPServer()
	server.Handler = withInstallerAssets(server.Handler, assets)
	fmt.Fprintf(output, "INIT_RUNTIME_ADDR=%s\nINIT_RUNTIME_URL=%s:%s/install\n", command.addr, loopbackInstallerURL, command.addr[len("127.0.0.1:"):])

	ctx, stop := contextFactory()
	defer stop()
	if err := app.Run(ctx); err != nil {
		fmt.Fprintf(errorOutput, "INIT_RUNTIME_ERROR=SERVER_STOPPED\n%v\n", err)
		return 1
	}
	return 0
}
