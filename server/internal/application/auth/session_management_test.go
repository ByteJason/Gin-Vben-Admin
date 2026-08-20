package auth_test

import (
	"context"
	"testing"
	"time"

	"example.com/gin-vben-admin/server/internal/application/auth"
	"example.com/gin-vben-admin/server/internal/domain/authdomain"
	"example.com/gin-vben-admin/server/internal/platform/authplatform"
)

type sessionQuery struct {
	sessions []authdomain.Session
	userID   string
	session  string
}

func (q *sessionQuery) ListByUser(_ context.Context, userID string) ([]authdomain.Session, error) {
	q.userID = userID
	return q.sessions, nil
}

func (q *sessionQuery) RevokeOwned(_ context.Context, userID, sessionID string) error {
	q.userID, q.session = userID, sessionID
	return nil
}

func TestSessionManagementDelegatesWithUserBoundary(t *testing.T) {
	query := &sessionQuery{sessions: []authdomain.Session{{ID: "s1", UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)}}}
	runtime := authplatform.NewMemorySessionStore()
	if err := runtime.Create(context.Background(), authdomain.Session{ID: "s1", UserID: "u1", RefreshJTI: "jti", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	svc := auth.NewService(userRepo{}, authplatform.BcryptHasher{Cost: 4}, authplatform.NewJWTService([]byte("test-secret"), time.Minute, time.Hour), runtime)
	svc.SetSessionQuery(query)

	sessions, err := svc.ListSessions(context.Background(), "u1")
	if err != nil || len(sessions) != 1 || query.userID != "u1" {
		t.Fatalf("ListSessions() = %+v, %v query=%+v", sessions, err, query)
	}
	if err := svc.RevokeSession(context.Background(), "u1", "s1"); err != nil {
		t.Fatalf("RevokeSession() error = %v", err)
	}
	if query.userID != "u1" || query.session != "s1" {
		t.Fatalf("RevokeSession() boundary = user:%q session:%q", query.userID, query.session)
	}
	if session, err := runtime.Get(context.Background(), "s1"); err != nil || !session.Revoked {
		t.Fatalf("RevokeSession() runtime state = %+v, %v", session, err)
	}
}

func TestRevokeSessionDoesNotTouchAnotherUsersRuntimeSession(t *testing.T) {
	runtime := authplatform.NewMemorySessionStore()
	if err := runtime.Create(context.Background(), authdomain.Session{ID: "s-other", UserID: "u2", RefreshJTI: "jti", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	query := &sessionQuery{sessions: []authdomain.Session{{ID: "s-other", UserID: "u2", ExpiresAt: time.Now().Add(time.Hour)}}}
	svc := auth.NewService(userRepo{}, authplatform.BcryptHasher{Cost: 4}, authplatform.NewJWTService([]byte("test-secret"), time.Minute, time.Hour), runtime)
	svc.SetSessionQuery(query)

	if err := svc.RevokeSession(context.Background(), "u1", "s-other"); err != authdomain.ErrSessionNotFound {
		t.Fatalf("RevokeSession() error = %v, want session not found", err)
	}
	if session, err := runtime.Get(context.Background(), "s-other"); err != nil || session.Revoked {
		t.Fatalf("another user's runtime session = %+v, %v", session, err)
	}
}
