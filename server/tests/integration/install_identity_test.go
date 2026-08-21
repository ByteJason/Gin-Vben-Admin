package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	installer "example.com/gin-vben-admin/server/internal/application/installer"
	"example.com/gin-vben-admin/server/internal/platform/installplatform"
	"example.com/gin-vben-admin/server/internal/platform/migration"
)

func TestInstallerIdentityTransactionIntegration(t *testing.T) {
	if integrationDisabled() {
		t.Skip("set DATA_PLATFORM_INTEGRATION=1 to run installer identity integration")
	}
	for _, item := range []struct {
		driver string
		dsn    string
	}{
		{driver: migration.DriverMySQL, dsn: requiredEnv(t, mysqlDSNEnv)},
		{driver: migration.DriverPostgres, dsn: requiredEnv(t, postgresDSNEnv)},
	} {
		t.Run(item.driver, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			runner, err := migration.New(item.driver, item.dsn)
			if err != nil {
				t.Fatalf("migration.New() error = %v", err)
			}
			defer runner.Close()
			if err := runner.Up(); err != nil {
				t.Fatalf("migration.Up() error = %v", err)
			}

			database := installer.DatabaseConnection{Driver: item.driver, Mode: "single", DSN: item.dsn}
			reference := fmt.Sprintf("it-install-%s-%d", item.driver, time.Now().UnixNano())
			username := fmt.Sprintf("it_install_admin_%s_%d", item.driver, time.Now().UnixNano())
			initializeAndClose := func() {
				store, err := installplatform.NewGORMIdentityStore(database)
				if err != nil {
					t.Fatalf("NewGORMIdentityStore() error = %v", err)
				}
				if err := store.Initialize(ctx, reference, username, "$2a$12$integration-fixture-hash"); err != nil {
					_ = store.Close()
					t.Fatalf("Initialize() error = %v", err)
				}
				if err := store.Close(); err != nil {
					t.Fatalf("Close() after initialize error = %v", err)
				}
			}
			rollbackAndClose := func() {
				store, err := installplatform.NewGORMIdentityStore(database)
				if err != nil {
					t.Fatalf("NewGORMIdentityStore() for rollback error = %v", err)
				}
				if err := store.Rollback(ctx, reference); err != nil {
					_ = store.Close()
					t.Fatalf("Rollback() error = %v", err)
				}
				if err := store.Close(); err != nil {
					t.Fatalf("Close() after rollback error = %v", err)
				}
			}
			t.Cleanup(func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cleanupCancel()
				store, openErr := installplatform.NewGORMIdentityStore(database)
				if openErr == nil {
					_ = store.Rollback(cleanupCtx, reference)
					_ = store.Close()
				}
			})

			initializeAndClose()
			rollbackAndClose()
			initializeAndClose()
			rollbackAndClose()
		})
	}
}
