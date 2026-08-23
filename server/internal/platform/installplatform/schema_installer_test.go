package installplatform

import (
	"context"
	"strings"
	"testing"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/migration"
)

func TestSchemaInstallerRunsEmbeddedMigrationsAgainstWriteEndpoint(t *testing.T) {
	t.Parallel()

	runner := &schemaRunnerStub{status: migration.Status{Version: 4, Applied: true}}
	var driver, dsn string
	service := NewSchemaInstaller(func(gotDriver, gotDSN string) (SchemaRunner, error) {
		driver, dsn = gotDriver, gotDSN
		return runner, nil
	})
	receipt, err := service.Up(context.Background(), installer.DatabaseConnection{
		Driver: "mysql", Mode: "single", Host: "127.0.0.1", Port: 3306,
		Database: "app", Username: "installer", Password: "database-secret",
	})
	if err != nil {
		t.Fatalf("Up() error = %v", err)
	}
	if driver != "mysql" || !strings.Contains(dsn, "127.0.0.1:3306") || !strings.Contains(dsn, "installer") {
		t.Fatalf("factory driver/dsn = %q/%q", driver, dsn)
	}
	if receipt.Version != 4 || runner.upCalls != 1 || runner.statusCalls != 1 || runner.closeCalls != 1 {
		t.Fatalf("receipt/runner = %#v/%#v", receipt, runner)
	}
}

type schemaRunnerStub struct {
	status      migration.Status
	upCalls     int
	statusCalls int
	closeCalls  int
}

func (s *schemaRunnerStub) Up() error {
	s.upCalls++
	return nil
}

func (s *schemaRunnerStub) Status() (migration.Status, error) {
	s.statusCalls++
	return s.status, nil
}

func (s *schemaRunnerStub) Close() error {
	s.closeCalls++
	return nil
}
