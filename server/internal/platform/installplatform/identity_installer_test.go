package installplatform

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
)

func TestIdentityInstallerHashesInitialPasswordAndUsesOpaqueRollbackReference(t *testing.T) {
	t.Parallel()

	store := &identityStoreStub{}
	var opened installer.DatabaseConnection
	factory := func(request installer.DatabaseConnection) (IdentityStore, error) {
		opened = request
		return store, nil
	}
	hasher := &passwordHasherStub{hash: "$2a$12$fixture-hash"}
	service := NewIdentityInstaller(factory, hasher, bytes.NewReader(bytes.Repeat([]byte{0x6b}, 64)))
	database := installer.DatabaseConnection{Driver: "mysql", Mode: "single", DSN: "user:database-secret@tcp(db:3306)/app"}
	receipt, err := service.Initialize(context.Background(), database, installer.AdminAccount{
		Username: "admin", Password: "initial-password-123",
	})
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	if hasher.password != "initial-password-123" || store.passwordHash != hasher.hash || store.username != "admin" {
		t.Fatalf("hasher/store inputs = %q/%q/%q", hasher.password, store.passwordHash, store.username)
	}
	if opened.DSN != database.DSN || receipt.Reference == "" || strings.Contains(receipt.Reference, "password") || strings.Contains(receipt.Reference, "secret") {
		t.Fatalf("opened/receipt = %#v/%#v", opened, receipt)
	}
	if store.closeCalls != 1 {
		t.Fatalf("Close() calls after initialize = %d, want 1", store.closeCalls)
	}
	if err := service.Rollback(context.Background(), receipt); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
	if store.rollbackReference != receipt.Reference || store.closeCalls != 2 {
		t.Fatalf("rollback reference/close = %q/%d", store.rollbackReference, store.closeCalls)
	}
	if err := service.Rollback(context.Background(), receipt); err != nil {
		t.Fatalf("second Rollback() after completed compensation error = %v", err)
	}
}

func TestInstallationUserRowStoresNormalizedUsername(t *testing.T) {
	row, err := newInstallationUserRow(" Alice ", "hash")
	if err != nil {
		t.Fatalf("newInstallationUserRow() error = %v", err)
	}
	if row.Username != "Alice" || row.UsernameNormalized == nil || *row.UsernameNormalized != "alice" || row.TenantID != initialTenantID || row.OrgID != nil {
		t.Fatalf("installation row = %+v", row)
	}
}

func TestIdentityInstallerRecoversRollbackAfterProcessRestartWithFreshConnectionInput(t *testing.T) {
	store := &identityStoreStub{}
	factory := func(installer.DatabaseConnection) (IdentityStore, error) { return store, nil }
	database := installer.DatabaseConnection{Driver: "mysql", Mode: "single", DSN: "user:fresh-secret@tcp(db:3306)/app"}
	first := NewIdentityInstaller(factory, &passwordHasherStub{hash: "$2a$12$fixture-hash"}, bytes.NewReader(bytes.Repeat([]byte{0x6c}, 64)))
	receipt, err := first.Initialize(context.Background(), database, installer.AdminAccount{Username: "admin", Password: "initial-password-123"})
	if err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}

	restarted := NewIdentityInstaller(factory, &passwordHasherStub{hash: "$2a$12$unused"}, bytes.NewReader(bytes.Repeat([]byte{0x7d}, 64)))
	if err := restarted.RecoverRollback(context.Background(), database, receipt); err != nil {
		t.Fatalf("RecoverRollback() error = %v", err)
	}
	if store.rollbackReference != receipt.Reference || store.closeCalls != 2 {
		t.Fatalf("recovered rollback reference/close = %q/%d", store.rollbackReference, store.closeCalls)
	}
}

func TestIdentityInstallerPreservesNotOwnedRecoverySentinel(t *testing.T) {
	store := &identityStoreStub{rollbackErr: installer.ErrIdentityNotOwned}
	service := NewIdentityInstaller(
		func(installer.DatabaseConnection) (IdentityStore, error) { return store, nil },
		&passwordHasherStub{hash: "$2a$12$fixture-hash"},
		bytes.NewReader(bytes.Repeat([]byte{0x7e}, 64)),
	)
	err := service.RecoverRollback(
		context.Background(),
		installer.DatabaseConnection{Driver: "mysql", Mode: "single", DSN: "user:secret@tcp(db:3306)/app"},
		installer.IdentityReceipt{Reference: "install-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
	)
	if !errors.Is(err, installer.ErrIdentityNotOwned) {
		t.Fatalf("RecoverRollback() error=%v, want ErrIdentityNotOwned", err)
	}
}

func TestIdentityInstallerPreservesSafeInitializationDiagnostic(t *testing.T) {
	cause := &navigationSeedConflictError{resourceKind: "permission", resourceID: "iam:users:read"}
	store := &identityStoreStub{initializeErr: cause}
	service := NewIdentityInstaller(
		func(installer.DatabaseConnection) (IdentityStore, error) { return store, nil },
		&passwordHasherStub{hash: "$2a$12$fixture-hash"},
		bytes.NewReader(bytes.Repeat([]byte{0x7f}, 64)),
	)
	receipt, err := service.InitializeWithReference(
		context.Background(),
		installer.DatabaseConnection{Driver: "mysql", Mode: "single", DSN: "user:secret@tcp(db:3306)/app"},
		installer.AdminAccount{Username: "admin", Password: "initial-password-123"},
		"install-cccccccccccccccccccccccccccccccc",
	)
	if err == nil || receipt.Reference == "" {
		t.Fatalf("InitializeWithReference() receipt=%#v error=%v", receipt, err)
	}
	var provider installer.FailureDiagnosticProvider
	if !errors.As(err, &provider) {
		t.Fatalf("InitializeWithReference() error=%T %v, want diagnostic provider", err, err)
	}
	got := provider.InstallationFailureDiagnostic()
	if got.Reason != "navigation_seed_conflict" || got.Operation != "apply" ||
		got.ResourceKind != "permission" || got.ResourceID != "iam:users:read" {
		t.Fatalf("diagnostic=%#v", got)
	}
}

func TestIdentityInstallerUsesCallerPreparedRecoveryReference(t *testing.T) {
	store := &identityStoreStub{}
	service := NewIdentityInstaller(
		func(installer.DatabaseConnection) (IdentityStore, error) { return store, nil },
		&passwordHasherStub{hash: "$2a$12$fixture-hash"},
		bytes.NewReader(bytes.Repeat([]byte{0x5a}, 64)),
	)
	reference := "install-0123456789abcdef0123456789abcdef"
	receipt, err := service.InitializeWithReference(
		context.Background(),
		installer.DatabaseConnection{Driver: "mysql", Mode: "single", DSN: "user:secret@tcp(db:3306)/app"},
		installer.AdminAccount{Username: "admin", Password: "initial-password-123"},
		reference,
	)
	if err != nil {
		t.Fatalf("InitializeWithReference() error = %v", err)
	}
	if receipt.Reference != reference || store.rollbackReference != reference {
		t.Fatalf("prepared identity receipt/store reference = %q/%q", receipt.Reference, store.rollbackReference)
	}
}

func TestIdentityInstallerFinalizeForgetsInMemoryDatabaseCredentials(t *testing.T) {
	service := NewIdentityInstaller(
		func(installer.DatabaseConnection) (IdentityStore, error) { return &identityStoreStub{}, nil },
		&passwordHasherStub{hash: "$2a$12$fixture-hash"},
		bytes.NewReader(bytes.Repeat([]byte{0x4e}, 64)),
	)
	database := installer.DatabaseConnection{Driver: "mysql", Mode: "single", DSN: "user:database-secret@tcp(db:3306)/app"}
	receipt, err := service.InitializeWithReference(
		context.Background(), database,
		installer.AdminAccount{Username: "admin", Password: "initial-password-123"},
		"install-33333333333333333333333333333333",
	)
	if err != nil {
		t.Fatalf("InitializeWithReference() error = %v", err)
	}
	if len(service.pending) != 1 {
		t.Fatalf("pending credentials before finalize = %d, want 1", len(service.pending))
	}
	if err := service.Finalize(context.Background(), receipt); err != nil {
		t.Fatalf("Finalize() error = %v", err)
	}
	if len(service.pending) != 0 {
		t.Fatalf("pending credentials after finalize = %#v", service.pending)
	}
	if err := service.Finalize(context.Background(), receipt); err != nil {
		t.Fatalf("second Finalize() error = %v", err)
	}
}

func TestIdentityInstallerRecoverRollbackForgetsCredentialsAfterInitializeCloseError(t *testing.T) {
	store := &identityStoreStub{closeErrors: map[int]error{1: errors.New("close after initialize failed")}}
	service := NewIdentityInstaller(
		func(installer.DatabaseConnection) (IdentityStore, error) { return store, nil },
		&passwordHasherStub{hash: "$2a$12$fixture-hash"},
		bytes.NewReader(bytes.Repeat([]byte{0x5f}, 64)),
	)
	database := installer.DatabaseConnection{Driver: "mysql", Mode: "single", DSN: "user:database-secret@tcp(db:3306)/app"}
	receipt, err := service.InitializeWithReference(
		context.Background(), database,
		installer.AdminAccount{Username: "admin", Password: "initial-password-123"},
		"install-44444444444444444444444444444444",
	)
	if err == nil || receipt.Reference == "" || len(service.pending) != 1 {
		t.Fatalf("InitializeWithReference() receipt=%#v error=%v pending=%d", receipt, err, len(service.pending))
	}
	if err := service.RecoverRollback(context.Background(), database, receipt); err != nil {
		t.Fatalf("RecoverRollback() error = %v", err)
	}
	if len(service.pending) != 0 {
		t.Fatalf("database credentials remain after recovered compensation: %#v", service.pending)
	}
}

type passwordHasherStub struct {
	password string
	hash     string
}

func (s *passwordHasherStub) Hash(password string) (string, error) {
	s.password = password
	return s.hash, nil
}

type identityStoreStub struct {
	username          string
	passwordHash      string
	rollbackReference string
	closeCalls        int
	closeErrors       map[int]error
	rollbackErr       error
	initializeErr     error
}

func (s *identityStoreStub) Initialize(_ context.Context, reference, username, passwordHash string) error {
	s.rollbackReference = reference
	s.username = username
	s.passwordHash = passwordHash
	return s.initializeErr
}

func (s *identityStoreStub) Rollback(_ context.Context, reference string) error {
	s.rollbackReference = reference
	return s.rollbackErr
}

func (s *identityStoreStub) Close() error {
	s.closeCalls++
	if s.closeErrors != nil {
		return s.closeErrors[s.closeCalls]
	}
	return nil
}
