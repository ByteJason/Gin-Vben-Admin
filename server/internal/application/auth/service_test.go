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

type userRepo struct{ user authdomain.User }

func (r userRepo) FindByIdentifier(context.Context, string) (authdomain.User, error) {
	return r.user, nil
}

type recordingUserRepo struct {
	user       authdomain.User
	identifier string
}

func (r *recordingUserRepo) FindByIdentifier(_ context.Context, identifier string) (authdomain.User, error) {
	r.identifier = identifier
	return r.user, nil
}

type failingUserRepo struct{}

func (failingUserRepo) FindByIdentifier(context.Context, string) (authdomain.User, error) {
	return authdomain.User{}, errors.New("database socket closed")
}

type recordingSessionJournal struct {
	created int
	rotated int
	revoked int
}

func (j *recordingSessionJournal) Create(context.Context, authdomain.Session) error {
	j.created++
	return nil
}

func (j *recordingSessionJournal) Rotate(context.Context, string, string, string, time.Time) error {
	j.rotated++
	return nil
}

func (j *recordingSessionJournal) Revoke(context.Context, string) error {
	j.revoked++
	return nil
}

func TestSessionJournalTracksLifecycle(t *testing.T) {
	hasher := authplatform.BcryptHasher{Cost: 4}
	hash, err := hasher.Hash("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	tokens := authplatform.NewJWTService([]byte("test-secret"), time.Minute, time.Hour)
	journal := &recordingSessionJournal{}
	svc := auth.NewService(userRepo{user: authdomain.User{ID: "1", Identifier: "alice", PasswordHash: hash, Active: true}}, hasher, tokens, authplatform.NewMemorySessionStore())
	svc.SetSessionJournal(journal)
	pair, err := svc.Login(context.Background(), "alice", "correct-password")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	claims, err := tokens.Parse(pair.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Refresh(context.Background(), pair.RefreshToken); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if err := svc.Logout(context.Background(), claims.SessionID); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if journal.created != 1 || journal.rotated != 1 || journal.revoked != 1 {
		t.Fatalf("journal calls = create:%d rotate:%d revoke:%d", journal.created, journal.rotated, journal.revoked)
	}
}

func TestLoginNormalizesEmailBeforeLookup(t *testing.T) {
	hasher := authplatform.BcryptHasher{Cost: 4}
	hash, err := hasher.Hash("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	repo := &recordingUserRepo{user: authdomain.User{ID: "user-1", Email: "Alice@Example.test", Identifier: "Alice", PasswordHash: hash, Active: true}}
	svc := auth.NewService(repo, hasher, authplatform.NewJWTService([]byte("test-secret"), time.Minute, time.Hour), authplatform.NewMemorySessionStore())
	if _, err := svc.Login(context.Background(), "  ALICE@EXAMPLE.TEST ", "correct-password"); err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if repo.identifier != "alice@example.test" {
		t.Fatalf("repository identifier = %q, want canonical email", repo.identifier)
	}
}

func TestLoginRefreshRotationAndReplayRejection(t *testing.T) {
	ctx := context.Background()
	hasher := authplatform.BcryptHasher{Cost: 4}
	hash, err := hasher.Hash("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	store := authplatform.NewMemorySessionStore()
	tokens := authplatform.NewJWTService([]byte("test-secret"), time.Minute, time.Hour)
	svc := auth.NewService(userRepo{user: authdomain.User{ID: "user-1", Identifier: "person@example.test", PasswordHash: hash, Active: true}}, hasher, tokens, store)

	pair, err := svc.Login(ctx, "person@example.test", "correct-password")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("Login() returned empty token pair")
	}

	claims, err := tokens.Parse(pair.RefreshToken)
	if err != nil {
		t.Fatalf("Parse(refresh) error = %v", err)
	}
	if claims.Type != authdomain.RefreshToken || claims.Subject != "user-1" {
		t.Fatalf("claims = %+v", claims)
	}

	rotated, err := svc.Refresh(ctx, pair.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if rotated.RefreshToken == pair.RefreshToken {
		t.Fatal("Refresh() did not rotate refresh token")
	}
	if _, err := svc.Refresh(ctx, pair.RefreshToken); !errors.Is(err, authdomain.ErrRefreshReplay) {
		t.Fatalf("replayed Refresh() error = %v, want ErrRefreshReplay", err)
	}
	if err := svc.Logout(ctx, claims.SessionID); err != nil {
		t.Fatalf("Logout() error = %v", err)
	}
	if _, err := svc.Refresh(ctx, rotated.RefreshToken); !errors.Is(err, authdomain.ErrSessionRevoked) {
		t.Fatalf("revoked Refresh() error = %v, want ErrSessionRevoked", err)
	}
}

func TestLoginRejectsWrongPasswordAndInactiveUser(t *testing.T) {
	hasher := authplatform.BcryptHasher{Cost: 4}
	hash, _ := hasher.Hash("correct-password")
	store := authplatform.NewMemorySessionStore()
	tokens := authplatform.NewJWTService([]byte("test-secret"), time.Minute, time.Hour)
	base := authdomain.User{ID: "user-1", Identifier: "person@example.test", PasswordHash: hash, Active: true}
	for _, tc := range []struct {
		name, password string
		active         bool
	}{
		{"wrong password", "wrong", true}, {"inactive", "correct-password", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := base
			u.Active = tc.active
			svc := auth.NewService(userRepo{user: u}, hasher, tokens, store)
			if _, err := svc.Login(context.Background(), u.Identifier, tc.password); !errors.Is(err, authdomain.ErrInvalidCredentials) {
				t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
			}
		})
	}
}

func TestLoginClosesOnUserStoreFailure(t *testing.T) {
	hasher := authplatform.BcryptHasher{Cost: 4}
	tokens := authplatform.NewJWTService([]byte("test-secret"), time.Minute, time.Hour)
	svc := auth.NewService(failingUserRepo{}, hasher, tokens, authplatform.NewMemorySessionStore())
	if _, err := svc.Login(context.Background(), "alice", "password"); !errors.Is(err, authdomain.ErrDependencyUnavailable) {
		t.Fatalf("Login() error = %v, want dependency unavailable", err)
	}
}

func TestLoginLocksIdentifierAfterFailedAttempts(t *testing.T) {
	hasher := authplatform.BcryptHasher{Cost: 4}
	hash, err := hasher.Hash("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	tokens := authplatform.NewJWTService([]byte("test-secret"), time.Minute, time.Hour)
	attempts := auth.NewMemoryLoginAttemptStore(3, time.Minute)
	svc := auth.NewService(userRepo{user: authdomain.User{ID: "user-1", Identifier: "alice", PasswordHash: hash, Active: true}}, hasher, tokens, authplatform.NewMemorySessionStore(), attempts)
	for i := 0; i < 2; i++ {
		if _, err := svc.Login(context.Background(), "alice", "wrong"); !errors.Is(err, authdomain.ErrInvalidCredentials) {
			t.Fatalf("attempt %d error = %v, want invalid credentials", i+1, err)
		}
	}
	if _, err := svc.Login(context.Background(), "alice", "wrong"); !errors.Is(err, authdomain.ErrAccountLocked) {
		t.Fatalf("threshold error = %v, want account locked", err)
	}
	if _, err := svc.Login(context.Background(), "alice", "correct-password"); !errors.Is(err, authdomain.ErrAccountLocked) {
		t.Fatalf("locked success error = %v, want account locked", err)
	}
}

func TestSuccessfulLoginResetsFailedAttemptCounter(t *testing.T) {
	hasher := authplatform.BcryptHasher{Cost: 4}
	hash, err := hasher.Hash("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	tokens := authplatform.NewJWTService([]byte("test-secret"), time.Minute, time.Hour)
	attempts := auth.NewMemoryLoginAttemptStore(2, time.Minute)
	svc := auth.NewService(userRepo{user: authdomain.User{ID: "user-1", Identifier: "alice", PasswordHash: hash, Active: true}}, hasher, tokens, authplatform.NewMemorySessionStore(), attempts)
	if _, err := svc.Login(context.Background(), "alice", "wrong"); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Fatalf("first failure = %v", err)
	}
	if _, err := svc.Login(context.Background(), "alice", "correct-password"); err != nil {
		t.Fatalf("successful login = %v", err)
	}
	if _, err := svc.Login(context.Background(), "alice", "wrong"); !errors.Is(err, authdomain.ErrInvalidCredentials) {
		t.Fatalf("post-reset failure = %v, want invalid credentials", err)
	}
}
