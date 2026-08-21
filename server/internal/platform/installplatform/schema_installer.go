package installplatform

import (
	"context"
	"errors"
	"strings"

	installer "example.com/gin-vben-admin/server/internal/application/installer"
	"example.com/gin-vben-admin/server/internal/platform/migration"
	"example.com/gin-vben-admin/server/internal/platform/persistence/gormdb"
)

var ErrSchemaInstallation = errors.New("database schema installation failed")

type SchemaRunner interface {
	Up() error
	Status() (migration.Status, error)
	Close() error
}

type SchemaRunnerFactory func(driver, dsn string) (SchemaRunner, error)

type SchemaInstaller struct {
	open SchemaRunnerFactory
}

func NewSchemaInstaller(factory SchemaRunnerFactory) *SchemaInstaller {
	return &SchemaInstaller{open: factory}
}

func NewSystemSchemaInstaller() *SchemaInstaller {
	return NewSchemaInstaller(func(driver, dsn string) (SchemaRunner, error) {
		return migration.New(driver, dsn)
	})
}

func (s *SchemaInstaller) Up(ctx context.Context, request installer.DatabaseConnection) (receipt installer.SchemaReceipt, resultErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return installer.SchemaReceipt{}, err
	}
	if s == nil || s.open == nil {
		return installer.SchemaReceipt{}, ErrSchemaInstallation
	}
	options, err := databaseOptionsFromRequest(request)
	if err != nil {
		return installer.SchemaReceipt{}, ErrSchemaInstallation
	}
	dsn := strings.TrimSpace(options.DSN)
	if options.Mode == gormdb.ModeReadWrite {
		dsn = strings.TrimSpace(options.PrimaryDSN)
	}
	if dsn == "" {
		return installer.SchemaReceipt{}, ErrSchemaInstallation
	}
	runner, err := s.open(options.Driver, dsn)
	if err != nil {
		return installer.SchemaReceipt{}, ErrSchemaInstallation
	}
	defer func() {
		if err := runner.Close(); err != nil && resultErr == nil {
			receipt = installer.SchemaReceipt{}
			resultErr = ErrSchemaInstallation
		}
	}()
	if err := runner.Up(); err != nil {
		return installer.SchemaReceipt{}, ErrSchemaInstallation
	}
	if err := ctx.Err(); err != nil {
		return installer.SchemaReceipt{}, err
	}
	status, err := runner.Status()
	if err != nil || !status.Applied || status.Dirty {
		return installer.SchemaReceipt{}, ErrSchemaInstallation
	}
	return installer.SchemaReceipt{Version: status.Version}, nil
}
