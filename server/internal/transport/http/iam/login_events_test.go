package iamhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	auditapp "example.com/gin-vben-admin/server/internal/application/audit"
	iamapp "example.com/gin-vben-admin/server/internal/application/iam"
	"example.com/gin-vben-admin/server/internal/domain/authdomain"
	domain "example.com/gin-vben-admin/server/internal/domain/iam"
	"github.com/gin-gonic/gin"
)

func TestIAMLoginEventsReturnsScopedRedactedAuthRecords(t *testing.T) {
	store := iamapp.NewMemoryStore()
	for _, user := range []domain.User{
		{ID: "admin", Username: "admin", TenantID: "default", Active: true},
		{ID: "u1", Username: "alice", TenantID: "default", OrgID: "org-a", Active: true, PasswordHash: "hash"},
		{ID: "u2", Username: "bob", TenantID: "default", OrgID: "org-b", Active: true, PasswordHash: "other"},
	} {
		if err := store.SaveUser(context.Background(), user); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SavePolicy(context.Background(), domain.Policy{Subject: "admin", Method: http.MethodGet, Path: "/api/admin/v1/iam/users/:id/login-events", Effect: domain.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	auditService := auditapp.NewService(auditapp.NewMemoryRepository([]auditapp.Event{
		{ID: "e1", ActorID: "u1", Action: "login", Resource: "auth", Outcome: "success", RequestID: "r1", Details: map[string]any{"password": "secret"}, CreatedAt: time.Unix(3, 0)},
		{ID: "e2", ActorID: "u1", Action: "update", Resource: "settings", Outcome: "success", CreatedAt: time.Unix(2, 0)},
	}))
	r := gin.New()
	claims := authdomain.Claims{Subject: "admin", Type: authdomain.AccessToken, ExpiresAt: time.Now().Add(time.Minute)}
	RegisterRoutes(r, NewHandlerWithAudit(iamapp.NewService(store), authStub{claims: claims}, auditService))
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/iam/users/u1/login-events?limit=10", nil)
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("X-Org-ID", "org-a")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || strings.Contains(resp.Body.String(), "secret") || strings.Contains(resp.Body.String(), "passwordHash") {
		t.Fatalf("login events status/body = %d/%s", resp.Code, resp.Body.String())
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Items []auditapp.Event `json:"items"`
			Total int              `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil || envelope.Code != 0 || envelope.Data.Total != 1 || len(envelope.Data.Items) != 1 || envelope.Data.Items[0].ID != "e1" || envelope.Data.Items[0].Details["password"] != "[REDACTED]" {
		t.Fatalf("login events envelope = %s err=%v", resp.Body.String(), err)
	}

	invalid := httptest.NewRequest(http.MethodGet, "/api/admin/v1/iam/users/u1/login-events?limit=-1", nil)
	invalid.Header.Set("Authorization", "Bearer test")
	invalid.Header.Set("X-Org-ID", "org-a")
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, invalid)
	if resp.Code != http.StatusBadRequest || !strings.Contains(resp.Body.String(), `"code":10000`) {
		t.Fatalf("invalid login event filter status/body = %d/%s", resp.Code, resp.Body.String())
	}

	crossOrg := httptest.NewRequest(http.MethodGet, "/api/admin/v1/iam/users/u2/login-events", nil)
	crossOrg.Header.Set("Authorization", "Bearer test")
	crossOrg.Header.Set("X-Org-ID", "org-a")
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, crossOrg)
	if resp.Code != http.StatusForbidden || !strings.Contains(resp.Body.String(), `"code":30000`) {
		t.Fatalf("cross-org login event status/body = %d/%s", resp.Code, resp.Body.String())
	}
}
