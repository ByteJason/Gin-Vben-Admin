package iamhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appauth "example.com/gin-vben-admin/server/internal/application/auth"
	iamapp "example.com/gin-vben-admin/server/internal/application/iam"
	"example.com/gin-vben-admin/server/internal/domain/authdomain"
	domain "example.com/gin-vben-admin/server/internal/domain/iam"
	"github.com/gin-gonic/gin"
)

type authStub struct{ claims authdomain.Claims }

func (a authStub) Login(context.Context, string, string) (authdomain.TokenPair, error) {
	return authdomain.TokenPair{}, nil
}
func (a authStub) Refresh(context.Context, string) (authdomain.TokenPair, error) {
	return authdomain.TokenPair{}, nil
}
func (a authStub) Logout(context.Context, string) error           { return nil }
func (a authStub) VerifyAccess(string) (authdomain.Claims, error) { return a.claims, nil }

var _ appauth.AuthService = authStub{}

func newIAMTestRouter(store *iamapp.MemoryStore, claims authdomain.Claims) *gin.Engine {
	r := gin.New()
	RegisterRoutes(r, NewHandler(iamapp.NewService(store), authStub{claims: claims}))
	return r
}

func TestIAMRoutesRequireBearer(t *testing.T) {
	r := newIAMTestRouter(iamapp.NewMemoryStore(), authdomain.Claims{Subject: "u1"})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/iam/users", nil)
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestIAMUserListUsesRolePolicyAndDefaultDeny(t *testing.T) {
	store := iamapp.NewMemoryStore()
	if err := store.SaveUser(context.Background(), domain.User{ID: "u1", Username: "alice", Active: true, RoleIDs: []string{"r-reader"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePolicy(context.Background(), domain.Policy{RoleID: "r-reader", Method: http.MethodGet, Path: "/api/admin/v1/iam/users", Effect: domain.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveUser(context.Background(), domain.User{ID: "u2", Username: "bob", Active: true}); err != nil {
		t.Fatal(err)
	}
	r := newIAMTestRouter(store, authdomain.Claims{Subject: "u1", Type: authdomain.AccessToken, ExpiresAt: time.Now().Add(time.Minute)})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/iam/users", nil)
	req.Header.Set("Authorization", "Bearer test")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("allowed status=%d body=%s", resp.Code, resp.Body.String())
	}
	var envelope struct {
		Code int           `json:"code"`
		Data []domain.User `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil || envelope.Code != 0 || len(envelope.Data) != 2 {
		t.Fatalf("allowed envelope=%s err=%v", resp.Body.String(), err)
	}

	storeDenied := iamapp.NewMemoryStore()
	if err := storeDenied.SaveUser(context.Background(), domain.User{ID: "u1", Username: "alice", Active: true}); err != nil {
		t.Fatal(err)
	}
	r = newIAMTestRouter(storeDenied, authdomain.Claims{Subject: "u1", Type: authdomain.AccessToken, ExpiresAt: time.Now().Add(time.Minute)})
	req = httptest.NewRequest(http.MethodGet, "/api/admin/v1/iam/users", nil)
	req.Header.Set("Authorization", "Bearer test")
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden || !strings.Contains(resp.Body.String(), `"code":30000`) {
		t.Fatalf("default deny status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestIAMRoleCreateReturnsValidationError(t *testing.T) {
	store := iamapp.NewMemoryStore()
	_ = store.SaveUser(context.Background(), domain.User{ID: "u1", Username: "alice", Active: true})
	_ = store.SavePolicy(context.Background(), domain.Policy{Subject: "u1", Method: http.MethodPost, Path: "/api/admin/v1/iam/roles", Effect: domain.EffectAllow})
	r := newIAMTestRouter(store, authdomain.Claims{Subject: "u1", Type: authdomain.AccessToken, ExpiresAt: time.Now().Add(time.Minute)})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/iam/roles", strings.NewReader(`{"name":""}`))
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest || !strings.Contains(resp.Body.String(), `"code":10000`) {
		t.Fatalf("validation status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestIAMPolicyAndDataScopeCollections(t *testing.T) {
	store := iamapp.NewMemoryStore()
	_ = store.SaveUser(context.Background(), domain.User{ID: "u1", Username: "alice", Active: true})
	_ = store.SavePolicy(context.Background(), domain.Policy{Subject: "u1", Method: http.MethodGet, Path: "/api/admin/v1/iam/policies", Effect: domain.EffectAllow})
	_ = store.SavePolicy(context.Background(), domain.Policy{Subject: "u1", Method: http.MethodGet, Path: "/api/admin/v1/iam/data-scopes", Effect: domain.EffectAllow})
	_ = store.SaveDataScope(context.Background(), domain.DataScope{Subject: "u1", Resource: "orders", Scope: domain.ScopeOwn})
	r := newIAMTestRouter(store, authdomain.Claims{Subject: "u1", Type: authdomain.AccessToken, ExpiresAt: time.Now().Add(time.Minute)})
	for _, path := range []string{"/api/admin/v1/iam/policies", "/api/admin/v1/iam/data-scopes"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer test")
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)
		if resp.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, resp.Code, resp.Body.String())
		}
	}
}
