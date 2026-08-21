// Package integration contains opt-in tests that exercise the authentication
// seam against the local MySQL/PostgreSQL and Redis services.  The second
// environment gate keeps ordinary `go test ./...` network-free.
package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"example.com/gin-vben-admin/server/internal/bootstrap"
	"example.com/gin-vben-admin/server/internal/config"
	installstate "example.com/gin-vben-admin/server/internal/domain/installstate"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
	"example.com/gin-vben-admin/server/internal/platform/authplatform"
	rediscache "example.com/gin-vben-admin/server/internal/platform/cache/redis"
	"example.com/gin-vben-admin/server/internal/platform/migration"
)

const authIntegrationSecret = "integration-auth-secret-012345678901234567890123"

// TestAuthRefreshRotationIntegration is intentionally opt-in.  It exercises
// the complete HTTP flow against each supported single-node SQL driver while
// using the supplied Redis service for refresh-session state:
// Login -> refresh rotation -> old-token replay (401) -> logout revocation.
func TestAuthRefreshRotationIntegration(t *testing.T) {
	if os.Getenv("DATA_PLATFORM_INTEGRATION") != "1" {
		t.Skip("set DATA_PLATFORM_INTEGRATION=1 to run authentication integration tests")
	}

	mysqlDSN := requiredEnv(t, mysqlDSNEnv)
	postgresDSN := requiredEnv(t, postgresDSNEnv)
	redisAddr := requiredEnv(t, redisAddrEnv)

	for _, tc := range []struct {
		driver string
		dsn    string
	}{
		{driver: migration.DriverMySQL, dsn: mysqlDSN},
		{driver: migration.DriverPostgres, dsn: postgresDSN},
	} {
		t.Run(tc.driver, func(t *testing.T) {
			testAuthRefreshRotation(t, tc.driver, tc.dsn, redisAddr)
		})
	}
}

func testAuthRefreshRotation(t *testing.T, driver, dsn, redisAddr string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	runner, err := migration.New(driver, dsn)
	if err != nil {
		t.Fatalf("migration.New() error = %v", err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	if err := runner.Up(); err != nil {
		t.Fatalf("migration.Up() error = %v", err)
	}

	cfg := config.Default()
	cfg.Server.Addr = "127.0.0.1:0"
	cfg.Database.Enabled = true
	cfg.Database.Driver = driver
	cfg.Database.Mode = "single"
	cfg.Database.DSN = dsn
	cfg.Redis.Enabled = true
	cfg.Redis.Mode = rediscache.ModeSingle
	cfg.Redis.Addr = redisAddr
	cfg.Redis.Namespace = fmt.Sprintf("app:v1:test-auth-%d", time.Now().UnixNano())
	cfg.Auth.Enabled = true
	cfg.Auth.JWTSecret = authIntegrationSecret
	cfg.Auth.AccessTTL = 5 * time.Minute
	cfg.Auth.RefreshTTL = 30 * time.Minute
	cfg.Auth.BcryptCost = 10
	cfg.Auth.SecureCookie = false
	cfg.Auth.RegistrationEnabled = true
	stateDir := t.TempDir()
	cfg.Install.StateDir = stateDir
	marker := installstate.Marker{
		SchemaVersion:    installstate.CurrentSchemaVersion,
		InstallerVersion: "integration",
		InstalledAt:      time.Now().UTC(),
		SelectedUI:       installstate.UIAntd,
		Mode:             installstate.ModeAPIOnly,
		ArtifactHash:     strings.Repeat("a", 64),
		ManifestHash:     strings.Repeat("b", 64),
	}
	markerBytes, err := json.Marshal(marker)
	if err != nil {
		t.Fatalf("marshal integration install marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, ".installed"), markerBytes, 0o600); err != nil {
		t.Fatalf("write integration install marker: %v", err)
	}

	app, err := bootstrap.New(cfg)
	if err != nil {
		t.Fatalf("bootstrap.New() error = %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })
	cleanupRedis, err := rediscache.New(rediscache.Config{
		Mode:      rediscache.ModeSingle,
		Addr:      redisAddr,
		Namespace: cfg.Redis.Namespace,
	})
	if err != nil {
		t.Fatalf("redis cleanup client error = %v", err)
	}
	redisCleanupKeys := make([]string, 0, 4)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		for _, key := range redisCleanupKeys {
			if err := cleanupRedis.Delete(cleanupCtx, key); err != nil {
				t.Logf("redis cleanup key %q: %v", key, err)
			}
		}
		_ = cleanupRedis.Close()
	})

	username := fmt.Sprintf("it_auth_%s_%d", driver, time.Now().UnixNano())
	password := "integration-password-012345"
	registeredUsername := fmt.Sprintf("it_register_%s_%d", driver, time.Now().UnixNano())
	registeredPassword := "registered-password-012345"
	hasher := authplatform.BcryptHasher{Cost: cfg.Auth.BcryptCost}
	passwordHash, err := hasher.Hash(password)
	if err != nil {
		t.Fatalf("password hash error = %v", err)
	}
	if err := app.Database().Write(ctx).Exec(
		"INSERT INTO users (username, username_normalized, password_hash, status) VALUES (?, ?, ?, ?)",
		username, strings.ToLower(username), passwordHash, "active",
	).Error; err != nil {
		t.Fatalf("create integration user error = %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = app.Database().Write(cleanupCtx).Exec("DELETE FROM users WHERE username = ?", username).Error
		_ = app.Database().Write(cleanupCtx).Exec("DELETE FROM users WHERE username = ?", registeredUsername).Error
	})

	ts := httptest.NewServer(app.HTTPServer().Handler)
	t.Cleanup(ts.Close)
	client := ts.Client()
	loginURL := ts.URL + "/api/admin/v1/auth/login"
	registerURL := ts.URL + "/api/admin/v1/auth/register"
	refreshURL := ts.URL + "/api/admin/v1/auth/refresh"
	logoutURL := ts.URL + "/api/admin/v1/auth/logout"

	registerResponse := doAuthRequest(t, client, http.MethodPost, registerURL, `{"username":"`+registeredUsername+`","password":"`+registeredPassword+`"}`, nil)
	if registerResponse.status != http.StatusOK || registerResponse.envelope.Code != 0 {
		t.Fatalf("register status/code = %d/%d, body=%s", registerResponse.status, registerResponse.envelope.Code, registerResponse.body)
	}
	var registeredHash string
	if err := app.Database().Read(ctx).Table("users").Select("password_hash").Where("username = ?", registeredUsername).Scan(&registeredHash).Error; err != nil {
		t.Fatalf("registered user lookup error = %v", err)
	}
	if registeredHash == "" || registeredHash == registeredPassword {
		t.Fatal("registration did not persist a password hash")
	}
	duplicateResponse := doAuthRequest(t, client, http.MethodPost, registerURL, `{"username":"`+registeredUsername+`","password":"`+registeredPassword+`"}`, nil)
	if duplicateResponse.status != http.StatusUnprocessableEntity || duplicateResponse.envelope.Code != 10001 {
		t.Fatalf("duplicate register status/code = %d/%d, body=%s", duplicateResponse.status, duplicateResponse.envelope.Code, duplicateResponse.body)
	}

	requestHeaders := map[string]string{
		"X-Request-ID":  "it-req-" + driver,
		"X-Device-ID":   "device-" + driver,
		"X-Device-Name": "integration-browser",
		"X-Tenant-ID":   "default",
		"User-Agent":    "gin-vben-integration/0.3",
	}
	loginResponse := doAuthRequest(t, client, http.MethodPost, loginURL, `{"username":"`+username+`","password":"`+password+`"}`, nil, requestHeaders)
	if loginResponse.status != http.StatusOK || loginResponse.envelope.Code != 0 {
		t.Fatalf("login status/code = %d/%d, body=%s", loginResponse.status, loginResponse.envelope.Code, loginResponse.body)
	}
	if loginResponse.envelope.Data.AccessToken == "" {
		t.Fatal("login response did not include access token")
	}
	loginCookie := requireRefreshCookie(t, loginResponse.cookies)
	oldRefresh := cloneCookie(loginCookie)

	// Register exact Redis cleanup once the session ID is known.  Cleanup never
	// touches unrelated keys or performs a global database reset.
	tokens := authplatform.NewJWTServiceWithOptions(
		[]byte(cfg.Auth.JWTSecret), cfg.Auth.AccessTTL, cfg.Auth.RefreshTTL, cfg.Auth.Issuer, cfg.Auth.Audience,
	)
	claims, err := tokens.Parse(oldRefresh.Value)
	if err != nil {
		t.Fatalf("parse login refresh token error = %v", err)
	}
	var sessionCount int64
	if err := app.Database().Read(ctx).Table("auth_sessions").Where("id = ?", claims.SessionID).Count(&sessionCount).Error; err != nil {
		t.Fatalf("count durable auth session error = %v", err)
	}
	if sessionCount != 1 {
		t.Fatalf("durable auth session count = %d, want 1", sessionCount)
	}
	durableSessions := authplatform.NewGORMSessionStore(app.Database())
	storedSession, err := durableSessions.Get(tenant.WithContext(ctx, tenant.Context{TenantID: "default"}), claims.SessionID)
	if err != nil || !storedSession.MatchesRefreshJTI(claims.TokenID) {
		t.Fatalf("durable session lookup = %+v err=%v", storedSession, err)
	}
	if storedSession.DeviceID != requestHeaders["X-Device-ID"] || storedSession.DeviceName != requestHeaders["X-Device-Name"] || storedSession.IPAddress == "" || storedSession.UserAgent != requestHeaders["User-Agent"] {
		t.Fatalf("durable request metadata = %+v", storedSession)
	}
	var loginAudit struct {
		EventType string
		Outcome   string
		RequestID string
		IPAddress string
		UserAgent string
		SessionID string
	}
	if err := app.Database().Read(ctx).Table("auth_audit_events").Where("session_id = ? AND event_type = ?", claims.SessionID, "auth.login").Order("id DESC").Take(&loginAudit).Error; err != nil {
		t.Fatalf("login audit lookup error = %v", err)
	}
	if loginAudit.Outcome != "success" || loginAudit.RequestID != requestHeaders["X-Request-ID"] || loginAudit.UserAgent != requestHeaders["User-Agent"] || loginAudit.SessionID != claims.SessionID {
		t.Fatalf("login audit = %+v", loginAudit)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = app.Database().Write(cleanupCtx).Exec("DELETE FROM auth_sessions WHERE id = ?", claims.SessionID).Error
		_ = app.Database().Write(cleanupCtx).Exec("DELETE FROM auth_audit_events WHERE session_id = ?", claims.SessionID).Error
	})
	sessionKey, err := app.Redis().Key("auth-session", claims.SessionID)
	if err != nil {
		t.Fatalf("build session cleanup key error = %v", err)
	}
	redisCleanupKeys = append(redisCleanupKeys, sessionKey)
	for _, logical := range []string{"account:" + username, "ip:127.0.0.1"} {
		digest := sha256.Sum256([]byte(logical))
		rateKey, keyErr := app.Redis().Key("auth-rate", hex.EncodeToString(digest[:]))
		if keyErr != nil {
			t.Fatalf("build rate-limit cleanup key error = %v", keyErr)
		}
		redisCleanupKeys = append(redisCleanupKeys, rateKey)
	}

	refreshResponse := doAuthRequest(t, client, http.MethodPost, refreshURL, "", loginCookie, requestHeaders)
	if refreshResponse.status != http.StatusOK || refreshResponse.envelope.Code != 0 {
		t.Fatalf("refresh status/code = %d/%d, body=%s", refreshResponse.status, refreshResponse.envelope.Code, refreshResponse.body)
	}
	if refreshResponse.envelope.Data.AccessToken == "" {
		t.Fatal("refresh response did not include access token")
	}
	rotatedCookie := requireRefreshCookie(t, refreshResponse.cookies)
	if rotatedCookie.Value == oldRefresh.Value {
		t.Fatal("refresh did not rotate the refresh cookie")
	}
	rotatedClaims, err := tokens.Parse(rotatedCookie.Value)
	if err != nil {
		t.Fatalf("parse rotated refresh token error = %v", err)
	}
	storedSession, err = durableSessions.Get(tenant.WithContext(ctx, tenant.Context{TenantID: "default"}), claims.SessionID)
	if err != nil || !storedSession.MatchesRefreshJTI(rotatedClaims.TokenID) {
		t.Fatalf("rotated durable session lookup = %+v err=%v", storedSession, err)
	}

	replayResponse := doAuthRequest(t, client, http.MethodPost, refreshURL, "", oldRefresh, requestHeaders)
	if replayResponse.status != http.StatusUnauthorized || replayResponse.envelope.Code != 20000 {
		t.Fatalf("old refresh replay status/code = %d/%d, body=%s", replayResponse.status, replayResponse.envelope.Code, replayResponse.body)
	}

	logoutResponse := doAuthRequest(t, client, http.MethodPost, logoutURL, "", rotatedCookie, requestHeaders)
	if logoutResponse.status != http.StatusOK || logoutResponse.envelope.Code != 0 {
		t.Fatalf("logout status/code = %d/%d, body=%s", logoutResponse.status, logoutResponse.envelope.Code, logoutResponse.body)
	}
	storedSession, err = durableSessions.Get(tenant.WithContext(ctx, tenant.Context{TenantID: "default"}), claims.SessionID)
	if err != nil || !storedSession.Revoked {
		t.Fatalf("revoked durable session lookup = %+v err=%v", storedSession, err)
	}
	if cleared := findCookie(logoutResponse.cookies, cfg.Auth.RefreshCookieName); cleared == nil || cleared.MaxAge >= 0 || cleared.Value != "" {
		t.Fatalf("logout did not clear refresh cookie: %+v", cleared)
	}

	revokedResponse := doAuthRequest(t, client, http.MethodPost, refreshURL, "", rotatedCookie, requestHeaders)
	if revokedResponse.status != http.StatusUnauthorized || revokedResponse.envelope.Code != 20000 {
		t.Fatalf("revoked refresh status/code = %d/%d, body=%s", revokedResponse.status, revokedResponse.envelope.Code, revokedResponse.body)
	}

	// A second session verifies the user-scoped device-session endpoint without
	// conflating its runtime revocation path with the logout-cookie assertion.
	secondLogin := doAuthRequest(t, client, http.MethodPost, loginURL, `{"username":"`+username+`","password":"`+password+`"}`, nil, requestHeaders)
	if secondLogin.status != http.StatusOK {
		t.Fatalf("second login status = %d, body=%s", secondLogin.status, secondLogin.body)
	}
	secondCookie := requireRefreshCookie(t, secondLogin.cookies)
	secondClaims, err := tokens.Parse(secondCookie.Value)
	if err != nil {
		t.Fatalf("parse second refresh token error = %v", err)
	}
	secondSessionKey, err := app.Redis().Key("auth-session", secondClaims.SessionID)
	if err != nil {
		t.Fatalf("build second session cleanup key error = %v", err)
	}
	redisCleanupKeys = append(redisCleanupKeys, secondSessionKey)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_ = app.Database().Write(cleanupCtx).Exec("DELETE FROM auth_sessions WHERE id = ?", secondClaims.SessionID).Error
		_ = app.Database().Write(cleanupCtx).Exec("DELETE FROM auth_audit_events WHERE session_id = ?", secondClaims.SessionID).Error
	})
	secondHeaders := cloneHeaders(requestHeaders)
	secondHeaders["Authorization"] = "Bearer " + secondLogin.envelope.Data.AccessToken
	sessionsResponse := doAuthRequest(t, client, http.MethodGet, ts.URL+"/api/admin/v1/auth/sessions", "", nil, secondHeaders)
	if sessionsResponse.status != http.StatusOK || !strings.Contains(sessionsResponse.body, secondClaims.SessionID) {
		t.Fatalf("device sessions status/body = %d/%s", sessionsResponse.status, sessionsResponse.body)
	}
	revokeResponse := doAuthRequest(t, client, http.MethodDelete, ts.URL+"/api/admin/v1/auth/sessions/"+secondClaims.SessionID, "", nil, secondHeaders)
	if revokeResponse.status != http.StatusOK || revokeResponse.envelope.Code != 0 {
		t.Fatalf("device revoke status/code = %d/%d, body=%s", revokeResponse.status, revokeResponse.envelope.Code, revokeResponse.body)
	}
	secondRevoked := doAuthRequest(t, client, http.MethodPost, refreshURL, "", secondCookie, requestHeaders)
	if secondRevoked.status != http.StatusUnauthorized || secondRevoked.envelope.Code != 20000 {
		t.Fatalf("device-revoked refresh status/code = %d/%d, body=%s", secondRevoked.status, secondRevoked.envelope.Code, secondRevoked.body)
	}
}

type authResponse struct {
	status   int
	body     string
	cookies  []*http.Cookie
	envelope struct {
		Code int              `json:"code"`
		Data authResponseData `json:"data"`
	}
}

type authResponseData struct {
	AccessToken string
}

func (d *authResponseData) UnmarshalJSON(raw []byte) error {
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	var payload struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return err
	}
	d.AccessToken = payload.AccessToken
	return nil
}

func doAuthRequest(t *testing.T, client *http.Client, method, endpoint, payload string, cookie *http.Cookie, headerSets ...map[string]string) authResponse {
	t.Helper()
	var body io.Reader
	if payload != "" {
		body = strings.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, endpoint, body)
	if err != nil {
		t.Fatalf("new %s request error = %v", method, err)
	}
	if payload != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if len(headerSets) > 0 {
		for key, value := range headerSets[0] {
			req.Header.Set(key, value)
		}
	}
	if cookie != nil {
		req.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s error = %v", method, endpoint, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s response error = %v", method, err)
	}
	result := authResponse{status: resp.StatusCode, body: string(raw), cookies: resp.Cookies()}
	if err := json.Unmarshal(raw, &result.envelope); err != nil {
		t.Fatalf("decode %s response %q: %v", method, string(raw), err)
	}
	return result
}

func requireRefreshCookie(t *testing.T, cookies []*http.Cookie) *http.Cookie {
	t.Helper()
	cookie := findCookie(cookies, "refresh_token")
	if cookie == nil || strings.TrimSpace(cookie.Value) == "" {
		t.Fatalf("response did not include a refresh cookie: %+v", cookies)
	}
	return cookie
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie != nil && cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func cloneCookie(cookie *http.Cookie) *http.Cookie {
	if cookie == nil {
		return nil
	}
	copy := *cookie
	return &copy
}

func cloneHeaders(headers map[string]string) map[string]string {
	clone := make(map[string]string, len(headers))
	for key, value := range headers {
		clone[key] = value
	}
	return clone
}
