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

func TestPostgresAliasesSelectTheGORMPostgresDriver(t *testing.T) {
	t.Parallel()

	for _, driver := range []string{"postgres", "pgsql", "postgresql", "pg"} {
		if !isSupportedDriver(driver) {
			t.Fatalf("isSupportedDriver(%q) = false", driver)
		}
	}
}

func TestDownRejectsStepsOutsideIntRange(t *testing.T) {
	t.Parallel()

	err := (&Runner{}).Down(uint(maxInt) + 1)
	if err == nil || !strings.Contains(err.Error(), "range") {
		t.Fatalf("Down(too-large) error = %v, want range validation error", err)
	}
}
