package migration

import (
	"strings"
	"testing"
)

func TestNewRejectsUnsupportedDriver(t *testing.T) {
	t.Parallel()

	_, err := New("sqlite", "file:ignored")
	if err == nil {
		t.Fatal("New() error = nil, want unsupported driver error")
	}
	if !strings.Contains(err.Error(), "unsupported migration database driver") {
		t.Fatalf("New() error = %q, want unsupported driver message", err)
	}
}

func TestDownRejectsZeroSteps(t *testing.T) {
	t.Parallel()

	err := (&Runner{}).Down(0)
	if err == nil {
		t.Fatal("Down(0) error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "positive") {
		t.Fatalf("Down(0) error = %q, want positive-step error", err)
	}
}

func TestMySQLDSNEnablesMultiStatements(t *testing.T) {
	t.Parallel()

	got, err := mysqlMigrationDSN("root:root@tcp(127.0.0.1:3306)/gin_vben_admin?parseTime=true&multiStatements=false")
	if err != nil {
		t.Fatalf("mysqlMigrationDSN() error = %v", err)
	}
	if !strings.Contains(got, "multiStatements=true") {
		t.Fatalf("mysqlMigrationDSN() = %q, want multiStatements=true", got)
	}
}

func TestPostgresMigrationsUseTheRuntimePGXSQLDriver(t *testing.T) {
	t.Parallel()

	got, err := migrationSQLDriver(DriverPostgres)
	if err != nil {
		t.Fatalf("migrationSQLDriver(postgres) error = %v", err)
	}
	if got != "pgx" {
		t.Fatalf("migrationSQLDriver(postgres) = %q, want pgx so probe and migration share TLS defaults", got)
	}
}

func TestDownRejectsStepsOutsideIntRange(t *testing.T) {
	t.Parallel()

	err := (&Runner{}).Down(uint(maxInt) + 1)
	if err == nil || !strings.Contains(err.Error(), "range") {
		t.Fatalf("Down(too-large) error = %v, want range validation error", err)
	}
}
