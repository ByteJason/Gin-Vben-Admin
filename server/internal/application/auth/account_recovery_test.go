package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"example.com/gin-vben-admin/server/internal/application/auth"
	"example.com/gin-vben-admin/server/internal/domain/authdomain"
	"example.com/gin-vben-admin/server/internal/platform/authplatform"
)

type accountProvisioner struct {
	created     authdomain.User
	updatedUser string
	updatedHash string
	createErr   error
	updateErr   error
}

func (p *accountProvisioner) CreateUser(_ context.Context, user authdomain.User) error {
	p.created = user
	return p.createErr
}

func (p *accountProvisioner) UpdatePassword(_ context.Context, identifier, passwordHash string) error {
	p.updatedUser = identifier
	p.updatedHash = passwordHash
	return p.updateErr
}

type passwordResetProvider struct {
	requestedFor string
	consumeToken string
	identifier   string
	requestErr   error
	consumeErr   error
}

func (p *passwordResetProvider) Request(_ context.Context, identifier string) error {
	p.requestedFor = identifier
	return p.requestErr
}

func (p *passwordResetProvider) Consume(_ context.Context, token string) (string, error) {
	p.consumeToken = token
	return p.identifier, p.consumeErr
}

func TestRegisterHashesPasswordBeforeCreatingActiveUser(t *testing.T) {
	hasher := authplatform.BcryptHasher{Cost: 4}
	provisioner := &accountProvisioner{}
	svc := auth.NewService(userRepo{}, hasher, authplatform.NewJWTService([]byte("test-secret"), time.Minute, time.Hour), authplatform.NewMemorySessionStore())
	svc.SetAccountProvisioner(provisioner)

	if err := svc.Register(context.Background(), " alice ", "correct-password"); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if provisioner.created.Identifier != "alice" || !provisioner.created.Active {
		t.Fatalf("created user = %+v", provisioner.created)
	}
	if provisioner.created.PasswordHash == "correct-password" || hasher.Compare(provisioner.created.PasswordHash, "correct-password") != nil {
		t.Fatal("Register() did not persist a bcrypt password hash")
	}
}

func TestRegisterPreservesDuplicateUserConflict(t *testing.T) {
	provisioner := &accountProvisioner{createErr: authdomain.ErrUserAlreadyExists}
	svc := auth.NewService(userRepo{}, authplatform.BcryptHasher{Cost: 4}, authplatform.NewJWTService([]byte("test-secret"), time.Minute, time.Hour), authplatform.NewMemorySessionStore())
	svc.SetAccountProvisioner(provisioner)

	if err := svc.Register(context.Background(), "alice", "correct-password"); !errors.Is(err, authdomain.ErrUserAlreadyExists) {
		t.Fatalf("Register() error = %v, want ErrUserAlreadyExists", err)
	}
}

func TestPasswordResetRequestDoesNotRevealMissingUser(t *testing.T) {
	provider := &passwordResetProvider{}
	svc := auth.NewService(userRepo{}, authplatform.BcryptHasher{Cost: 4}, authplatform.NewJWTService([]byte("test-secret"), time.Minute, time.Hour), authplatform.NewMemorySessionStore())
	svc.SetPasswordResetProvider(provider)

	if err := svc.RequestPasswordReset(context.Background(), "missing"); err != nil {
		t.Fatalf("RequestPasswordReset(missing) error = %v", err)
	}
	if provider.requestedFor != "" {
		t.Fatalf("provider received missing identifier %q", provider.requestedFor)
	}
}

func TestPasswordResetConsumesOneTimeTokenAndHashesReplacement(t *testing.T) {
	hasher := authplatform.BcryptHasher{Cost: 4}
	provisioner := &accountProvisioner{}
	provider := &passwordResetProvider{identifier: "alice"}
	svc := auth.NewService(userRepo{user: authdomain.User{ID: "1", Identifier: "alice", Active: true}}, hasher, authplatform.NewJWTService([]byte("test-secret"), time.Minute, time.Hour), authplatform.NewMemorySessionStore())
	svc.SetAccountProvisioner(provisioner)
	svc.SetPasswordResetProvider(provider)

	if err := svc.RequestPasswordReset(context.Background(), " alice "); err != nil {
		t.Fatalf("RequestPasswordReset() error = %v", err)
	}
	if provider.requestedFor != "alice" {
		t.Fatalf("provider requested identifier = %q", provider.requestedFor)
	}
	if err := svc.ResetPassword(context.Background(), "reset-token", "replacement-password"); err != nil {
		t.Fatalf("ResetPassword() error = %v", err)
	}
	if provider.consumeToken != "reset-token" || provisioner.updatedUser != "alice" {
		t.Fatalf("consume/update = %q/%q", provider.consumeToken, provisioner.updatedUser)
	}
	if provisioner.updatedHash == "replacement-password" || hasher.Compare(provisioner.updatedHash, "replacement-password") != nil {
		t.Fatal("ResetPassword() did not persist a bcrypt password hash")
	}
}
