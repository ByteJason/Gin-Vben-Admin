// Package migration owns the one-shot GORM schema installer used by the
// command line tool and the interactive installer.
//
// The schema is deliberately represented by Go models in
// internal/platform/persistence/model and registered by server/migrations.
// A fresh installation calls Migrator().CreateTable for each model; it never
// performs an incremental column/index alteration. Future upgrades are
// explicit versioned Migrator operations, not startup-time AutoMigrate.
package migration

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	"github.com/ByteJason/Gin-Vben-Admin/server/migrations"
	adminmigrations "github.com/ByteJason/Gin-Vben-Admin/server/migrations/versions/admin"
	"gorm.io/gorm"
)

const (
	DriverMySQL    = gormdb.DriverMySQL
	DriverPostgres = gormdb.DriverPostgres
	// SchemaVersion is the single fresh-install schema version. Keeping a
	// stable value preserves the installer/CLI status contract without a
	// second migration-history table.
	SchemaVersion uint = 1
	maxInt             = int(^uint(0) >> 1)
)

// Status describes whether the single schema has been created.
type Status struct {
	Version uint
	Dirty   bool
	Applied bool
}

// Runner owns the configured primary GORM connection used by the schema
// operation. It intentionally does not hold a separate database/sql
// migration connection, so the selected driver and pool are identical to the
// runtime database.
type Runner struct {
	store  *gormdb.Store
	db     *gorm.DB
	driver string
}

// New creates a runner for a supported write database. The DSN is consumed by
// gormdb.Open and is never included in returned errors or command output.
func New(driver, dsn string) (*Runner, error) {
	driver = gormdb.NormalizeDriver(driver)
	if !isSupportedDriver(driver) {
		return nil, fmt.Errorf("unsupported migration database driver %q", driver)
	}
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, errors.New("migration database DSN is required")
	}
	store, err := gormdb.Open(gormdb.Options{
		Driver: driver,
		Mode:   gormdb.ModeSingle,
		DSN:    dsn,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize %s migration database: %w", driver, err)
	}
	return &Runner{store: store, db: store.DB(), driver: driver}, nil
}

// NewWithDB builds a runner around an already initialized GORM handle. It is
// useful to share the application's primary connection and keeps unit tests
// from opening a second endpoint. The runner does not close a borrowed DB.
func NewWithDB(db *gorm.DB, driver string) (*Runner, error) {
	if db == nil {
		return nil, errors.New("migration database is not initialized")
	}
	driver = gormdb.NormalizeDriver(driver)
	if !isSupportedDriver(driver) {
		return nil, fmt.Errorf("unsupported migration database driver %q", driver)
	}
	return &Runner{db: db, driver: driver}, nil
}

// Up creates all missing tables and seeds the fixed bootstrap rows. Existing
// tables are left untouched; this is the intended fresh-install behaviour.
func (r *Runner) Up() error {
	if err := r.validate(); err != nil {
		return err
	}
	if err := migrations.CreateSchema(r.db); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

// ApplySettingsMailCleanup runs the explicit v003 data migration against the
// same primary connection used by the schema runner. It is deliberately a
// separate operation from Up: creating the fresh schema must never silently
// delete data, while an operator can invoke this idempotent cleanup during a
// reviewed upgrade window. The optional cache adapter receives only the
// narrowly scoped settings-cache cleanup hook; independent mail tables and
// caches remain outside this migration boundary.
func (r *Runner) ApplySettingsMailCleanup(ctx context.Context, cache adminmigrations.LegacyMailCacheCleaner) (adminmigrations.CleanupReport, error) {
	if err := r.validate(); err != nil {
		return adminmigrations.CleanupReport{}, err
	}
	return adminmigrations.UpV003WithCache(ctx, r.db, cache)
}

// UpSettingsMailCleanup is a descriptive alias for callers that name data
// migrations by their upward direction.
func (r *Runner) UpSettingsMailCleanup(ctx context.Context, cache adminmigrations.LegacyMailCacheCleaner) (adminmigrations.CleanupReport, error) {
	return r.ApplySettingsMailCleanup(ctx, cache)
}

// Down removes the one schema as a reversible local-install operation. There
// is only one schema version, therefore steps must be exactly one.
func (r *Runner) Down(steps uint) error {
	if steps == 0 {
		return errors.New("migration down steps must be positive")
	}
	if uint64(steps) > uint64(maxInt) {
		return errors.New("migration down steps exceed supported range")
	}
	if steps > SchemaVersion {
		return fmt.Errorf("migration down steps exceed schema version %d", SchemaVersion)
	}
	if err := r.validate(); err != nil {
		return err
	}
	if err := migrations.DropSchema(r.db); err != nil {
		return fmt.Errorf("revert schema: %w", err)
	}
	return nil
}

// Status checks the bootstrap table on the configured primary connection.
// GORM's Migrator has no dirty flag for CreateTable, so Dirty is always false.
func (r *Runner) Status() (Status, error) {
	if err := r.validate(); err != nil {
		return Status{}, err
	}
	applied, err := migrations.SchemaStatus(r.db)
	if err != nil {
		return Status{}, fmt.Errorf("read migration status: %w", err)
	}
	if !applied {
		return Status{}, nil
	}
	return Status{Version: SchemaVersion, Applied: true}, nil
}

// Close releases an owned gormdb.Store. Borrowed handles created by
// NewWithDB are left to their owner.
func (r *Runner) Close() error {
	if r == nil {
		return nil
	}
	var err error
	if r.store != nil {
		err = r.store.Close()
	}
	r.store = nil
	r.db = nil
	return err
}

func (r *Runner) validate() error {
	if r == nil || r.db == nil {
		return errors.New("migration runner is not initialized")
	}
	return nil
}

func isSupportedDriver(driver string) bool {
	driver = gormdb.NormalizeDriver(driver)
	return driver == DriverMySQL || driver == DriverPostgres
}

// PrimaryDB is a convenience for code that wants the same write handle as a
// runner without reaching into its implementation.
func (r *Runner) PrimaryDB(ctx context.Context) *gorm.DB {
	if r == nil || r.db == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return r.db.WithContext(ctx)
}
