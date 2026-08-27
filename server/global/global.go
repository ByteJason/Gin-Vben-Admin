// Package global contains the process-wide database handles used by the
// installation/migration seam.  The application still passes a concrete
// *gorm.DB to repositories; this package is only the small compatibility
// surface required by code that needs to run a schema operation from the
// configured global database.
package global

import (
	"strings"
	"sync"

	"gorm.io/gorm"
)

var (
	// DB is the currently configured GORM database.  It is kept exported for
	// compatibility with the conventional global.DB.Migrator() call site.
	// New code should prefer GetDB/SetDatabase when it can do so.
	DB *gorm.DB

	// Driver is the canonical database driver name ("mysql" or "postgres").
	// It is exported for the same compatibility reason as DB.
	Driver string

	stateMu sync.RWMutex
)

// SetDatabase publishes the handle and canonical driver as one state change.
// Database constructors use this form after the selected dialect is fully
// initialized.
func SetDatabase(db *gorm.DB, driver string) {
	stateMu.Lock()
	DB = db
	Driver = normalizeDriver(driver)
	stateMu.Unlock()
}

// SetDB publishes the process-wide GORM database handle.
func SetDB(db *gorm.DB) {
	stateMu.Lock()
	DB = db
	stateMu.Unlock()
}

// GetDB returns the process-wide GORM database handle, or nil when no database
// has been configured.
func GetDB() *gorm.DB {
	stateMu.RLock()
	db := DB
	stateMu.RUnlock()
	return db
}

// SetDriver publishes a canonical database driver name.  Aliases are
// normalized here so callers cannot accidentally expose "pgsql" and
// "postgres" as two different runtime identities.
func SetDriver(driver string) {
	stateMu.Lock()
	Driver = normalizeDriver(driver)
	stateMu.Unlock()
}

// GetDriver returns the currently configured canonical database driver name.
func GetDriver() string {
	stateMu.RLock()
	driver := Driver
	stateMu.RUnlock()
	return driver
}

// Reset clears both global database values.  Store.Close calls this only when
// the global handle still belongs to that store, preventing one test or
// process component from clearing a newer database opened by another.
func Reset() {
	stateMu.Lock()
	DB = nil
	Driver = ""
	stateMu.Unlock()
}

func normalizeDriver(driver string) string {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "pgsql", "postgresql", "pg":
		return "postgres"
	case "mysql":
		return "mysql"
	default:
		return strings.ToLower(strings.TrimSpace(driver))
	}
}
