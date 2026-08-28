package integration

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	appauth "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/auth"
	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/authplatform"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/installplatform"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/migration"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
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

func TestInstalledIdentityAuthenticatesAcrossRuntimeStoreIntegration(t *testing.T) {
	if integrationDisabled() {
		t.Skip("set DATA_PLATFORM_INTEGRATION=1 to run installed identity authentication integration")
	}
	for _, item := range []struct {
		driver string
		dsn    string
	}{
		{driver: migration.DriverMySQL, dsn: requiredEnv(t, mysqlDSNEnv)},
		{driver: migration.DriverPostgres, dsn: requiredEnv(t, postgresDSNEnv)},
	} {
		t.Run(item.driver, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
			suffix := time.Now().UnixNano()
			reference := fmt.Sprintf("it-auth-boundary-%s-%d", item.driver, suffix)
			username := fmt.Sprintf("it_auth_boundary_%s_%d", item.driver, suffix)
			password := "Abc123"
			identity := installplatform.NewSystemIdentityInstaller()
			receipt, err := identity.InitializeWithReference(ctx, database, installer.AdminAccount{
				Username: username,
				Password: password,
			}, reference)
			if err != nil {
				t.Fatalf("InitializeWithReference() error = %v", err)
			}
			t.Cleanup(func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cleanupCancel()
				if rollbackErr := identity.Rollback(cleanupCtx, receipt); rollbackErr != nil {
					t.Errorf("cleanup identity rollback error = %v", rollbackErr)
				}
			})

			runtimeStore, err := gormdb.Open(gormdb.Options{
				Driver: item.driver,
				Mode:   gormdb.ModeSingle,
				DSN:    item.dsn,
			})
			if err != nil {
				t.Fatalf("gormdb.Open(runtime) error = %v", err)
			}
			t.Cleanup(func() {
				if closeErr := runtimeStore.Close(); closeErr != nil {
					t.Errorf("runtime store close error = %v", closeErr)
				}
			})

			scope, err := tenant.NewContext("default", "", false)
			if err != nil {
				t.Fatalf("tenant.NewContext() error = %v", err)
			}
			loginCtx := tenant.WithContext(ctx, scope)
			users := authplatform.NewGORMUserRepository(runtimeStore)
			service := appauth.NewService(
				users,
				authplatform.BcryptHasher{Cost: 12},
				authplatform.NewJWTService([]byte("installed-identity-integration-secret"), time.Minute, time.Hour),
				authplatform.NewMemorySessionStore(),
			)

			pair, err := service.Login(loginCtx, username, password)
			if err != nil {
				t.Fatalf("Login(installed password) error = %v", err)
			}
			if pair.AccessToken == "" || pair.RefreshToken == "" {
				t.Fatalf("Login(installed password) token pair = %#v, want non-empty tokens", pair)
			}
			if _, err := service.Login(loginCtx, username, "Abc124"); !errors.Is(err, authdomain.ErrInvalidCredentials) {
				t.Fatalf("Login(wrong password) error = %v, want ErrInvalidCredentials", err)
			}

			attempts := &integrationLoginAttemptResetter{}
			reset := installer.NewInitialAdminPasswordResetService(
				installplatform.NewGORMInitialAdminPasswordStore(runtimeStore),
				authplatform.BcryptHasher{Cost: 12},
				attempts,
			)
			if err := reset.Reset(loginCtx, "Def456"); err != nil {
				t.Fatalf("Reset(initial administrator password) error = %v", err)
			}
			if attempts.identifier != username {
				t.Fatalf("login attempt reset identifier = %q, want %q", attempts.identifier, username)
			}
			if _, err := service.Login(loginCtx, username, password); !errors.Is(err, authdomain.ErrInvalidCredentials) {
				t.Fatalf("Login(old password after reset) error = %v, want ErrInvalidCredentials", err)
			}
			pair, err = service.Login(loginCtx, username, "Def456")
			if err != nil {
				t.Fatalf("Login(reset password) error = %v", err)
			}
			if pair.AccessToken == "" || pair.RefreshToken == "" {
				t.Fatalf("Login(reset password) token pair = %#v, want non-empty tokens", pair)
			}

			if err := identity.Rollback(ctx, receipt); err != nil {
				t.Fatalf("Rollback() error = %v", err)
			}
			if _, err := users.FindByIdentifier(loginCtx, username); !errors.Is(err, authdomain.ErrInvalidCredentials) {
				t.Fatalf("FindByIdentifier() after rollback error = %v, want ErrInvalidCredentials", err)
			}
		})
	}
}

type integrationLoginAttemptResetter struct {
	identifier string
}

func (s *integrationLoginAttemptResetter) Reset(_ context.Context, identifier string) error {
	s.identifier = identifier
	return nil
}
