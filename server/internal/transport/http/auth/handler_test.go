package authhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appauth "example.com/gin-vben-admin/server/internal/application/auth"
	"example.com/gin-vben-admin/server/internal/config"
	"example.com/gin-vben-admin/server/internal/domain/authdomain"
	"example.com/gin-vben-admin/server/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
)

type fakeAuthService struct {
	loginPair       authdomain.TokenPair
	refreshPair     authdomain.TokenPair
	loginErr        error
	refreshErr      error
	logoutErr       error
	logoutToken     string
	refreshGot      string
	claims          authdomain.Claims
	loginMeta       appauth.RequestMetadata
	refreshMeta     appauth.RequestMetadata
	logoutMeta      appauth.RequestMetadata
	loginIdentifier string
}

func (f *fakeAuthService) Login(ctx context.Context, identifier string, _ string) (authdomain.TokenPair, error) {
	f.loginMeta = appauth.RequestMetadataFromContext(ctx)
	f.loginIdentifier = identifier
	return f.loginPair, f.loginErr
}

func (f *fakeAuthService) Refresh(ctx context.Context, token string) (authdomain.TokenPair, error) {
	f.refreshMeta = appauth.RequestMetadataFromContext(ctx)
	f.refreshGot = token
	return f.refreshPair, f.refreshErr
}

func (f *fakeAuthService) Logout(ctx context.Context, _ string) error {
	f.logoutMeta = appauth.RequestMetadataFromContext(ctx)
	return f.logoutErr
}

func (f *fakeAuthService) VerifyAccess(string) (authdomain.Claims, error) {
	return f.claims, nil
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

func TestLoginAcceptsCanonicalEmailIdentifierPayload(t *testing.T) {
	service := &fakeAuthService{loginPair: authdomain.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 60}}
	r := newAuthRouter(service, testAuthConfig())
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{"identifier":"  Alice@Example.TEST ","identifierType":"email","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("email login status = %d, body=%s", res.Code, res.Body.String())
	}
	if service.loginIdentifier != "alice@example.test" {
		t.Fatalf("service identifier = %q, want canonical email", service.loginIdentifier)
	}
}

func TestLoginRejectsPhoneIdentifierPayload(t *testing.T) {
	service := &fakeAuthService{loginPair: authdomain.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 60}}
	r := newAuthRouter(service, testAuthConfig())
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{"identifier":"+8613800138000","identifierType":"phone","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("phone login status = %d, want 400; body=%s", res.Code, res.Body.String())
	}
	if service.loginIdentifier != "" {
		t.Fatalf("phone identifier reached service: %q", service.loginIdentifier)
	}
}

func TestLoginRejectsConflictingCompatibilityIdentifiers(t *testing.T) {
	service := &fakeAuthService{loginPair: authdomain.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 60}}
	r := newAuthRouter(service, testAuthConfig())
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{"username":"alice","identifier":"bob","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("conflicting identifiers status = %d, want 400; body=%s", res.Code, res.Body.String())
	}
	if service.loginIdentifier != "" {
		t.Fatalf("conflicting identifiers reached service: %q", service.loginIdentifier)
	}
}

func TestAuthHandlersPropagateRequestMetadata(t *testing.T) {
	service := &fakeAuthService{loginPair: authdomain.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 60}}
	r := newAuthRouter(service, testAuthConfig())
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{"username":"alice","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-ID", "req-http")
	req.Header.Set("X-Device-ID", "device-http")
	req.Header.Set("X-Device-Name", "Browser")
	req.Header.Set("User-Agent", "test-agent")
	req.RemoteAddr = "192.0.2.10:4567"
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("login status = %d; body=%s", res.Code, res.Body.String())
	}
	want := appauth.RequestMetadata{RequestID: "req-http", DeviceID: "device-http", DeviceName: "Browser", IPAddress: "192.0.2.10", UserAgent: "test-agent"}
	if service.loginMeta != want {
		t.Fatalf("login metadata = %+v, want %+v", service.loginMeta, want)
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

func TestLoginUsesConfiguredCaptchaProvider(t *testing.T) {
	cfg := testAuthConfig()
	service := &fakeAuthService{loginPair: authdomain.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 60}}
	provider := appauth.NewMemoryCaptchaProvider(time.Minute)
	challenge, err := provider.Issue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.PutAnswer(challenge.ID, "4821"); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(service, cfg)
	handler.SetCaptchaProvider(provider)
	r := gin.New()
	RegisterRoutes(r, handler)

	missing := httptest.NewRecorder()
	missingReq := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{"username":"alice","password":"secret"}`))
	missingReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(missing, missingReq)
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing captcha status = %d, want 400", missing.Code)
	}
	invalid := httptest.NewRecorder()
	invalidReq := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{"username":"alice","password":"secret","captchaId":"`+challenge.ID+`","captcha":"0000"}`))
	invalidReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(invalid, invalidReq)
	if invalid.Code != http.StatusBadRequest || !strings.Contains(invalid.Body.String(), `"code":10005`) {
		t.Fatalf("invalid captcha status=%d body=%s", invalid.Code, invalid.Body.String())
	}

	validChallenge, err := provider.Issue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.PutAnswer(validChallenge.ID, "4821"); err != nil {
		t.Fatal(err)
	}
	valid := httptest.NewRecorder()
	validReq := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{"username":"alice","password":"secret","captchaId":"`+validChallenge.ID+`","captcha":"4821"}`))
	validReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(valid, validReq)
	if valid.Code != http.StatusOK {
		t.Fatalf("valid captcha status = %d, body=%s", valid.Code, valid.Body.String())
	}
}

func TestCaptchaChallengeEndpointDoesNotExposeAnswer(t *testing.T) {
	cfg := testAuthConfig()
	handler := NewHandler(&fakeAuthService{}, cfg)
	provider := appauth.NewMemoryCaptchaProvider(time.Minute)
	handler.SetCaptchaProvider(provider)
	r := gin.New()
	RegisterRoutes(r, handler)
	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/admin/v1/auth/captcha", nil))
	if res.Code != http.StatusOK || strings.Contains(res.Body.String(), "answer") {
		t.Fatalf("captcha challenge response status=%d body=%s", res.Code, res.Body.String())
	}
}

type riskAuthService struct {
	fakeAuthService
	failures int
	calls    int
}

func (s *riskAuthService) Login(ctx context.Context, identifier, password string) (authdomain.TokenPair, error) {
	s.calls++
	if s.calls <= s.failures {
		s.loginIdentifier = identifier
		return authdomain.TokenPair{}, authdomain.ErrInvalidCredentials
	}
	return s.fakeAuthService.Login(ctx, identifier, password)
}

func TestLoginActivatesCaptchaAfterRiskFailuresAndResetsOnSuccess(t *testing.T) {
	cfg := testAuthConfig()
	cfg.CaptchaEnabled = false
	cfg.CaptchaRiskThreshold = 2
	cfg.CaptchaRiskWindow = time.Minute
	service := &riskAuthService{
		fakeAuthService: fakeAuthService{loginPair: authdomain.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 60}},
		failures:        2,
	}
	provider := appauth.NewMemoryCaptchaProvider(time.Minute)
	risk := appauth.NewMemoryCaptchaRiskStore()
	handler := NewHandler(service, cfg)
	handler.SetCaptchaProvider(provider)
	handler.SetCaptchaRiskStore(risk)
	r := gin.New()
	RegisterRoutes(r, handler)

	for attempt := 1; attempt <= 2; attempt++ {
		res := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{"identifier":"alice","password":"bad"}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(res, req)
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("failed attempt %d status = %d, body=%s", attempt, res.Code, res.Body.String())
		}
	}
	if service.calls != 2 {
		t.Fatalf("service calls after failed attempts = %d, want 2", service.calls)
	}

	blocked := httptest.NewRecorder()
	blockedReq := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{"identifier":"alice","password":"secret"}`))
	blockedReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(blocked, blockedReq)
	if blocked.Code != http.StatusBadRequest || service.calls != 2 {
		t.Fatalf("risk-triggered captcha status=%d calls=%d body=%s", blocked.Code, service.calls, blocked.Body.String())
	}

	challenge, err := provider.Issue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.PutAnswer(challenge.ID, "4821"); err != nil {
		t.Fatal(err)
	}
	valid := httptest.NewRecorder()
	validReq := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{"identifier":"alice","password":"secret","captchaId":"`+challenge.ID+`","captcha":"4821"}`))
	validReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(valid, validReq)
	if valid.Code != http.StatusOK {
		t.Fatalf("captcha-protected success status=%d body=%s", valid.Code, valid.Body.String())
	}
	if required, err := risk.Requires(context.Background(), "alice|192.0.2.1", cfg.CaptchaRiskThreshold, cfg.CaptchaRiskWindow); err != nil || required {
		t.Fatalf("risk was not reset after success: required=%v err=%v", required, err)
	}
}

func TestCaptchaEnabledFailsClosedWhenProviderIsUnavailable(t *testing.T) {
	cfg := testAuthConfig()
	cfg.CaptchaEnabled = true
	service := &fakeAuthService{loginPair: authdomain.TokenPair{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 60}}
	r := newAuthRouter(service, cfg)
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{"identifier":"alice","password":"secret"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(res, req)
	if res.Code != http.StatusServiceUnavailable {
		t.Fatalf("captcha-enabled without provider status=%d body=%s", res.Code, res.Body.String())
	}
	if service.loginIdentifier != "" {
		t.Fatalf("login reached service without captcha provider: %q", service.loginIdentifier)
	}
}
