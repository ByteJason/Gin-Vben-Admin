package authhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appauth "example.com/gin-vben-admin/server/internal/application/auth"
	"example.com/gin-vben-admin/server/internal/config"
	"example.com/gin-vben-admin/server/internal/domain/authdomain"
	"example.com/gin-vben-admin/server/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
)

type fakeAuthService struct {
	loginPair   authdomain.TokenPair
	refreshPair authdomain.TokenPair
	loginErr    error
	refreshErr  error
	logoutErr   error
	logoutToken string
	refreshGot  string
}

func (f *fakeAuthService) Login(context.Context, string, string) (authdomain.TokenPair, error) {
	return f.loginPair, f.loginErr
}

func (f *fakeAuthService) Refresh(_ context.Context, token string) (authdomain.TokenPair, error) {
	f.refreshGot = token
	return f.refreshPair, f.refreshErr
}

func (f *fakeAuthService) Logout(context.Context, string) error { return f.logoutErr }

func (f *fakeAuthService) VerifyAccess(string) (authdomain.Claims, error) {
	return authdomain.Claims{}, nil
}

func (f *fakeAuthService) LogoutWithRefreshToken(_ context.Context, token string) error {
	f.logoutToken = token
	return f.logoutErr
}

var _ appauth.AuthService = (*fakeAuthService)(nil)

func testAuthConfig() config.AuthConfig {
	cfg := config.Default().Auth
	cfg.Enabled = true
	cfg.JWTSecret = strings.Repeat("s", 32)
	return cfg
}

func newAuthRouter(service appauth.AuthService, cfg config.AuthConfig) *gin.Engine {
	r := gin.New()
	r.Use(middleware.RequestID())
	RegisterRoutes(r, NewHandler(service, cfg))
	return r
}

func newRateLimitedAuthRouter(service appauth.AuthService, cfg config.AuthConfig, limiter appauth.RateLimiter) *gin.Engine {
	r := gin.New()
	r.Use(middleware.RequestID())
	RegisterRoutes(r, NewHandler(service, cfg, limiter))
	return r
}

func TestLoginReturnsCamelCaseAccessTokenAndHttpOnlyRefreshCookie(t *testing.T) {
	service := &fakeAuthService{loginPair: authdomain.TokenPair{
		AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresIn: 900,
	}}
	r := newAuthRouter(service, testAuthConfig())
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{"username":"alice","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200; body=%s", res.Code, res.Body.String())
	}
	var body struct {
		Code int `json:"code"`
		Data struct {
			AccessToken string `json:"accessToken"`
			TokenType   string `json:"tokenType"`
			ExpiresIn   int64  `json:"expiresIn"`
			Refresh     string `json:"refreshToken"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Code != 0 || body.Data.AccessToken != "access-1" || body.Data.TokenType != "Bearer" || body.Data.ExpiresIn != 900 || body.Data.Refresh != "" {
		t.Fatalf("unexpected login body: %+v", body)
	}
	cookie := res.Result().Cookies()
	if len(cookie) != 1 || cookie[0].Name != "refresh_token" || cookie[0].Value != "refresh-1" || !cookie[0].HttpOnly || cookie[0].SameSite != http.SameSiteLaxMode || cookie[0].Path != "/api/admin/v1/auth" {
		t.Fatalf("unexpected refresh cookie: %+v", cookie)
	}
}

func TestLoginMapsInvalidCredentialsWithoutDetail(t *testing.T) {
	service := &fakeAuthService{loginErr: authdomain.ErrInvalidCredentials}
	r := newAuthRouter(service, testAuthConfig())
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{"username":"alice","password":"bad"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("login status = %d, want 401", res.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &body)
	if body["code"] != float64(10002) || body["message"] != "invalid credentials" {
		t.Fatalf("unexpected error body: %v", body)
	}
}

func TestLoginMapsLockedAccountToGenericCredentialsError(t *testing.T) {
	service := &fakeAuthService{loginErr: authdomain.ErrAccountLocked}
	r := newAuthRouter(service, testAuthConfig())
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{"username":"alice","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("locked login status = %d, want 401", res.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &body)
	if body["code"] != float64(codeCredentials) || body["message"] != "invalid credentials" {
		t.Fatalf("locked account leaked state: %v", body)
	}
}

func TestLoginMapsAccountLockoutToGenericCredentialsError(t *testing.T) {
	service := &fakeAuthService{loginErr: authdomain.ErrAccountLocked}
	r := newAuthRouter(service, testAuthConfig())
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{"username":"alice","password":"bad"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("login status = %d, want 401", res.Code)
	}
	var body map[string]any
	_ = json.Unmarshal(res.Body.Bytes(), &body)
	if body["code"] != float64(codeCredentials) || body["message"] != "invalid credentials" {
		t.Fatalf("unexpected error body: %v", body)
	}
}

func TestLoginMapsDependencyFailureToServiceUnavailable(t *testing.T) {
	service := &fakeAuthService{loginErr: authdomain.ErrDependencyUnavailable}
	r := newAuthRouter(service, testAuthConfig())
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{"username":"alice","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("login status = %d, want 503; body=%s", res.Code, res.Body.String())
	}
}

func TestLoginRateLimitReturns429AfterConfiguredAttempts(t *testing.T) {
	cfg := testAuthConfig()
	cfg.RateLimitMaxAttempts = 1
	service := &fakeAuthService{loginPair: authdomain.TokenPair{AccessToken: "a", RefreshToken: "r", ExpiresIn: 60}}
	r := newRateLimitedAuthRouter(service, cfg, appauth.NewMemoryRateLimiter())
	for attempt := 1; attempt <= 2; attempt++ {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{"username":"alice","password":"secret"}`))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		r.ServeHTTP(res, req)
		want := http.StatusOK
		if attempt == 2 {
			want = http.StatusTooManyRequests
		}
		if res.Code != want {
			t.Fatalf("attempt %d status = %d, want %d; body=%s", attempt, res.Code, want, res.Body.String())
		}
		if attempt == 2 && res.Header().Get("Retry-After") == "" {
			t.Fatal("rate-limited response did not include Retry-After")
		}
	}
}

func TestRefreshReadsCookieRotatesAndDoesNotExposeRefreshToken(t *testing.T) {
	service := &fakeAuthService{refreshPair: authdomain.TokenPair{AccessToken: "access-2", RefreshToken: "refresh-2", ExpiresIn: 900}}
	r := newAuthRouter(service, testAuthConfig())
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "refresh-1"})
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)

	if res.Code != http.StatusOK || service.refreshGot != "refresh-1" {
		t.Fatalf("refresh status=%d token=%q body=%s", res.Code, service.refreshGot, res.Body.String())
	}
	if strings.Contains(res.Body.String(), "refresh-2") || len(res.Result().Cookies()) != 1 || res.Result().Cookies()[0].Value != "refresh-2" {
		t.Fatalf("refresh token leaked or cookie not rotated: body=%s cookies=%v", res.Body.String(), res.Result().Cookies())
	}
}

func TestRefreshMissingCookieIsUnauthenticated(t *testing.T) {
	r := newAuthRouter(&fakeAuthService{}, testAuthConfig())
	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/refresh", nil))
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("refresh status = %d, want 401", res.Code)
	}
}

func TestLogoutRevokesByRefreshTokenAndClearsCookie(t *testing.T) {
	service := &fakeAuthService{}
	r := newAuthRouter(service, testAuthConfig())
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "refresh-1"})
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)

	if res.Code != http.StatusOK || service.logoutToken != "refresh-1" {
		t.Fatalf("logout status=%d token=%q body=%s", res.Code, service.logoutToken, res.Body.String())
	}
	cookies := res.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge >= 0 || cookies[0].Value != "" || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("logout did not clear cookie: %+v", cookies)
	}
}

func TestAuthDisabledReturnsServiceUnavailable(t *testing.T) {
	cfg := testAuthConfig()
	cfg.Enabled = false
	r := newAuthRouter(&fakeAuthService{}, cfg)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{"username":"a","password":"b"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("disabled auth status = %d, want 503", res.Code)
	}
}
