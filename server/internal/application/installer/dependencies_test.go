package installer

import (
	"context"
	"errors"
	"testing"
)

func TestDependencyCheckServiceReturnsSafeDatabaseResult(t *testing.T) {
	db := databaseProbeStub{result: DependencyCheck{Kind: "database", Driver: "mysql", Mode: "single", OK: true, Reason: "reachable"}}
	service := NewDependencyCheckService(&db, redisProbeStub{})
	result, err := service.CheckDatabase(context.Background(), DatabaseConnection{Driver: "mysql", Mode: "single", Host: "db", Port: 3306, Database: "app", Username: "root", Password: "secret"})
	if err != nil {
		t.Fatalf("CheckDatabase() error = %v", err)
	}
	if !result.OK || result.Kind != "database" || result.Driver != "mysql" {
		t.Fatalf("result = %#v", result)
	}
	if db.request.Password != "secret" {
		t.Fatalf("probe request password was not passed to connector")
	}
}

func TestDependencyCheckServiceRejectsUnsupportedDatabaseAndRedisModes(t *testing.T) {
	service := NewDependencyCheckService(&databaseProbeStub{}, redisProbeStub{})
	if _, err := service.CheckDatabase(context.Background(), DatabaseConnection{Driver: "sqlite", Mode: "single"}); err == nil {
		t.Fatal("unsupported database driver error = nil")
	}
	if _, err := service.CheckRedis(context.Background(), RedisConnection{Mode: "cluster", Addrs: []string{"redis-a"}}); err == nil {
		t.Fatal("incomplete redis cluster error = nil")
	}
}

func TestDependencyCheckServiceMapsProbeFailureWithoutExposingCause(t *testing.T) {
	service := NewDependencyCheckService(&databaseProbeStub{err: errors.New("dsn=postgres://user:secret@host/app")}, redisProbeStub{})
	result, err := service.CheckDatabase(context.Background(), DatabaseConnection{Driver: "postgres", Mode: "single", DSN: "postgres://user:secret@host/app"})
	if err != nil {
		t.Fatalf("CheckDatabase() error = %v", err)
	}
	if result.OK || result.Reason != "connection_failed" {
		t.Fatalf("result = %#v", result)
	}
	if result.Message != "" {
		t.Fatalf("result contains unsafe details: %#v", result)
	}
}

func TestDependencyCheckServiceHonorsCancellation(t *testing.T) {
	service := NewDependencyCheckService(&databaseProbeStub{}, redisProbeStub{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.CheckRedis(ctx, RedisConnection{Mode: "single", Addr: "redis:6379"}); err == nil {
		t.Fatal("canceled CheckRedis() error = nil")
	}
}

type databaseProbeStub struct {
	request DatabaseConnection
	result  DependencyCheck
	err     error
}

func (s *databaseProbeStub) CheckDatabase(_ context.Context, request DatabaseConnection) (DependencyCheck, error) {
	s.request = request
	if s.err != nil {
		return DependencyCheck{}, s.err
	}
	return s.result, nil
}

type redisProbeStub struct {
	result DependencyCheck
	err    error
}

func (s redisProbeStub) CheckRedis(context.Context, RedisConnection) (DependencyCheck, error) {
	if s.err != nil {
		return DependencyCheck{}, s.err
	}
	return s.result, nil
}
