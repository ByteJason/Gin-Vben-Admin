package installplatform

import (
	"context"
	"errors"
	"strings"
	"testing"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	rediscache "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/cache/redis"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
)

func TestDependencyProbeBuildsStructuredMySQLAndPostgresOptions(t *testing.T) {
	mysql, err := databaseOptionsFromRequest(installer.DatabaseConnection{Driver: "mysql", Mode: "single", Host: "db.example", Port: 3306, Database: "app", Username: "admin", Password: "secret"})
	if err != nil {
		t.Fatalf("mysql options error = %v", err)
	}
	if mysql.Driver != "mysql" || !strings.Contains(mysql.DSN, "db.example:3306") || !strings.Contains(mysql.DSN, "admin") {
		t.Fatalf("mysql options = %+v", mysql)
	}
	postgres, err := databaseOptionsFromRequest(installer.DatabaseConnection{Driver: "pgsql", Mode: "single", Host: "db.example", Port: 5432, Database: "app", Username: "admin", Password: "secret", TLSMode: "disable"})
	if err != nil {
		t.Fatalf("postgres options error = %v", err)
	}
	if postgres.Driver != "postgres" || !strings.Contains(postgres.DSN, "db.example:5432") || !strings.Contains(postgres.DSN, "sslmode=disable") {
		t.Fatalf("postgres options = %+v", postgres)
	}
}

func TestDependencyProbeMapsSuccessfulFakeChecksToSafeResults(t *testing.T) {
	probe := NewDependencyProbe(
		func(gormdb.Options) (DatabasePinger, error) { return pingerStub{}, nil },
		func(rediscache.Config) (RedisPinger, error) { return redisPingerStub{}, nil },
	)
	db, err := probe.CheckDatabase(context.Background(), installer.DatabaseConnection{Driver: "mysql", Mode: "single", DSN: "user:secret@tcp(db:3306)/app"})
	if err != nil || !db.OK || db.Kind != "database" || db.Reason != "reachable" || db.Message != "" {
		t.Fatalf("database result = %#v, err=%v", db, err)
	}
	redis, err := probe.CheckRedis(context.Background(), installer.RedisConnection{Mode: "single", Addr: "redis:6379"})
	if err != nil || !redis.OK || redis.Kind != "redis" || redis.Reason != "reachable" {
		t.Fatalf("redis result = %#v, err=%v", redis, err)
	}
}

func TestDependencyProbeDoesNotExposeOpenOrPingErrors(t *testing.T) {
	probe := NewDependencyProbe(
		func(gormdb.Options) (DatabasePinger, error) {
			return nil, errors.New("postgres://user:secret@host/app")
		},
		func(rediscache.Config) (RedisPinger, error) { return nil, errors.New("redis password=secret") },
	)
	db, err := probe.CheckDatabase(context.Background(), installer.DatabaseConnection{Driver: "postgres", Mode: "single", DSN: "postgres://user:secret@host/app"})
	if err == nil || db.OK || strings.Contains(strings.ToLower(err.Error()), "secret") {
		t.Fatalf("database error = %v, result=%#v; want internal error for application sanitizer", err, db)
	}
	redis, err := probe.CheckRedis(context.Background(), installer.RedisConnection{Mode: "single", Addr: "redis:6379"})
	if err == nil || redis.OK || strings.Contains(strings.ToLower(err.Error()), "secret") {
		t.Fatalf("redis error = %v, result=%#v", err, redis)
	}
}

func TestDependencyProbeHonorsCanceledContext(t *testing.T) {
	called := false
	probe := NewDependencyProbe(
		func(gormdb.Options) (DatabasePinger, error) { called = true; return pingerStub{}, nil },
		func(rediscache.Config) (RedisPinger, error) { called = true; return redisPingerStub{}, nil },
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := probe.CheckDatabase(ctx, installer.DatabaseConnection{Driver: "mysql", Mode: "single", DSN: "user:pass@tcp(db:3306)/app"})
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("canceled probe error=%v called=%v", err, called)
	}
}

type pingerStub struct{}

func (pingerStub) Ping(context.Context) error { return nil }
func (pingerStub) Close() error               { return nil }

type redisPingerStub struct{}

func (redisPingerStub) Ping(context.Context) error { return nil }
func (redisPingerStub) Close() error               { return nil }
