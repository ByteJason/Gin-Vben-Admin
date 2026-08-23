package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/application/auth"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/authplatform"
)

type accountProvisioner struct {
	created     authdomain.User
	updatedUser string
	updatedHash string
	createErr   error
	updateErr   error
}

type recordingRecoveryRepo struct {
	user       authdomain.User
	identifier string
}

func (r *recordingRecoveryRepo) FindByIdentifier(_ context.Context, identifier string) (authdomain.User, error) {
	r.identifier = identifier
	return r.user, nil
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

func TestRegisterNormalizesEmailIdentifierBeforeProvisioning(t *testing.T) {
	provisioner := &accountProvisioner{}
	svc := auth.NewService(userRepo{}, authplatform.BcryptHasher{Cost: 4}, authplatform.NewJWTService([]byte("test-secret"), time.Minute, time.Hour), authplatform.NewMemorySessionStore())
	svc.SetAccountProvisioner(provisioner)

	if err := svc.Register(context.Background(), " Alice@Example.TEST ", "correct-password"); err != nil {
		t.Fatalf("Register(email) error = %v", err)
	}
	if provisioner.created.Identifier != "alice@example.test" || provisioner.created.Email != "alice@example.test" || provisioner.created.Username != "alice@example.test" {
		t.Fatalf("created email user = %+v", provisioner.created)
	}
}

func TestPasswordResetNormalizesEmailIdentifierBeforeLookup(t *testing.T) {
	repo := &recordingRecoveryRepo{user: authdomain.User{ID: "1", Identifier: "alice@example.test", Active: true}}
	provider := &passwordResetProvider{}
	svc := auth.NewService(repo, authplatform.BcryptHasher{Cost: 4}, authplatform.NewJWTService([]byte("test-secret"), time.Minute, time.Hour), authplatform.NewMemorySessionStore())
	svc.SetPasswordResetProvider(provider)

	if err := svc.RequestPasswordReset(context.Background(), " Alice@Example.TEST "); err != nil {
		t.Fatalf("RequestPasswordReset(email) error = %v", err)
	}
	if repo.identifier != "alice@example.test" || provider.requestedFor != "alice@example.test" {
		t.Fatalf("reset identifiers = repo:%q provider:%q", repo.identifier, provider.requestedFor)
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
