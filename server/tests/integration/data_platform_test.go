// Package integration contains opt-in tests that exercise the local data
// platform containers. DATA_PLATFORM_INTEGRATION is a required second gate so
// ordinary go test ./... remains network-free.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	rediscache "example.com/gin-vben-admin/server/internal/platform/cache/redis"
	"example.com/gin-vben-admin/server/internal/platform/migration"
	"example.com/gin-vben-admin/server/internal/platform/persistence/gormdb"
	"gorm.io/gorm"
)

const (
	mysqlDSNEnv    = "TEST_MYSQL_DSN"
	postgresDSNEnv = "TEST_POSTGRES_DSN"
	redisAddrEnv   = "TEST_REDIS_ADDR"
)

func TestSingleNodeDataPlatform(t *testing.T) {
	if os.Getenv("DATA_PLATFORM_INTEGRATION") != "1" {
		t.Skip("set DATA_PLATFORM_INTEGRATION=1 to run local data-platform integration tests")
	}

	mysqlDSN := requiredEnv(t, mysqlDSNEnv)
	postgresDSN := requiredEnv(t, postgresDSNEnv)
	redisAddr := requiredEnv(t, redisAddrEnv)

	t.Run("mysql", func(t *testing.T) {
		testDatabaseLifecycle(t, migration.DriverMySQL, mysqlDSN)
	})
	t.Run("postgres", func(t *testing.T) {
		testDatabaseLifecycle(t, migration.DriverPostgres, postgresDSN)
	})
	t.Run("redis", func(t *testing.T) {
		testRedisLifecycle(t, redisAddr)
	})
}

func requiredEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("%s must be set when DATA_PLATFORM_INTEGRATION=1", name)
	}
	return value
}

func testDatabaseLifecycle(t *testing.T, driver, dsn string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	runner, err := migration.New(driver, dsn)
	if err != nil {
		t.Fatalf("migration.New() error = %v", err)
	}
	store, err := gormdb.Open(gormdb.Options{
		Driver: driver,
		Mode:   gormdb.ModeSingle,
		DSN:    dsn,
	})
	if err != nil {
		_ = runner.Close()
		t.Fatalf("gormdb.Open() error = %v", err)
	}
	// Cleanup only migration-owned state. The test database itself is never
	// dropped, and no unrelated schema/table is touched.
	t.Cleanup(func() {
		if status, statusErr := runner.Status(); statusErr == nil && status.Applied {
			_ = runner.Down(1)
		}
		_ = store.Close()
		_ = runner.Close()
	})

	if err := store.Ping(ctx); err != nil {
		t.Fatalf("store.Ping() error = %v", err)
	}

	// Up is idempotent, so an interrupted previous run can be resumed without
	// touching anything outside the migration-owned app_metadata table.
	if err := runner.Up(); err != nil {
		t.Fatalf("migration.Up() error = %v", err)
	}
	assertMigrationStatus(t, runner, 1, true)
	assertMetadataTable(t, store, ctx, true)
	assertMetadataRow(t, store, ctx, "product", true)

	commitKey := "integration-" + driver + "-commit"
	rollbackKey := "integration-" + driver + "-rollback"
	cleanupKeys := func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = store.Write(cleanupCtx).Exec("DELETE FROM app_metadata WHERE metadata_key IN (?, ?)", commitKey, rollbackKey).Error
	}
	cleanupKeys()
	t.Cleanup(cleanupKeys)

	commitPayload := map[string]any{"driver": driver, "phase": "commit"}
	if err := store.WithinTransaction(ctx, func(tx *gorm.DB) error {
		return insertMetadata(tx, commitKey, commitPayload)
	}); err != nil {
		t.Fatalf("commit transaction error = %v", err)
	}
	assertMetadataRow(t, store, ctx, commitKey, true)

	rollbackPayload := map[string]any{"driver": driver, "phase": "rollback"}
	rollbackErr := errors.New("integration rollback sentinel")
	if err := store.WithinTransaction(ctx, func(tx *gorm.DB) error {
		if err := insertMetadata(tx, rollbackKey, rollbackPayload); err != nil {
			return err
		}
		return rollbackErr
	}); !errors.Is(err, rollbackErr) {
		t.Fatalf("rollback transaction error = %v, want sentinel", err)
	}
	assertMetadataRow(t, store, ctx, rollbackKey, false)

	cleanupKeys()
	if err := runner.Down(1); err != nil {
		t.Fatalf("migration.Down(1) error = %v", err)
	}
	assertMigrationStatus(t, runner, 0, false)
	assertMetadataTable(t, store, ctx, false)

	if err := runner.Up(); err != nil {
		t.Fatalf("migration.Up() restore error = %v", err)
	}
	assertMigrationStatus(t, runner, 1, true)
	assertMetadataTable(t, store, ctx, true)
	assertMetadataRow(t, store, ctx, "product", true)
}

func insertMetadata(tx *gorm.DB, key string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode metadata payload: %w", err)
	}
	return tx.Exec(
		"INSERT INTO app_metadata (metadata_key, metadata_value, version) VALUES (?, ?, ?)",
		key,
		string(encoded),
		1,
	).Error
}

func assertMigrationStatus(t *testing.T, runner *migration.Runner, wantVersion uint, wantApplied bool) {
	t.Helper()
	status, err := runner.Status()
	if err != nil {
		t.Fatalf("migration.Status() error = %v", err)
	}
	if status.Version != wantVersion || status.Dirty || status.Applied != wantApplied {
		t.Fatalf("migration.Status() = %+v, want version=%d dirty=false applied=%t", status, wantVersion, wantApplied)
	}
}

func assertMetadataTable(t *testing.T, store *gormdb.Store, ctx context.Context, want bool) {
	t.Helper()
	got := store.Write(ctx).Migrator().HasTable("app_metadata")
	if got != want {
		t.Fatalf("app_metadata table present = %t, want %t", got, want)
	}
}

func assertMetadataRow(t *testing.T, store *gormdb.Store, ctx context.Context, key string, want bool) {
	t.Helper()
	var count int64
	if err := store.Read(ctx).Table("app_metadata").Where("metadata_key = ?", key).Count(&count).Error; err != nil {
		t.Fatalf("count metadata key %q: %v", key, err)
	}
	if (count > 0) != want {
		t.Fatalf("metadata key %q present = %t, want %t", key, count > 0, want)
	}
}

func testRedisLifecycle(t *testing.T, addr string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := rediscache.New(rediscache.Config{Mode: rediscache.ModeSingle, Addr: addr, Namespace: "app:v1"})
	if err != nil {
		t.Fatalf("redis.New() error = %v", err)
	}
	// Register Close first so later key/lock cleanup runs while the client is
	// still usable (testing cleanups execute in LIFO order).
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("redis.Ping() error = %v", err)
	}

	key, err := client.Key("test", "integration-json")
	if err != nil {
		t.Fatalf("redis.Key() error = %v", err)
	}
	if err := client.Delete(ctx, key); err != nil {
		t.Fatalf("redis initial Delete() error = %v", err)
	}
	t.Cleanup(func() { _ = client.Delete(context.Background(), key) })

	var missing map[string]any
	if err := client.GetJSON(ctx, key, &missing); !errors.Is(err, rediscache.ErrCacheMiss) {
		t.Fatalf("redis missing GetJSON() error = %v, want ErrCacheMiss", err)
	}
	want := map[string]any{"driver": "redis", "phase": "integration"}
	if err := client.SetJSON(ctx, key, want, time.Minute); err != nil {
		t.Fatalf("redis SetJSON() error = %v", err)
	}
	var got map[string]any
	if err := client.GetJSON(ctx, key, &got); err != nil {
		t.Fatalf("redis GetJSON() error = %v", err)
	}
	if got["driver"] != want["driver"] || got["phase"] != want["phase"] {
		t.Fatalf("redis GetJSON() = %#v, want %#v", got, want)
	}

	lock, err := client.AcquireLock(ctx, "test-integration-lock", time.Minute)
	if err != nil {
		t.Fatalf("redis AcquireLock() error = %v", err)
	}
	t.Cleanup(func() { _ = lock.Release(context.Background()) })
	if _, err := client.AcquireLock(ctx, "test-integration-lock", time.Minute); !errors.Is(err, rediscache.ErrLockNotAcquired) {
		t.Fatalf("second redis AcquireLock() error = %v, want ErrLockNotAcquired", err)
	}
	if err := lock.Release(ctx); err != nil {
		t.Fatalf("redis lock Release() error = %v", err)
	}
}
