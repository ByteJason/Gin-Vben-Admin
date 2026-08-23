package integration

import (
	"context"
	"os"
	"testing"
	"time"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/installplatform"
)

func TestInstallerDependencyChecksIntegration(t *testing.T) {
	if integrationDisabled() {
		t.Skip("set DATA_PLATFORM_INTEGRATION=1 to run installer connection probes")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	probe := installplatform.NewSystemDependencyProbe()
	service := installer.NewDependencyCheckService(probe, probe)

	for _, item := range []struct {
		driver string
		dsn    string
	}{
		{driver: "mysql", dsn: requiredEnv(t, mysqlDSNEnv)},
		{driver: "postgres", dsn: requiredEnv(t, postgresDSNEnv)},
	} {
		result, err := service.CheckDatabase(ctx, installer.DatabaseConnection{Driver: item.driver, Mode: "single", DSN: item.dsn})
		if err != nil {
			t.Fatalf("%s connection check error = %v", item.driver, err)
		}
		if !result.OK || result.Kind != "database" || result.Driver != item.driver || result.Reason != "reachable" || result.Message != "" {
			t.Fatalf("%s safe result = %#v", item.driver, result)
		}
	}

	result, err := service.CheckRedis(ctx, installer.RedisConnection{Mode: "single", Addr: requiredEnv(t, redisAddrEnv)})
	if err != nil {
		t.Fatalf("redis connection check error = %v", err)
	}
	if !result.OK || result.Kind != "redis" || result.Reason != "reachable" || result.Message != "" {
		t.Fatalf("redis safe result = %#v", result)
	}
}

func integrationDisabled() bool {
	return testing.Short() || os.Getenv("DATA_PLATFORM_INTEGRATION") != "1"
}
