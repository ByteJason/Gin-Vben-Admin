package installer

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type initialAdminPasswordHasherStub struct {
	hash     string
	err      error
	password string
}

func (s *initialAdminPasswordHasherStub) Hash(password string) (string, error) {
	s.password = password
	return s.hash, s.err
}

type initialAdminPasswordStoreStub struct {
	identifier        string
	identifierErr     error
	resetErr          error
	passwordHash      string
	resetIdentifier   string
	identifierCalls   int
	resetCalls        int
	operationSequence *[]string
}

func (s *initialAdminPasswordStoreStub) InitialAdminIdentifier(context.Context) (string, error) {
	s.identifierCalls++
	return s.identifier, s.identifierErr
}

func (s *initialAdminPasswordStoreStub) ResetInitialAdminPassword(_ context.Context, identifier, passwordHash string) error {
	s.resetCalls++
	s.resetIdentifier = identifier
	s.passwordHash = passwordHash
	if s.operationSequence != nil {
		*s.operationSequence = append(*s.operationSequence, "database")
	}
	return s.resetErr
}

type loginAttemptResetterStub struct {
	identifier        string
	err               error
	calls             int
	operationSequence *[]string
}

func (s *loginAttemptResetterStub) Reset(_ context.Context, identifier string) error {
	s.calls++
	s.identifier = identifier
	if s.operationSequence != nil {
		*s.operationSequence = append(*s.operationSequence, "redis")
	}
	return s.err
}

func TestInitialAdminPasswordResetUsesExactPasswordAndClearsLoginFailures(t *testing.T) {
	hasher := &initialAdminPasswordHasherStub{hash: "$2a$12$fixture"}
	sequence := make([]string, 0, 2)
	store := &initialAdminPasswordStoreStub{identifier: "admin", operationSequence: &sequence}
	attempts := &loginAttemptResetterStub{operationSequence: &sequence}
	service := NewInitialAdminPasswordResetService(store, hasher, attempts)

	if err := service.Reset(context.Background(), "Abc123"); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	if hasher.password != "Abc123" {
		t.Fatalf("hasher password = %q, want exact input", hasher.password)
	}
	if store.identifierCalls != 1 || store.resetCalls != 1 || store.resetIdentifier != "admin" || store.passwordHash != hasher.hash {
		t.Fatalf("store resolution/reset/identifier/hash = %d/%d/%q/%q", store.identifierCalls, store.resetCalls, store.resetIdentifier, store.passwordHash)
	}
	if attempts.calls != 1 || attempts.identifier != "admin" {
		t.Fatalf("attempt reset calls/identifier = %d/%q", attempts.calls, attempts.identifier)
	}
	if strings.Join(sequence, ",") != "redis,database" {
		t.Fatalf("mutation order = %v, want Redis before database", sequence)
	}
}

func TestInitialAdminPasswordResetRejectsInvalidPolicyBeforeHashing(t *testing.T) {
	for name, password := range map[string]string{
		"too short":    "Abc12",
		"letters only": "Abcdef",
		"digits only":  "123456",
		"symbol":       "Abc12!",
		"too long":     "A1" + strings.Repeat("a", 71),
	} {
		t.Run(name, func(t *testing.T) {
			hasher := &initialAdminPasswordHasherStub{hash: "unused"}
			store := &initialAdminPasswordStoreStub{identifier: "admin"}
			attempts := &loginAttemptResetterStub{}
			service := NewInitialAdminPasswordResetService(store, hasher, attempts)

			err := service.Reset(context.Background(), password)
			if !errors.Is(err, ErrInvalidInitialAdminPassword) {
				t.Fatalf("Reset() error = %v, want ErrInvalidInitialAdminPassword", err)
			}
			if hasher.password != "" || store.identifierCalls != 0 || store.resetCalls != 0 || attempts.calls != 0 {
				t.Fatalf("invalid password reached dependencies: hash=%q resolve=%d reset=%d attempts=%d", hasher.password, store.identifierCalls, store.resetCalls, attempts.calls)
			}
		})
	}
}

func TestInitialAdminPasswordResetMapsDependencyFailures(t *testing.T) {
	dependencyErr := errors.New("dependency detail")
	for name, service := range map[string]*InitialAdminPasswordResetService{
		"hash": NewInitialAdminPasswordResetService(
			&initialAdminPasswordStoreStub{identifier: "admin"},
			&initialAdminPasswordHasherStub{err: dependencyErr},
			&loginAttemptResetterStub{},
		),
		"identity": NewInitialAdminPasswordResetService(
			&initialAdminPasswordStoreStub{identifierErr: dependencyErr},
			&initialAdminPasswordHasherStub{hash: "$2a$12$fixture"},
			&loginAttemptResetterStub{},
		),
		"attempt reset": NewInitialAdminPasswordResetService(
			&initialAdminPasswordStoreStub{identifier: "admin"},
			&initialAdminPasswordHasherStub{hash: "$2a$12$fixture"},
			&loginAttemptResetterStub{err: dependencyErr},
		),
		"store": NewInitialAdminPasswordResetService(
			&initialAdminPasswordStoreStub{identifier: "admin", resetErr: dependencyErr},
			&initialAdminPasswordHasherStub{hash: "$2a$12$fixture"},
			&loginAttemptResetterStub{},
		),
	} {
		t.Run(name, func(t *testing.T) {
			err := service.Reset(context.Background(), "Abc123")
			if !errors.Is(err, ErrInitialAdminPasswordReset) || errors.Is(err, dependencyErr) {
				t.Fatalf("Reset() error = %v, want bounded reset error", err)
			}
		})
	}
}

func TestInitialAdminPasswordResetHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := NewInitialAdminPasswordResetService(
		&initialAdminPasswordStoreStub{identifier: "admin"},
		&initialAdminPasswordHasherStub{hash: "$2a$12$fixture"},
		&loginAttemptResetterStub{},
	)
	if err := service.Reset(ctx, "Abc123"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Reset() error = %v, want context.Canceled", err)
	}
}
