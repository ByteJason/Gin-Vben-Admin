package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	rediscache "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/cache/redis"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/migration"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
)

// TestHATopologyRoundTrip is opt-in because it connects to the isolated B8
// compose project. It verifies the runtime seams, not production failover
// guarantees; destructive fault injection is kept in the runbook.
func TestHATopologyRoundTrip(t *testing.T) {
	if os.Getenv("B8_HA_INTEGRATION") != "1" {
		t.Skip("set B8_HA_INTEGRATION=1 to run isolated HA topology checks")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	addressMap := requiredAddressMap(t)

	t.Run("mysql-read-write", func(t *testing.T) {
		testReadWriteTopology(t, ctx, "mysql", requiredHAEnv(t, "B8_MYSQL_PRIMARY_DSN"), requiredHAEnv(t, "B8_MYSQL_REPLICA_DSN"))
	})
	t.Run("postgres-read-write", func(t *testing.T) {
		testReadWriteTopology(t, ctx, "postgres", requiredHAEnv(t, "B8_POSTGRES_PRIMARY_DSN"), requiredHAEnv(t, "B8_POSTGRES_REPLICA_DSN"))
	})
	t.Run("redis-sentinel", func(t *testing.T) {
		testRedisTopology(t, ctx, rediscache.Config{
			Mode:       rediscache.ModeSentinel,
			Addrs:      []string{requiredHAEnv(t, "B8_REDIS_SENTINEL_A"), requiredHAEnv(t, "B8_REDIS_SENTINEL_B"), requiredHAEnv(t, "B8_REDIS_SENTINEL_C")},
			MasterName: requiredHAEnv(t, "B8_REDIS_SENTINEL_MASTER"),
			AddressMap: addressMap,
			Namespace:  "app:v1",
		}, "sentinel")
	})
	t.Run("redis-cluster", func(t *testing.T) {
		testRedisTopology(t, ctx, rediscache.Config{
			Mode:       rediscache.ModeCluster,
			Addrs:      []string{requiredHAEnv(t, "B8_REDIS_CLUSTER_A"), requiredHAEnv(t, "B8_REDIS_CLUSTER_B"), requiredHAEnv(t, "B8_REDIS_CLUSTER_C"), requiredHAEnv(t, "B8_REDIS_CLUSTER_D"), requiredHAEnv(t, "B8_REDIS_CLUSTER_E"), requiredHAEnv(t, "B8_REDIS_CLUSTER_F")},
			AddressMap: addressMap,
			Namespace:  "app:v1",
		}, "cluster")
	})
}

func testReadWriteTopology(t *testing.T, ctx context.Context, driver, primaryDSN, replicaDSN string) {
	t.Helper()
	runner, err := migration.New(driver, primaryDSN)
	if err != nil {
		t.Fatalf("open %s migration runner: %v", driver, err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	if err := runner.Up(); err != nil {
		t.Fatalf("migrate %s primary: %v", driver, err)
	}
	store, err := gormdb.Open(gormdb.Options{
		Driver:      driver,
		Mode:        gormdb.ModeReadWrite,
		PrimaryDSN:  primaryDSN,
		ReplicaDSNs: []string{replicaDSN},
		ReadPolicy:  gormdb.ReadPolicyRoundRobin,
	})
	if err != nil {
		t.Fatalf("open %s read/write store: %v", driver, err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Ping(ctx); err != nil {
		t.Fatalf("ping %s primary+replica: %v", driver, err)
	}

	key := fmt.Sprintf("ha-%s-%d", driver, time.Now().UnixNano())
	payload := map[string]any{"driver": driver, "topology": "read_write"}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = store.Write(cleanupCtx).Exec("DELETE FROM app_metadata WHERE metadata_key = ?", key).Error
	}
	cleanup()
	t.Cleanup(cleanup)
	if err := store.Write(ctx).Exec("INSERT INTO app_metadata (metadata_key, metadata_value, version) VALUES (?, ?, ?)", key, string(encoded), 1).Error; err != nil {
		t.Fatalf("write on primary %s: %v", driver, err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for {
		var count int64
		err := store.Read(ctx).Table("app_metadata").Where("metadata_key = ?", key).Count(&count).Error
		if err == nil && count == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("read from replica %s did not observe primary write: count=%d err=%v", driver, count, err)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func testRedisTopology(t *testing.T, ctx context.Context, config rediscache.Config, label string) {
	t.Helper()
	client, err := rediscache.New(config)
	if err != nil {
		t.Fatalf("open redis %s: %v", label, err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("ping redis %s: %v", label, err)
	}
	key, err := client.Key("ha", label, fmt.Sprintf("%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	_ = client.Delete(ctx, key)
	t.Cleanup(func() { _ = client.Delete(context.Background(), key) })
	want := map[string]string{"topology": label}
	if err := client.SetJSON(ctx, key, want, time.Minute); err != nil {
		t.Fatalf("set redis %s: %v", label, err)
	}
	var got map[string]string
	if err := client.GetJSON(ctx, key, &got); err != nil {
		t.Fatalf("get redis %s: %v", label, err)
	}
	if got["topology"] != label {
		t.Fatalf("redis %s value = %#v, want %#v", label, got, want)
	}
}

func requiredHAEnv(t *testing.T, name string) string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		t.Fatalf("%s must be set when B8_HA_INTEGRATION=1", name)
	}
	return value
}

func requiredAddressMap(t *testing.T) map[string]string {
	t.Helper()
	value := strings.TrimSpace(os.Getenv("B8_REDIS_ADDRESS_MAP"))
	if value == "" {
		t.Fatalf("B8_REDIS_ADDRESS_MAP must be a JSON object when B8_HA_INTEGRATION=1")
	}
	var mapping map[string]string
	if err := json.Unmarshal([]byte(value), &mapping); err != nil || len(mapping) == 0 {
		t.Fatalf("B8_REDIS_ADDRESS_MAP must be valid non-empty JSON: %v", err)
	}
	return mapping
}
