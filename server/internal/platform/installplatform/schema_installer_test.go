package installplatform

import (
	"context"
	"errors"
	"strings"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
	migratedatabase "github.com/golang-migrate/migrate/v4/database"

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

func TestSchemaInstallerPreservesSafeTLSFailureDiagnostic(t *testing.T) {
	t.Parallel()

	cause := errors.New("pq: SSL is not enabled on the server; password=database-secret")
	service := NewSchemaInstaller(func(string, string) (SchemaRunner, error) {
		return nil, cause
	})
	_, err := service.Up(context.Background(), installer.DatabaseConnection{
		Driver: "postgres", Mode: "single", Host: "127.0.0.1", Port: 5432,
		Database: "app", Username: "installer", Password: "database-secret",
	})
	if !errors.Is(err, ErrSchemaInstallation) || !errors.Is(err, cause) {
		t.Fatalf("Up() error = %v, want schema sentinel and original cause in the chain", err)
	}
	var diagnostic installer.FailureDiagnosticProvider
	if !errors.As(err, &diagnostic) {
		t.Fatalf("Up() error = %T %v, want FailureDiagnosticProvider", err, err)
	}
	got := diagnostic.InstallationFailureDiagnostic()
	if got.Operation != "connect" || got.Reason != "tls_mode_mismatch" {
		t.Fatalf("diagnostic = %#v, want connect/tls_mode_mismatch", got)
	}
	if strings.Contains(err.Error(), "database-secret") || strings.Contains(err.Error(), "SSL is not enabled") {
		t.Fatalf("public schema error leaked raw database detail: diagnostic=%#v error=%q", got, err)
	}
}

func TestSchemaInstallerReportsDirtyMigrationState(t *testing.T) {
	t.Parallel()

	runner := &schemaRunnerStub{status: migration.Status{Version: 7, Applied: true, Dirty: true}}
	service := NewSchemaInstaller(func(string, string) (SchemaRunner, error) { return runner, nil })
	_, err := service.Up(context.Background(), installer.DatabaseConnection{
		Driver: "postgres", Mode: "single", Host: "127.0.0.1", Port: 5432,
		Database: "app", Username: "installer",
	})
	var diagnostic installer.FailureDiagnosticProvider
	if !errors.As(err, &diagnostic) {
		t.Fatalf("Up() error = %T %v, want FailureDiagnosticProvider", err, err)
	}
	got := diagnostic.InstallationFailureDiagnostic()
	if got.Operation != "status" || got.Reason != "migration_dirty" {
		t.Fatalf("diagnostic = %#v, want status/migration_dirty", got)
	}
}

func TestSchemaInstallerExtractsMySQLSQLStateWithoutLeakingMessage(t *testing.T) {
	t.Parallel()

	cause := &mysqldriver.MySQLError{
		Number:   1045,
		SQLState: [5]byte{'2', '8', '0', '0', '0'},
		Message:  "Access denied password=database-secret",
	}
	service := NewSchemaInstaller(func(string, string) (SchemaRunner, error) { return nil, cause })
	_, err := service.Up(context.Background(), installer.DatabaseConnection{
		Driver: "mysql", Mode: "single", Host: "127.0.0.1", Port: 3306,
		Database: "app", Username: "installer", Password: "database-secret",
	})
	var diagnostic installer.FailureDiagnosticProvider
	if !errors.As(err, &diagnostic) {
		t.Fatalf("Up() error = %T %v, want FailureDiagnosticProvider", err, err)
	}
	got := diagnostic.InstallationFailureDiagnostic()
	if got.Operation != "connect" || got.Reason != "authentication_failed" || got.DatabaseCode != "28000" {
		t.Fatalf("diagnostic = %#v, want connect/authentication_failed/28000", got)
	}
	if strings.Contains(err.Error(), "database-secret") || strings.Contains(err.Error(), "Access denied") {
		t.Fatalf("schema error leaked MySQL message: %q", err)
	}
}

func TestSchemaInstallerExtractsMySQLSQLStateFromMigrationError(t *testing.T) {
	t.Parallel()

	cause := &mysqldriver.MySQLError{
		Number:   1142,
		SQLState: [5]byte{'4', '2', '0', '0', '0'},
		Message:  "CREATE command denied password=database-secret",
	}
	runner := &schemaRunnerStub{upErr: migratedatabase.Error{OrigErr: cause, Err: "migration failed"}}
	service := NewSchemaInstaller(func(string, string) (SchemaRunner, error) { return runner, nil })
	_, err := service.Up(context.Background(), installer.DatabaseConnection{
		Driver: "mysql", Mode: "single", Host: "127.0.0.1", Port: 3306,
		Database: "app", Username: "installer", Password: "database-secret",
	})
	var diagnostic installer.FailureDiagnosticProvider
	if !errors.As(err, &diagnostic) {
		t.Fatalf("Up() error = %T %v, want FailureDiagnosticProvider", err, err)
	}
	got := diagnostic.InstallationFailureDiagnostic()
	if got.Operation != "apply" || got.Reason != "migration_statement_failed" || got.DatabaseCode != "42000" {
		t.Fatalf("diagnostic = %#v, want apply/migration_statement_failed/42000", got)
	}
	if strings.Contains(err.Error(), "database-secret") || strings.Contains(err.Error(), "CREATE command") {
		t.Fatalf("schema error leaked MySQL migration message: %q", err)
	}
}

func TestSchemaInstallerDistinguishesTLSCertificateFailures(t *testing.T) {
	t.Parallel()

	cause := errors.New("tls handshake failed: x509: certificate signed by unknown authority")
	service := NewSchemaInstaller(func(string, string) (SchemaRunner, error) { return nil, cause })
	_, err := service.Up(context.Background(), installer.DatabaseConnection{
		Driver: "postgres", Mode: "single", Host: "db.example", Port: 5432,
		Database: "app", Username: "installer", Password: "database-secret",
		TLSMode: "verify-full",
	})
	var diagnostic installer.FailureDiagnosticProvider
	if !errors.As(err, &diagnostic) {
		t.Fatalf("Up() error = %T %v, want FailureDiagnosticProvider", err, err)
	}
	got := diagnostic.InstallationFailureDiagnostic()
	if got.Operation != "connect" || got.Reason != "tls_configuration_failed" {
		t.Fatalf("diagnostic = %#v, want connect/tls_configuration_failed", got)
	}
}

func TestSchemaInstallerPrefersSQLStateOverTLSFallbackNoise(t *testing.T) {
	t.Parallel()

	cause := errors.Join(
		errors.New("tls error: server refused TLS connection"),
		schemaSQLStateError{code: "3D000"},
	)
	service := NewSchemaInstaller(func(string, string) (SchemaRunner, error) { return nil, cause })
	_, err := service.Up(context.Background(), installer.DatabaseConnection{
		Driver: "postgres", Mode: "single", Host: "127.0.0.1", Port: 5432,
		Database: "missing", Username: "installer",
	})
	var diagnostic installer.FailureDiagnosticProvider
	if !errors.As(err, &diagnostic) {
		t.Fatalf("Up() error = %T %v, want FailureDiagnosticProvider", err, err)
	}
	got := diagnostic.InstallationFailureDiagnostic()
	if got.Reason != "database_unavailable" || got.DatabaseCode != "3D000" {
		t.Fatalf("diagnostic = %#v, want database_unavailable/3D000", got)
	}
}

func TestSchemaInstallerClassifiesMigrationSQLStateWithoutLeakingQuery(t *testing.T) {
	t.Parallel()

	cause := schemaSQLStateError{code: "42501"}
	runner := &schemaRunnerStub{
		upErr: migratedatabase.Error{
			OrigErr: cause,
			Err:     "migration failed",
			Query:   []byte("SELECT 'password=database-secret'"),
		},
	}
	service := NewSchemaInstaller(func(string, string) (SchemaRunner, error) { return runner, nil })
	_, err := service.Up(context.Background(), installer.DatabaseConnection{
		Driver: "postgres", Mode: "single", Host: "127.0.0.1", Port: 5432,
		Database: "app", Username: "installer", Password: "database-secret",
	})
	var diagnostic installer.FailureDiagnosticProvider
	if !errors.As(err, &diagnostic) {
		t.Fatalf("Up() error = %T %v, want FailureDiagnosticProvider", err, err)
	}
	got := diagnostic.InstallationFailureDiagnostic()
	if got.Operation != "apply" || got.Reason != "permission_denied" || got.DatabaseCode != "42501" {
		t.Fatalf("diagnostic = %#v, want apply/permission_denied/42501", got)
	}
	if strings.Contains(err.Error(), "database-secret") || strings.Contains(err.Error(), "SELECT") {
		t.Fatalf("schema error leaked migration query: %q", err)
	}
}

type schemaSQLStateError struct{ code string }

func (e schemaSQLStateError) Error() string    { return "database operation failed" }
func (e schemaSQLStateError) SQLState() string { return e.code }

type schemaRunnerStub struct {
	status      migration.Status
	upErr       error
	statusErr   error
	closeErr    error
	upCalls     int
	statusCalls int
	closeCalls  int
}

func (s *schemaRunnerStub) Up() error {
	s.upCalls++
	return s.upErr
}

func (s *schemaRunnerStub) Status() (migration.Status, error) {
	s.statusCalls++
	return s.status, s.statusErr
}

func (s *schemaRunnerStub) Close() error {
	s.closeCalls++
	return s.closeErr
}
