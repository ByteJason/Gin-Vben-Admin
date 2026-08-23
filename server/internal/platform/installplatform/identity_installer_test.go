package installplatform

import (
	"bytes"
	"context"
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
}

func TestInstallationUserRowStoresNormalizedUsername(t *testing.T) {
	row, err := newInstallationUserRow(" Alice ", "hash")
	if err != nil {
		t.Fatalf("newInstallationUserRow() error = %v", err)
	}
	if row.Username != "Alice" || row.UsernameNormalized == nil || *row.UsernameNormalized != "alice" {
		t.Fatalf("installation row = %+v", row)
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
}

func (s *identityStoreStub) Initialize(_ context.Context, reference, username, passwordHash string) error {
	s.rollbackReference = reference
	s.username = username
	s.passwordHash = passwordHash
	return nil
}

func (s *identityStoreStub) Rollback(_ context.Context, reference string) error {
	s.rollbackReference = reference
	return nil
}

func (s *identityStoreStub) Close() error {
	s.closeCalls++
	return nil
}
