package authhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appauth "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/auth"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	"github.com/gin-gonic/gin"
)

type fakeSessionManager struct {
	sessions []authdomain.Session
	userID   string
	session  string
}

func (f *fakeSessionManager) ListSessions(_ context.Context, userID string) ([]authdomain.Session, error) {
	f.userID = userID
	return f.sessions, nil
}

func (f *fakeSessionManager) RevokeSession(_ context.Context, userID, sessionID string) error {
	f.userID, f.session = userID, sessionID
	return nil
}

func newSessionRouter(manager appauth.SessionManagementService) *gin.Engine {
	r := gin.New()
	handler := NewHandler(&fakeAuthService{claims: authdomain.Claims{Subject: "user-1"}}, testAuthConfig())
	handler.SetSessionManager(manager)
	RegisterRoutes(r, handler)
	return r
}

func TestSessionListReturnsSafeDeviceFields(t *testing.T) {
	manager := &fakeSessionManager{sessions: []authdomain.Session{{
		ID: "s1", UserID: "user-1", RefreshJTI: "secret-jti", RefreshJTIHash: "secret-hash",
		DeviceID: "device-1", DeviceName: "Safari", IPAddress: "127.0.0.1", UserAgent: "UA", ExpiresAt: time.Now().Add(time.Hour),
	}}}
	r := newSessionRouter(manager)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/auth/sessions", nil)
	req.Header.Set("Authorization", "Bearer access-token")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)

	if res.Code != http.StatusOK || manager.userID != "user-1" {
		t.Fatalf("session list status=%d user=%q body=%s", res.Code, manager.userID, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "secret-jti") || strings.Contains(res.Body.String(), "secret-hash") {
		t.Fatalf("session list leaked token material: %s", res.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != float64(0) {
		t.Fatalf("session list body=%v", body)
	}
}

func TestSessionRevokeUsesPathIDAndBearerSubject(t *testing.T) {
	manager := &fakeSessionManager{}
	r := newSessionRouter(manager)
	req := httptest.NewRequest(http.MethodDelete, "/api/admin/v1/auth/sessions/s1", nil)
	req.Header.Set("Authorization", "Bearer access-token")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("session revoke status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestSessionListRequiresBearerToken(t *testing.T) {
	r := newSessionRouter(&fakeSessionManager{})
	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/admin/v1/auth/sessions", nil))
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("session list without bearer status=%d", res.Code)
	}
}

var _ appauth.SessionManagementService = (*fakeSessionManager)(nil)
