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
	httpmiddleware "example.com/gin-vben-admin/server/internal/transport/http/middleware"
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

func newIAMTestRouterWithTenantPolicy(store *iamapp.MemoryStore, claims authdomain.Claims, policy httpmiddleware.TenantPolicy) *gin.Engine {
	r := gin.New()
	RegisterRoutes(r, NewHandler(iamapp.NewService(store), authStub{claims: claims}), policy)
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

func TestIAMCurrentUserReturnsVersionedProfile(t *testing.T) {
	store := iamapp.NewMemoryStore()
	if err := store.SaveUser(context.Background(), domain.User{
		ID: "u1", Username: "alice", DisplayName: "Alice", Active: true, RoleIDs: []string{"r-admin"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePolicy(context.Background(), domain.Policy{
		Subject: "u1", Method: http.MethodGet, Path: "/api/admin/v1/iam/me", Effect: domain.EffectAllow,
	}); err != nil {
		t.Fatal(err)
	}
	r := newIAMTestRouter(store, authdomain.Claims{Subject: "u1", Type: authdomain.AccessToken, ExpiresAt: time.Now().Add(time.Minute)})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/iam/me", nil)
	req.Header.Set("Authorization", "Bearer test")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			UserID   string   `json:"userId"`
			Username string   `json:"username"`
			RealName string   `json:"realName"`
			Roles    []string `json:"roles"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != 0 || envelope.Data.UserID != "u1" || envelope.Data.Username != "alice" || envelope.Data.RealName != "Alice" || len(envelope.Data.Roles) != 1 || envelope.Data.Roles[0] != "r-admin" {
		t.Fatalf("profile envelope=%s", resp.Body.String())
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
		Code int `json:"code"`
		Data struct {
			Items []domain.User `json:"items"`
			Total int           `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil || envelope.Code != 0 || envelope.Data.Total != 2 || len(envelope.Data.Items) != 2 {
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

func TestIAMUsesValidatedTenantContextInsteadOfRawHeader(t *testing.T) {
	store := iamapp.NewMemoryStore()
	_ = store.SaveUser(context.Background(), domain.User{ID: "u1", Username: "alice", Active: true})
	_ = store.SavePolicy(context.Background(), domain.Policy{Subject: "u1", Domain: "tenant-a", Method: http.MethodGet, Path: "/api/admin/v1/iam/users", Effect: domain.EffectAllow})
	r := newIAMTestRouterWithTenantPolicy(store, authdomain.Claims{Subject: "u1", Type: authdomain.AccessToken, ExpiresAt: time.Now().Add(time.Minute)}, httpmiddleware.TenantPolicy{Mode: "multi"})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/iam/users", nil)
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("X-Tenant-ID", "tenant-a")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("matching tenant status=%d body=%s", resp.Code, resp.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/v1/iam/users", nil)
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("X-Tenant-ID", "tenant-b")
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden || !strings.Contains(resp.Body.String(), `"code":30000`) {
		t.Fatalf("mismatched tenant status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestIAMUserListSupportsBoundedPaginationAndSearch(t *testing.T) {
	store := iamapp.NewMemoryStore()
	for _, user := range []domain.User{
		{ID: "1", Username: "alice", DisplayName: "Alice", TenantID: "default", Active: true},
		{ID: "2", Username: "albert", DisplayName: "Albert", TenantID: "default", Active: true},
		{ID: "3", Username: "bob", DisplayName: "Bob", TenantID: "default", Active: false},
	} {
		if err := store.SaveUser(context.Background(), user); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SavePolicy(context.Background(), domain.Policy{Subject: "u1", Method: http.MethodGet, Path: "/api/admin/v1/iam/users", Effect: domain.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveUser(context.Background(), domain.User{ID: "u1", Username: "admin", TenantID: "default", Active: true}); err != nil {
		t.Fatal(err)
	}
	r := newIAMTestRouter(store, authdomain.Claims{Subject: "u1", Type: authdomain.AccessToken, ExpiresAt: time.Now().Add(time.Minute)})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/iam/users?page=2&pageSize=1&keyword=al&status=active&sort=username", nil)
	req.Header.Set("Authorization", "Bearer test")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Items []struct {
				Username string `json:"username"`
			} `json:"items"`
			Total    int `json:"total"`
			Page     int `json:"page"`
			PageSize int `json:"pageSize"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != 0 || envelope.Data.Total != 2 || envelope.Data.Page != 2 || envelope.Data.PageSize != 1 || len(envelope.Data.Items) != 1 || envelope.Data.Items[0].Username != "alice" {
		t.Fatalf("page envelope=%s", resp.Body.String())
	}
}

func TestIAMUserListRejectsInvalidPagination(t *testing.T) {
	store := iamapp.NewMemoryStore()
	_ = store.SaveUser(context.Background(), domain.User{ID: "u1", Username: "admin", TenantID: "default", Active: true})
	_ = store.SavePolicy(context.Background(), domain.Policy{Subject: "u1", Method: http.MethodGet, Path: "/api/admin/v1/iam/users", Effect: domain.EffectAllow})
	r := newIAMTestRouter(store, authdomain.Claims{Subject: "u1", Type: authdomain.AccessToken, ExpiresAt: time.Now().Add(time.Minute)})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/iam/users?pageSize=101", nil)
	req.Header.Set("Authorization", "Bearer test")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest || !strings.Contains(resp.Body.String(), `"code":10000`) {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
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

func TestIAMUserDetailCreateAndUpdateHidePasswordAndMapConflict(t *testing.T) {
	store := iamapp.NewMemoryStore()
	service := iamapp.NewService(store)
	service.SetPasswordHasher(testPasswordHasher{})
	_ = store.SaveUser(context.Background(), domain.User{ID: "admin", Username: "admin", TenantID: "default", Active: true})
	for _, policy := range []domain.Policy{
		{Subject: "admin", Method: http.MethodPost, Path: "/api/admin/v1/iam/users", Effect: domain.EffectAllow},
		{Subject: "admin", Method: http.MethodGet, Path: "/api/admin/v1/iam/users/:id", Effect: domain.EffectAllow},
		{Subject: "admin", Method: http.MethodPatch, Path: "/api/admin/v1/iam/users/:id", Effect: domain.EffectAllow},
	} {
		if err := store.SavePolicy(context.Background(), policy); err != nil {
			t.Fatal(err)
		}
	}
	r := gin.New()
	RegisterRoutes(r, NewHandler(service, authStub{claims: authdomain.Claims{Subject: "admin", Type: authdomain.AccessToken, ExpiresAt: time.Now().Add(time.Minute)}}))

	create := httptest.NewRequest(http.MethodPost, "/api/admin/v1/iam/users", strings.NewReader(`{"username":"alice","password":"correct-password","email":"alice@example.test"}`))
	create.Header.Set("Authorization", "Bearer test")
	create.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, create)
	if resp.Code != http.StatusOK || strings.Contains(resp.Body.String(), "passwordHash") || strings.Contains(resp.Body.String(), "correct-password") {
		t.Fatalf("create status/body = %d/%s", resp.Code, resp.Body.String())
	}
	var created struct {
		Code int                           `json:"code"`
		Data struct{ ID, Username string } `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &created); err != nil || created.Code != 0 || created.Data.ID == "" || created.Data.Username != "alice" {
		t.Fatalf("created envelope = %s err=%v", resp.Body.String(), err)
	}

	get := httptest.NewRequest(http.MethodGet, "/api/admin/v1/iam/users/"+created.Data.ID, nil)
	get.Header.Set("Authorization", "Bearer test")
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, get)
	if resp.Code != http.StatusOK || strings.Contains(resp.Body.String(), "passwordHash") {
		t.Fatalf("get status/body = %d/%s", resp.Code, resp.Body.String())
	}

	patch := httptest.NewRequest(http.MethodPatch, "/api/admin/v1/iam/users/"+created.Data.ID, strings.NewReader(`{"nickname":"Alice A"}`))
	patch.Header.Set("Authorization", "Bearer test")
	patch.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, patch)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "Alice A") {
		t.Fatalf("patch status/body = %d/%s", resp.Code, resp.Body.String())
	}

	duplicate := httptest.NewRequest(http.MethodPost, "/api/admin/v1/iam/users", strings.NewReader(`{"username":"ALICE","password":"another-password"}`))
	duplicate.Header.Set("Authorization", "Bearer test")
	duplicate.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, duplicate)
	if resp.Code != http.StatusConflict || !strings.Contains(resp.Body.String(), `"code":10011`) {
		t.Fatalf("duplicate status/body = %d/%s", resp.Code, resp.Body.String())
	}
}

func TestIAMUserDeleteSoftDeletesAndIsIdempotent(t *testing.T) {
	store := iamapp.NewMemoryStore()
	admin := domain.User{ID: "admin", Username: "admin", TenantID: "default", Active: true}
	target := domain.User{ID: "target", Username: "alice", TenantID: "default", Active: true, PasswordHash: "secret-hash", RoleIDs: []string{"r-reader"}}
	if err := store.SaveUser(context.Background(), admin); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveUser(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePolicy(context.Background(), domain.Policy{Subject: "admin", Method: http.MethodDelete, Path: "/api/admin/v1/iam/users/:id", Effect: domain.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	r := newIAMTestRouter(store, authdomain.Claims{Subject: "admin", Type: authdomain.AccessToken, ExpiresAt: time.Now().Add(time.Minute)})
	request := func(id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, "/api/admin/v1/iam/users/"+id, nil)
		req.Header.Set("Authorization", "Bearer test")
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)
		return resp
	}
	if resp := request(target.ID); resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"code":0`) || strings.Contains(resp.Body.String(), "secret-hash") {
		t.Fatalf("delete status/body = %d/%s", resp.Code, resp.Body.String())
	}
	stored, err := store.FindUser(context.Background(), target.ID)
	if err != nil || stored.Active || stored.PasswordHash != target.PasswordHash || len(stored.RoleIDs) != 1 {
		t.Fatalf("stored deleted user = %+v err=%v", stored, err)
	}
	if resp := request(target.ID); resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"code":0`) {
		t.Fatalf("idempotent delete status/body = %d/%s", resp.Code, resp.Body.String())
	}
	if resp := request("missing"); resp.Code != http.StatusNotFound || !strings.Contains(resp.Body.String(), `"code":10001`) {
		t.Fatalf("missing delete status/body = %d/%s", resp.Code, resp.Body.String())
	}
}

type testPasswordHasher struct{}

func (testPasswordHasher) Hash(password string) (string, error) { return "hash:" + password, nil }
