package integration

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"example.com/gin-vben-admin/server/internal/bootstrap"
	"example.com/gin-vben-admin/server/internal/config"
	domain "example.com/gin-vben-admin/server/internal/domain/iam"
	"example.com/gin-vben-admin/server/internal/platform/authplatform"
	"example.com/gin-vben-admin/server/internal/platform/cache/redis"
	"example.com/gin-vben-admin/server/internal/platform/iamplatform"
	"example.com/gin-vben-admin/server/internal/platform/migration"
)

func TestRBACHTTPIntegration(t *testing.T) {
	if os.Getenv("DATA_PLATFORM_INTEGRATION") != "1" {
		t.Skip("set DATA_PLATFORM_INTEGRATION=1 to run RBAC HTTP integration")
	}
	for _, tc := range []struct {
		driver string
		dsn    string
	}{
		{driver: migration.DriverMySQL, dsn: requiredEnv(t, mysqlDSNEnv)},
		{driver: migration.DriverPostgres, dsn: requiredEnv(t, postgresDSNEnv)},
	} {
		t.Run(tc.driver, func(t *testing.T) {
			testRBACHTTP(t, tc.driver, tc.dsn, requiredEnv(t, redisAddrEnv))
		})
	}
}

func testRBACHTTP(t *testing.T, driver, dsn, redisAddr string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	runner, err := migration.New(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runner.Close() })
	if err := runner.Up(); err != nil {
		t.Fatal(err)
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
	cfg.Redis.Namespace = fmt.Sprintf("app:v1:it-rbac-http-%d", time.Now().UnixNano())
	cfg.Auth.Enabled = true
	cfg.Auth.JWTSecret = "integration-rbac-http-secret-012345678901234567890"
	cfg.Auth.AccessTTL = 5 * time.Minute
	cfg.Auth.RefreshTTL = 30 * time.Minute
	cfg.Auth.BcryptCost = 10
	cfg.Auth.SecureCookie = false
	app, err := bootstrap.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close() })

	suffix := fmt.Sprintf("%s_%d", driver, time.Now().UnixNano())
	username := "it_rbac_http_" + suffix
	roleID := "it-http-role-" + suffix
	hasher := authplatform.BcryptHasher{Cost: cfg.Auth.BcryptCost}
	hash, err := hasher.Hash("integration-password-012345")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Database().Write(ctx).Exec("INSERT INTO users (username, password_hash, status) VALUES (?, ?, ?)", username, hash, "active").Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		app.Database().Write(cleanupCtx).Exec("DELETE FROM roles WHERE id = ?", roleID)
		app.Database().Write(cleanupCtx).Exec("DELETE FROM users WHERE username = ?", username)
	})
	var userID uint64
	if err := app.Database().Read(ctx).Table("users").Where("username = ?", username).Pluck("id", &userID).Error; err != nil {
		t.Fatal(err)
	}
	persistence := iamplatform.NewGORMStore(app.Database())
	if err := persistence.SaveRole(ctx, iamRole(roleID)); err != nil {
		t.Fatal(err)
	}
	if err := persistence.SaveUser(ctx, iamUser(userID, username, roleID)); err != nil {
		t.Fatal(err)
	}
	if err := persistence.SavePolicy(ctx, iamPolicy(roleID)); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(app.HTTPServer().Handler)
	t.Cleanup(ts.Close)
	login := doAuthRequest(t, ts.Client(), http.MethodPost, ts.URL+"/api/admin/v1/auth/login", `{"username":"`+username+`","password":"integration-password-012345"}`, nil)
	if login.status != http.StatusOK || login.envelope.Data.AccessToken == "" {
		t.Fatalf("login status=%d body=%s", login.status, login.body)
	}
	access := login.envelope.Data.AccessToken
	t.Cleanup(func() {
		// Remove only the refresh session created by this test.
		if cookie := requireRefreshCookie(t, login.cookies); cookie != nil {
			claims, parseErr := authplatform.NewJWTServiceWithOptions([]byte(cfg.Auth.JWTSecret), cfg.Auth.AccessTTL, cfg.Auth.RefreshTTL, cfg.Auth.Issuer, cfg.Auth.Audience).Parse(cookie.Value)
			if parseErr == nil {
				key, keyErr := app.Redis().Key("auth-session", claims.SessionID)
				if keyErr == nil {
					_ = app.Redis().Delete(context.Background(), key)
				}
			}
		}
	})

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/admin/v1/iam/users", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"code":0`) {
		t.Fatalf("authorized IAM request status=%d body=%s", resp.StatusCode, body)
	}
}

func iamRole(id string) domain.Role {
	return domain.Role{ID: id, Name: "Integration HTTP Role", Active: true, DataScope: domain.ScopeOwn}
}
func iamUser(id uint64, username, roleID string) domain.User {
	return domain.User{ID: fmt.Sprint(id), Username: username, Active: true, RoleIDs: []string{roleID}}
}
func iamPolicy(roleID string) domain.Policy {
	return domain.Policy{RoleID: roleID, Method: http.MethodGet, Path: "/api/admin/v1/iam/users", Effect: domain.EffectAllow}
}
