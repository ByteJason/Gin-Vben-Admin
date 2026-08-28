// Command admin-password performs local-only recovery of the administrator
// recorded by a completed installation. Password material is accepted only as
// two matching stdin lines and is never emitted by the command.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/bootstrap"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/config"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/authplatform"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/installplatform"
)

type resetRuntime interface {
	Reset(context.Context, string) error
	Close() error
}

type runtimeFactory func(string) (resetRuntime, error)

type initialAdminRuntime struct {
	dependencies *bootstrap.CredentialRecoveryDependencies
	service      *installer.InitialAdminPasswordResetService
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdin, os.Stdout, newInitialAdminRuntime))
}

func run(ctx context.Context, args []string, input io.Reader, output io.Writer, factory runtimeFactory) int {
	command, err := parseCommand(args)
	if err != nil {
		writeFailure(output, "invalid_command")
		return 2
	}

	password, err := readConfirmedPassword(input)
	if err != nil {
		writeFailure(output, "input")
		return 2
	}
	if factory == nil {
		writeFailure(output, "initialize")
		return 1
	}
	runtime, err := factory(command.configPath)
	if err != nil || runtime == nil {
		writeFailure(output, "initialize")
		return 1
	}

	resetErr := runtime.Reset(ctx, password)
	closeErr := runtime.Close()
	if resetErr != nil {
		writeFailure(output, "operation")
		return 1
	}
	if closeErr != nil {
		writeFailure(output, "close")
		return 1
	}

	writeSuccess(output)
	return 0
}

func newInitialAdminRuntime(configPath string) (resetRuntime, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, errors.New("load admin password runtime configuration")
	}
	if !cfg.Database.Enabled || !cfg.Redis.Enabled || !cfg.Auth.Enabled {
		return nil, errors.New("admin password reset requires database, redis, and auth")
	}

	dependencies, err := bootstrap.NewCredentialRecoveryDependencies(cfg)
	if err != nil {
		return nil, errors.New("initialize admin password runtime")
	}
	if dependencies.Database() == nil || dependencies.Redis() == nil {
		_ = dependencies.Close()
		return nil, errors.New("initialize admin password dependencies")
	}

	store := installplatform.NewGORMInitialAdminPasswordStore(dependencies.Database())
	hasher := authplatform.BcryptHasher{Cost: cfg.Auth.BcryptCost}
	attempts := authplatform.NewRedisLoginAttemptStore(dependencies.Redis(), cfg.Auth.LockoutThreshold, cfg.Auth.LockoutDuration)
	service := installer.NewInitialAdminPasswordResetService(store, hasher, attempts)
	return &initialAdminRuntime{dependencies: dependencies, service: service}, nil
}

func (r *initialAdminRuntime) Reset(ctx context.Context, password string) error {
	if r == nil || r.service == nil {
		return errors.New("admin password runtime is not initialized")
	}
	return r.service.Reset(ctx, password)
}

func (r *initialAdminRuntime) Close() error {
	if r == nil || r.dependencies == nil {
		return nil
	}
	return r.dependencies.Close()
}

func writeSuccess(output io.Writer) {
	if output == nil {
		return
	}
	_, _ = fmt.Fprint(output, "ADMIN_PASSWORD_RESET=OK\nLOGIN_FAILURE_STATE_RESET=OK\n")
}

func writeFailure(output io.Writer, category string) {
	if output == nil {
		return
	}
	_, _ = fmt.Fprintf(output, "ADMIN_PASSWORD_RESET=ERROR\nADMIN_PASSWORD_RESET_ERROR=%s\n", category)
}
