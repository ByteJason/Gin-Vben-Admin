package installplatform

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type initialAdminTransactionStoreStub struct {
	database *gorm.DB
	calls    int
	err      error
}

func (s *initialAdminTransactionStoreStub) WithinTransaction(_ context.Context, operation func(*gorm.DB) error) error {
	s.calls++
	s.err = operation(s.database)
	return s.err
}

func TestGORMInitialAdminPasswordStoreResetsOnlyInstalledIdentity(t *testing.T) {
	database, mock := newInitialAdminPasswordSQLMock(t)
	metadata, err := json.Marshal(installationIdentityMetadata{
		InstallationID: "install-fixture", State: "installed", UserID: 17,
		Username: "Admin", RoleID: installationRoleID,
	})
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "app_metadata" WHERE metadata_key = $1 LIMIT $2 FOR UPDATE`)).
		WithArgs(installationMetadataKey, 1).
		WillReturnRows(sqlmock.NewRows([]string{"metadata_key", "metadata_value", "version"}).
			AddRow(installationMetadataKey, metadata, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "users" WHERE tenant_id = $1 AND id = $2 AND username = $3 LIMIT $4 FOR UPDATE`)).
		WithArgs(initialTenantID, uint64(17), "Admin", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "org_id", "username", "username_normalized", "password_hash", "status", "must_change_password",
		}).AddRow(17, initialTenantID, nil, "Admin", "admin", "$2a$12$old", "active", true))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "app_metadata" WHERE metadata_key = $1 LIMIT $2 FOR UPDATE`)).
		WithArgs(installationMetadataKey, 1).
		WillReturnRows(sqlmock.NewRows([]string{"metadata_key", "metadata_value", "version"}).
			AddRow(installationMetadataKey, metadata, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "users" WHERE tenant_id = $1 AND id = $2 AND username = $3 LIMIT $4 FOR UPDATE`)).
		WithArgs(initialTenantID, uint64(17), "Admin", 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "tenant_id", "org_id", "username", "username_normalized", "password_hash", "status", "must_change_password",
		}).AddRow(17, initialTenantID, nil, "Admin", "admin", "$2a$12$old", "active", true))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "users" SET "failed_attempts"=$1,"locked_until"=$2,"must_change_password"=$3,"password_changed_at"=$4,"password_hash"=$5 WHERE tenant_id = $6 AND id = $7 AND username = $8 AND status = $9`)).
		WithArgs(0, nil, false, sqlmock.AnyArg(), "$2a$12$new", initialTenantID, uint64(17), "Admin", "active").
		WillReturnResult(sqlmock.NewResult(0, 1))

	transaction := &initialAdminTransactionStoreStub{database: database}
	store := &GORMInitialAdminPasswordStore{database: transaction}
	identifier, err := store.InitialAdminIdentifier(context.Background())
	if err != nil {
		t.Fatalf("InitialAdminIdentifier() error = %v (transaction: %v)", err, transaction.err)
	}
	if identifier != "admin" {
		t.Fatalf("identifier = %q, want admin", identifier)
	}
	if err := store.ResetInitialAdminPassword(context.Background(), identifier, "$2a$12$new"); err != nil {
		t.Fatalf("ResetInitialAdminPassword() error = %v (transaction: %v)", err, transaction.err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGORMInitialAdminPasswordStoreRejectsIncompleteReceipt(t *testing.T) {
	database, mock := newInitialAdminPasswordSQLMock(t)
	metadata, err := json.Marshal(installationIdentityMetadata{
		InstallationID: "install-fixture", State: "initializing", UserID: 17,
		Username: "Admin", RoleID: installationRoleID,
	})
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "app_metadata" WHERE metadata_key = $1 LIMIT $2 FOR UPDATE`)).
		WithArgs(installationMetadataKey, 1).
		WillReturnRows(sqlmock.NewRows([]string{"metadata_key", "metadata_value", "version"}).
			AddRow(installationMetadataKey, metadata, 1))

	store := &GORMInitialAdminPasswordStore{database: &initialAdminTransactionStoreStub{database: database}}
	if _, err := store.InitialAdminIdentifier(context.Background()); !errors.Is(err, ErrInitialAdminPasswordReset) {
		t.Fatalf("InitialAdminIdentifier() error = %v, want ErrInitialAdminPasswordReset", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestGORMInitialAdminPasswordStoreHonorsCanceledContextBeforeDatabase(t *testing.T) {
	database := &initialAdminTransactionStoreStub{}
	store := &GORMInitialAdminPasswordStore{database: database}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.InitialAdminIdentifier(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("InitialAdminIdentifier() error = %v, want context.Canceled", err)
	}
	if database.calls != 0 {
		t.Fatalf("database calls = %d, want 0", database.calls)
	}
}

func newInitialAdminPasswordSQLMock(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDatabase, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDatabase.Close() })
	database, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDatabase}), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return database, mock
}
