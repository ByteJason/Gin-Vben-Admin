// Package migration runs immutable, embedded database schema migrations.
package migration

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/ByteJason/Gin-Vben-Admin/server/migrations"

	migrate "github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	mysqlmigration "github.com/golang-migrate/migrate/v4/database/mysql"
	postgresmigration "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const (
	DriverMySQL    = "mysql"
	DriverPostgres = "postgres"
	maxInt         = int(^uint(0) >> 1)
)

// Status describes the migration state recorded by the selected database.
type Status struct {
	Version uint
	Dirty   bool
	Applied bool
}

// Runner owns one migration engine and the database connection behind it.
type Runner struct {
	engine *migrate.Migrate
}

// New creates a migration runner for a supported write database. It loads SQL
// from the embedded migration tree and never writes the DSN to stdout or stderr.
func New(driver, dsn string) (*Runner, error) {
	driver = strings.ToLower(strings.TrimSpace(driver))
	if !isSupportedDriver(driver) {
		return nil, fmt.Errorf("unsupported migration database driver %q", driver)
	}
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, errors.New("migration database DSN is required")
	}

	source, err := iofs.New(migrations.FS, driver)
	if err != nil {
		return nil, fmt.Errorf("load %s migration assets: %w", driver, err)
	}

	databaseDriver, err := databaseDriver(driver, dsn)
	if err != nil {
		_ = source.Close()
		return nil, err
	}

	engine, err := migrate.NewWithInstance("iofs", source, driver, databaseDriver)
	if err != nil {
		_ = source.Close()
		_ = databaseDriver.Close()
		return nil, fmt.Errorf("initialize %s migration runner: %w", driver, err)
	}
	return &Runner{engine: engine}, nil
}

// Up applies every pending migration. A database already at the latest version
// is a successful no-op.
func (r *Runner) Up() error {
	if r == nil || r.engine == nil {
		return errors.New("migration runner is not initialized")
	}
	if err := r.engine.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// Down reverts exactly steps migrations. steps must be positive.
func (r *Runner) Down(steps uint) error {
	if steps == 0 {
		return errors.New("migration down steps must be positive")
	}
	if uint64(steps) > uint64(maxInt) {
		return errors.New("migration down steps exceed supported range")
	}
	if r == nil || r.engine == nil {
		return errors.New("migration runner is not initialized")
	}
	if err := r.engine.Steps(-int(steps)); err != nil {
		return fmt.Errorf("revert %d migration steps: %w", steps, err)
	}
	return nil
}

// Status returns the currently recorded migration version and dirty state.
func (r *Runner) Status() (Status, error) {
	if r == nil || r.engine == nil {
		return Status{}, errors.New("migration runner is not initialized")
	}

	version, dirty, err := r.engine.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return Status{}, nil
	}
	if err != nil {
		return Status{}, fmt.Errorf("read migration status: %w", err)
	}
	return Status{Version: version, Dirty: dirty, Applied: true}, nil
}

// Close releases the migration source and database resources.
func (r *Runner) Close() error {
	if r == nil || r.engine == nil {
		return nil
	}
	sourceErr, databaseErr := r.engine.Close()
	r.engine = nil
	if sourceErr != nil {
		return fmt.Errorf("close migration source: %w", sourceErr)
	}
	if databaseErr != nil {
		return fmt.Errorf("close migration database: %w", databaseErr)
	}
	return nil
}

func isSupportedDriver(driver string) bool {
	return driver == DriverMySQL || driver == DriverPostgres
}

func databaseDriver(driver, dsn string) (database.Driver, error) {
	sqlDriver, err := migrationSQLDriver(driver)
	if err != nil {
		return nil, err
	}
	switch driver {
	case DriverMySQL:
		normalizedDSN, err := mysqlMigrationDSN(dsn)
		if err != nil {
			return nil, fmt.Errorf("normalize mysql migration DSN: %w", err)
		}
		db, err := sql.Open(sqlDriver, normalizedDSN)
		if err != nil {
			return nil, fmt.Errorf("open mysql migration database: %w", err)
		}
		driver, err := mysqlmigration.WithInstance(db, &mysqlmigration.Config{})
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("connect mysql migration database: %w", err)
		}
		return driver, nil
	case DriverPostgres:
		db, err := sql.Open(sqlDriver, dsn)
		if err != nil {
			return nil, fmt.Errorf("open postgres migration database: %w", err)
		}
		driver, err := postgresmigration.WithInstance(db, &postgresmigration.Config{MultiStatementEnabled: true})
		if err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("connect postgres migration database: %w", err)
		}
		return driver, nil
	default:
		return nil, fmt.Errorf("unsupported migration database driver %q", driver)
	}
}

// migrationSQLDriver keeps the schema runner on the same database/sql driver
// as the runtime connection probe. In particular, pgx defaults an omitted
// PostgreSQL sslmode to prefer, while lib/pq defaults it to require. Using pgx
// for both paths prevents a successful probe from failing immediately when a
// local PostgreSQL server does not offer TLS.
func migrationSQLDriver(driver string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case DriverMySQL:
		return DriverMySQL, nil
	case DriverPostgres:
		return "pgx", nil
	default:
		return "", fmt.Errorf("unsupported migration database driver %q", driver)
	}
}

func mysqlMigrationDSN(dsn string) (string, error) {
	separator := strings.LastIndex(dsn, "?")
	if separator < 0 {
		return dsn + "?multiStatements=true", nil
	}

	query, err := url.ParseQuery(dsn[separator+1:])
	if err != nil {
		return "", errors.New("invalid mysql migration DSN query")
	}
	query.Set("multiStatements", "true")
	return dsn[:separator+1] + query.Encode(), nil
}
