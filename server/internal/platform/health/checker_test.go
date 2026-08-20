package health

import (
	"context"
	"errors"
	"testing"
	"time"
)

type dependencyStub struct {
	name string
	err  error
}

func (d dependencyStub) Name() string { return d.name }

func (d dependencyStub) Ping(context.Context) error { return d.err }

func TestCheckerReportsReadyOnlyWhenEveryDependencyIsUp(t *testing.T) {
	checker := NewChecker(time.Second,
		dependencyStub{name: "database"},
		dependencyStub{name: "redis", err: errors.New("connection refused")},
	)

	result := checker.Check(context.Background())

	if result.Ready {
		t.Fatal("readiness = true, want false")
	}
	if got := result.Checks["database"]; got != StatusUp {
		t.Fatalf("database status = %q, want %q", got, StatusUp)
	}
	if got := result.Checks["redis"]; got != StatusDown {
		t.Fatalf("redis status = %q, want %q", got, StatusDown)
	}
}

func TestCheckerWithoutDependenciesIsReady(t *testing.T) {
	result := NewChecker(time.Second).Check(context.Background())
	if !result.Ready {
		t.Fatal("readiness = false, want true")
	}
	if len(result.Checks) != 0 {
		t.Fatalf("checks = %#v, want empty", result.Checks)
	}
}
