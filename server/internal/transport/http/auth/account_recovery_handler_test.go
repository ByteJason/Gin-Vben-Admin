package authhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appauth "example.com/gin-vben-admin/server/internal/application/auth"
	"example.com/gin-vben-admin/server/internal/config"
	"example.com/gin-vben-admin/server/internal/domain/authdomain"
	"github.com/gin-gonic/gin"
)

type fakeAccountRecovery struct {
	registerIdentifier string
	registerPassword   string
	resetIdentifier    string
	resetToken         string
	resetPassword      string
	registerErr        error
	requestErr         error
	resetErr           error
}

func (f *fakeAccountRecovery) Register(_ context.Context, identifier, password string) error {
	f.registerIdentifier, f.registerPassword = identifier, password
	return f.registerErr
}

func (f *fakeAccountRecovery) RequestPasswordReset(_ context.Context, identifier string) error {
	f.resetIdentifier = identifier
	return f.requestErr
}

func (f *fakeAccountRecovery) ResetPassword(_ context.Context, token, password string) error {
	f.resetToken, f.resetPassword = token, password
	return f.resetErr
}

func newRecoveryRouter(service appauth.AuthService, recovery appauth.AccountRecoveryService, cfg config.AuthConfig) *gin.Engine {
	r := gin.New()
	cfg.RegistrationEnabled = true
	handler := NewHandler(service, cfg)
	handler.SetAccountRecovery(recovery)
	RegisterRoutes(r, handler)
	return r
}

func TestRegisterEndpointDelegatesAndDoesNotReturnPassword(t *testing.T) {
	recovery := &fakeAccountRecovery{}
	r := newRecoveryRouter(&fakeAuthService{}, recovery, testAuthConfig())
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/register", strings.NewReader(`{"username":" alice ","password":"correct-password"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(res, req)

	if res.Code != http.StatusOK || recovery.registerIdentifier != "alice" || recovery.registerPassword != "correct-password" {
		t.Fatalf("register status=%d recovery=%+v body=%s", res.Code, recovery, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "correct-password") {
		t.Fatal("register response leaked password")
	}
}

func TestPasswordResetRequestReturnsGenericSuccess(t *testing.T) {
	recovery := &fakeAccountRecovery{}
	r := newRecoveryRouter(&fakeAuthService{}, recovery, testAuthConfig())
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/password/reset/request", strings.NewReader(`{"username":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(res, req)

	if res.Code != http.StatusOK || recovery.resetIdentifier != "alice" {
		t.Fatalf("reset request status=%d identifier=%q body=%s", res.Code, recovery.resetIdentifier, res.Body.String())
	}
}

func TestPasswordResetMapsInvalidTokenWithoutDetail(t *testing.T) {
	recovery := &fakeAccountRecovery{resetErr: authdomain.ErrPasswordResetInvalid}
	r := newRecoveryRouter(&fakeAuthService{}, recovery, testAuthConfig())
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/password/reset", strings.NewReader(`{"token":"bad","password":"replacement-password"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("reset status=%d body=%s", res.Code, res.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != float64(codeCredentials) || body["message"] != "invalid credentials" || strings.Contains(res.Body.String(), "bad") {
		t.Fatalf("unexpected reset error body: %v", body)
	}
}

func TestPasswordResetDependencyFailureIs503(t *testing.T) {
	recovery := &fakeAccountRecovery{requestErr: errors.New("provider unavailable")}
	r := newRecoveryRouter(&fakeAuthService{}, recovery, testAuthConfig())
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/password/reset/request", strings.NewReader(`{"username":"alice"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("reset dependency status=%d body=%s", res.Code, res.Body.String())
	}
}

var _ appauth.AccountRecoveryService = (*fakeAccountRecovery)(nil)
