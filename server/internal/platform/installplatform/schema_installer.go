package installplatform

import (
	"context"
	"errors"
	"strings"

	mysqldriver "github.com/go-sql-driver/mysql"
	migrate "github.com/golang-migrate/migrate/v4"
	migratedatabase "github.com/golang-migrate/migrate/v4/database"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	migrationplatform "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/migration"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
)

var ErrSchemaInstallation = errors.New("database schema installation failed")

type SchemaRunner interface {
	Up() error
	Status() (migrationplatform.Status, error)
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
		return migrationplatform.New(driver, dsn)
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
		return installer.SchemaReceipt{}, newSchemaInstallationError("connect", "invalid_configuration", "", ErrSchemaInstallation)
	}
	options, err := databaseOptionsFromRequest(request)
	if err != nil {
		return installer.SchemaReceipt{}, newSchemaInstallationError("connect", "invalid_configuration", "", err)
	}
	dsn := strings.TrimSpace(options.DSN)
	if options.Mode == gormdb.ModeReadWrite {
		dsn = strings.TrimSpace(options.PrimaryDSN)
	}
	if dsn == "" {
		return installer.SchemaReceipt{}, newSchemaInstallationError("connect", "invalid_configuration", "", errors.New("migration database endpoint is empty"))
	}
	runner, err := s.open(options.Driver, dsn)
	if err != nil {
		reason, databaseCode := classifySchemaFailure("connect", err)
		return installer.SchemaReceipt{}, newSchemaInstallationError("connect", reason, databaseCode, err)
	}
	defer func() {
		if err := runner.Close(); err != nil && resultErr == nil {
			receipt = installer.SchemaReceipt{}
			reason, databaseCode := classifySchemaFailure("close", err)
			resultErr = newSchemaInstallationError("close", reason, databaseCode, err)
		}
	}()
	if err := runner.Up(); err != nil {
		reason, databaseCode := classifySchemaFailure("apply", err)
		return installer.SchemaReceipt{}, newSchemaInstallationError("apply", reason, databaseCode, err)
	}
	if err := ctx.Err(); err != nil {
		return installer.SchemaReceipt{}, err
	}
	status, err := runner.Status()
	if err != nil {
		reason, databaseCode := classifySchemaFailure("status", err)
		return installer.SchemaReceipt{}, newSchemaInstallationError("status", reason, databaseCode, err)
	}
	if status.Dirty {
		return installer.SchemaReceipt{}, newSchemaInstallationError("status", "migration_dirty", "", errors.New("migration status is dirty"))
	}
	if !status.Applied {
		return installer.SchemaReceipt{}, newSchemaInstallationError("status", "migration_status_failed", "", errors.New("migration status is not applied"))
	}
	return installer.SchemaReceipt{Version: status.Version}, nil
}

type schemaInstallationError struct {
	operation    string
	reason       string
	databaseCode string
	cause        error
}

func newSchemaInstallationError(operation, reason, databaseCode string, cause error) error {
	return &schemaInstallationError{
		operation:    operation,
		reason:       reason,
		databaseCode: normalizedDatabaseCode(databaseCode),
		cause:        cause,
	}
}

// Error deliberately excludes the driver error. The original cause remains in
// the errors.Is/errors.As chain for internal classification, while HTTP and
// structured logging consume InstallationFailureDiagnostic instead.
func (e *schemaInstallationError) Error() string {
	return ErrSchemaInstallation.Error()
}

func (e *schemaInstallationError) Unwrap() error {
	return e.cause
}

func (e *schemaInstallationError) Is(target error) bool {
	return target == ErrSchemaInstallation
}

func (e *schemaInstallationError) InstallationFailureDiagnostic() installer.FailureDiagnostic {
	if e == nil {
		return installer.FailureDiagnostic{}
	}
	return installer.FailureDiagnostic{
		Reason:       e.reason,
		Operation:    e.operation,
		DatabaseCode: e.databaseCode,
	}
}

func classifySchemaFailure(operation string, err error) (reason, databaseCode string) {
	databaseCode = databaseErrorCode(err)
	lower := strings.ToLower(errString(err))
	var dirty migrate.ErrDirty
	if errors.As(err, &dirty) {
		return "migration_dirty", databaseCode
	}

	switch {
	case strings.HasPrefix(databaseCode, "28"):
		return "authentication_failed", databaseCode
	case databaseCode == "42501":
		return "permission_denied", databaseCode
	case databaseCode == "3F000":
		return "schema_unavailable", databaseCode
	case databaseCode == "3D000":
		return "database_unavailable", databaseCode
	case databaseCode == "42P07" || databaseCode == "42701" || databaseCode == "42710":
		return "schema_conflict", databaseCode
	case databaseCode == "55P03" || databaseCode == "40P01":
		return "database_busy", databaseCode
	case strings.HasPrefix(databaseCode, "08"):
		return "database_unavailable", databaseCode
	}
	if strings.Contains(lower, "ssl is not enabled") ||
		strings.Contains(lower, "server does not support ssl") ||
		strings.Contains(lower, "server refused tls connection") {
		return "tls_mode_mismatch", databaseCode
	}
	if strings.Contains(lower, "tls handshake") || strings.Contains(lower, "x509:") {
		return "tls_configuration_failed", databaseCode
	}

	switch operation {
	case "connect":
		return "database_unavailable", databaseCode
	case "apply":
		return "migration_statement_failed", databaseCode
	case "status":
		return "migration_status_failed", databaseCode
	case "close":
		return "migration_close_failed", databaseCode
	default:
		return "unknown", databaseCode
	}
}

func databaseErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if code := directDatabaseErrorCode(err); code != "" {
		return code
	}
	var queryError *migratedatabase.Error
	if errors.As(err, &queryError) && queryError != nil {
		return directDatabaseErrorCode(queryError.OrigErr)
	}
	// Multi-statement migration execution returns database.Error as a value,
	// while connection/status paths generally return a pointer.
	var queryErrorValue migratedatabase.Error
	if errors.As(err, &queryErrorValue) {
		return directDatabaseErrorCode(queryErrorValue.OrigErr)
	}
	return ""
}

func directDatabaseErrorCode(err error) string {
	if err == nil {
		return ""
	}
	type sqlStateProvider interface {
		SQLState() string
	}
	var state sqlStateProvider
	if errors.As(err, &state) {
		return normalizedDatabaseCode(state.SQLState())
	}
	var mysqlError *mysqldriver.MySQLError
	if errors.As(err, &mysqlError) && mysqlError != nil {
		return normalizedDatabaseCode(string(mysqlError.SQLState[:]))
	}
	return ""
}

func normalizedDatabaseCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) != 5 {
		return ""
	}
	for _, character := range code {
		if (character < '0' || character > '9') && (character < 'A' || character > 'Z') {
			return ""
		}
	}
	return code
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
