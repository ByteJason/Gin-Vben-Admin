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

func TestIAMComponentRegistryIsGuardedAndReturnsAllowlist(t *testing.T) {
	store := iamapp.NewMemoryStore()
	if err := store.SaveUser(context.Background(), domain.User{ID: "u1", Username: "alice", Active: true}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePolicy(context.Background(), domain.Policy{Subject: "u1", Method: http.MethodGet, Path: "/api/admin/v1/iam/components", Effect: domain.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	r := newIAMTestRouter(store, authdomain.Claims{Subject: "u1", Type: authdomain.AccessToken, ExpiresAt: time.Now().Add(time.Minute)})
	request := httptest.NewRequest(http.MethodGet, "/api/admin/v1/iam/components", nil)
	request.Header.Set("Authorization", "Bearer test")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Code int `json:"code"`
		Data []struct {
			Component string `json:"component"`
			Kind      string `json:"kind"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != 0 || len(envelope.Data) == 0 || envelope.Data[0].Component == "" || envelope.Data[0].Kind == "" {
		t.Fatalf("envelope=%s", response.Body.String())
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

func TestIAMUserBatchStatusReturnsPerItemResultsWithoutCrossTenantLeak(t *testing.T) {
	store := iamapp.NewMemoryStore()
	admin := domain.User{ID: "admin", Username: "admin", TenantID: "default", Active: true}
	target := domain.User{ID: "target", Username: "alice", TenantID: "default", Active: true, PasswordHash: "secret-hash", RoleIDs: []string{"r-reader"}}
	otherTenant := domain.User{ID: "other", Username: "bob", TenantID: "tenant-b", Active: true}
	for _, user := range []domain.User{admin, target, otherTenant} {
		if err := store.SaveUser(context.Background(), user); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SavePolicy(context.Background(), domain.Policy{Subject: "admin", Method: http.MethodPost, Path: "/api/admin/v1/iam/users/batch-status", Effect: domain.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	r := newIAMTestRouter(store, authdomain.Claims{Subject: "admin", Type: authdomain.AccessToken, ExpiresAt: time.Now().Add(time.Minute)})
	body := `{"items":[{"id":"target","active":false},{"id":"missing","active":false},{"id":"other","active":false},{"id":"target","active":true}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/iam/users/batch-status", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("batch status=%d body=%s", resp.Code, resp.Body.String())
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Results []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
				Code   int    `json:"code"`
			} `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != 0 || len(envelope.Data.Results) != 4 || envelope.Data.Results[0].Status != "disabled" || envelope.Data.Results[0].Code != 0 || envelope.Data.Results[1].Status != "not_found" || envelope.Data.Results[1].Code != 10001 || envelope.Data.Results[2].Status != "not_found" || envelope.Data.Results[2].Code != 10001 || envelope.Data.Results[3].Status != "invalid" || envelope.Data.Results[3].Code != 10000 {
		t.Fatalf("batch envelope=%s", resp.Body.String())
	}
	stored, err := store.FindUser(context.Background(), target.ID)
	if err != nil || stored.Active || stored.PasswordHash != target.PasswordHash || len(stored.RoleIDs) != 1 {
		t.Fatalf("stored batch target=%+v err=%v", stored, err)
	}
}

func TestIAMUserResetPasswordReturnsNoCredentialAndPreservesState(t *testing.T) {
	store := iamapp.NewMemoryStore()
	admin := domain.User{ID: "admin", Username: "admin", TenantID: "default", Active: true}
	target := domain.User{ID: "target", Username: "alice", TenantID: "default", OrgID: "org-a", Active: false, PasswordHash: "old-hash", RoleIDs: []string{"r-reader"}}
	for _, user := range []domain.User{admin, target} {
		if err := store.SaveUser(context.Background(), user); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SavePolicy(context.Background(), domain.Policy{Subject: "admin", Method: http.MethodPost, Path: "/api/admin/v1/iam/users/:id/reset-password", Effect: domain.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	service := iamapp.NewService(store)
	service.SetPasswordHasher(testPasswordHasher{})
	r := newIAMTestRouter(store, authdomain.Claims{Subject: "admin", Type: authdomain.AccessToken, ExpiresAt: time.Now().Add(time.Minute)})
	// The default router builds its own service; wire the same hasher through a
	// handler so the endpoint exercises the production credential seam.
	r = gin.New()
	RegisterRoutes(r, NewHandler(service, authStub{claims: authdomain.Claims{Subject: "admin", Type: authdomain.AccessToken, ExpiresAt: time.Now().Add(time.Minute)}}))
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/iam/users/target/reset-password", strings.NewReader(`{"password":"new-password"}`))
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"code":0`) || strings.Contains(resp.Body.String(), "passwordHash") || strings.Contains(resp.Body.String(), "new-password") || strings.Contains(resp.Body.String(), "hash:new-password") {
		t.Fatalf("reset status/body = %d/%s", resp.Code, resp.Body.String())
	}
	stored, err := store.FindUser(context.Background(), target.ID)
	if err != nil || stored.PasswordHash != "hash:new-password" || stored.Active || len(stored.RoleIDs) != 1 || stored.RoleIDs[0] != "r-reader" {
		t.Fatalf("stored reset target = %+v err=%v", stored, err)
	}

	short := httptest.NewRequest(http.MethodPost, "/api/admin/v1/iam/users/target/reset-password", strings.NewReader(`{"password":"short"}`))
	short.Header.Set("Authorization", "Bearer test")
	short.Header.Set("Content-Type", "application/json")
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, short)
	if resp.Code != http.StatusBadRequest || !strings.Contains(resp.Body.String(), `"code":10000`) {
		t.Fatalf("short reset status/body = %d/%s", resp.Code, resp.Body.String())
	}
}

func TestIAMRoleAssignmentReplacesScopedMembersWithoutCredentialLeak(t *testing.T) {
	store := iamapp.NewMemoryStore()
	admin := domain.User{ID: "admin", Username: "admin", TenantID: "default", Active: true}
	targetA := domain.User{ID: "u1", Username: "alice", TenantID: "default", OrgID: "org-a", Active: true, PasswordHash: "hash-a", RoleIDs: []string{"role-editor", "role-other"}}
	targetB := domain.User{ID: "u2", Username: "bob", TenantID: "default", OrgID: "org-a", Active: true, PasswordHash: "hash-b"}
	otherOrg := domain.User{ID: "u3", Username: "carol", TenantID: "default", OrgID: "org-b", Active: true, PasswordHash: "hash-c", RoleIDs: []string{"role-editor"}}
	for _, user := range []domain.User{admin, targetA, targetB, otherOrg} {
		if err := store.SaveUser(context.Background(), user); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveRole(context.Background(), domain.Role{ID: "role-editor", Name: "Editor", Active: true, UserIDs: []string{"u1", "u3"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePolicy(context.Background(), domain.Policy{Subject: "admin", Method: http.MethodPut, Path: "/api/admin/v1/iam/roles/:id/users", Effect: domain.EffectAllow}); err != nil {
		t.Fatal(err)
	}
	r := newIAMTestRouter(store, authdomain.Claims{Subject: "admin", Type: authdomain.AccessToken, ExpiresAt: time.Now().Add(time.Minute)})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/v1/iam/roles/role-editor/users", strings.NewReader(`{"userIds":["u2"]}`))
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Org-ID", "org-a")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"code":0`) || strings.Contains(resp.Body.String(), "hash-a") || strings.Contains(resp.Body.String(), "passwordHash") {
		t.Fatalf("role assignment status/body = %d/%s", resp.Code, resp.Body.String())
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			ID      string   `json:"id"`
			UserIDs []string `json:"userIds"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil || envelope.Code != 0 || envelope.Data.ID != "role-editor" || len(envelope.Data.UserIDs) != 1 || envelope.Data.UserIDs[0] != "u2" {
		t.Fatalf("role assignment envelope = %s err=%v", resp.Body.String(), err)
	}
	storedA, _ := store.FindUser(context.Background(), targetA.ID)
	storedB, _ := store.FindUser(context.Background(), targetB.ID)
	storedOther, _ := store.FindUser(context.Background(), otherOrg.ID)
	if hasRoleID(storedA.RoleIDs, "role-editor") || !hasRoleID(storedA.RoleIDs, "role-other") || !hasRoleID(storedB.RoleIDs, "role-editor") || !hasRoleID(storedOther.RoleIDs, "role-editor") || storedA.PasswordHash != targetA.PasswordHash {
		t.Fatalf("role assignment relationships/credentials = a:%v b:%v other:%v hashes:%q", storedA.RoleIDs, storedB.RoleIDs, storedOther.RoleIDs, storedA.PasswordHash)
	}

	bad := httptest.NewRequest(http.MethodPut, "/api/admin/v1/iam/roles/role-editor/users", strings.NewReader(`{"userIds":["u3"]}`))
	bad.Header.Set("Authorization", "Bearer test")
	bad.Header.Set("Content-Type", "application/json")
	bad.Header.Set("X-Org-ID", "org-a")
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, bad)
	if resp.Code != http.StatusForbidden || !strings.Contains(resp.Body.String(), `"code":30000`) {
		t.Fatalf("cross-organization assignment status/body = %d/%s", resp.Code, resp.Body.String())
	}
}

func TestIAMRolePermissionAssignmentReplacesPoliciesAndReturnsIDs(t *testing.T) {
	store := iamapp.NewMemoryStore()
	admin := domain.User{ID: "admin", Username: "admin", TenantID: "default", OrgID: "org-a", Active: true}
	role := domain.Role{ID: "role-editor", Name: "Editor", TenantID: "default", OrgID: "org-a", Active: true}
	for _, permission := range []domain.Permission{
		{ID: "users.read", Name: "Read users", Method: http.MethodGet, Path: "/api/admin/v1/iam/users"},
		{ID: "users.write", Name: "Write users", Method: http.MethodPost, Path: "/api/admin/v1/iam/users"},
	} {
		if err := store.SavePermission(context.Background(), permission); err != nil {
			t.Fatal(err)
		}
	}
	for _, save := range []func() error{
		func() error { return store.SaveUser(context.Background(), admin) },
		func() error { return store.SaveRole(context.Background(), role) },
		func() error {
			return store.SavePolicy(context.Background(), domain.Policy{Subject: "admin", Method: http.MethodPut, Path: "/api/admin/v1/iam/roles/:id/permissions", Effect: domain.EffectAllow})
		},
		func() error {
			return store.SavePolicy(context.Background(), domain.Policy{RoleID: role.ID, PermissionID: "old", Method: http.MethodDelete, Path: "/old", Effect: domain.EffectAllow})
		},
	} {
		if err := save(); err != nil {
			t.Fatal(err)
		}
	}
	r := newIAMTestRouter(store, authdomain.Claims{Subject: admin.ID, Type: authdomain.AccessToken, ExpiresAt: time.Now().Add(time.Minute)})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/v1/iam/roles/role-editor/permissions", strings.NewReader(`{"permissionIds":["users.write","users.read"]}`))
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Org-ID", "org-a")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || strings.Contains(resp.Body.String(), "password") || strings.Contains(resp.Body.String(), "token") {
		t.Fatalf("permission assignment status/body = %d/%s", resp.Code, resp.Body.String())
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			ID            string   `json:"id"`
			PermissionIDs []string `json:"permissionIds"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil || envelope.Code != 0 || envelope.Data.ID != role.ID || len(envelope.Data.PermissionIDs) != 2 || envelope.Data.PermissionIDs[0] != "users.write" || envelope.Data.PermissionIDs[1] != "users.read" {
		t.Fatalf("permission assignment envelope = %s err=%v", resp.Body.String(), err)
	}
	policies, err := store.ListPolicies(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rolePolicyIDs := make(map[string]bool)
	for _, policy := range policies {
		if policy.RoleID == role.ID {
			rolePolicyIDs[policy.PermissionID] = true
		}
	}
	if len(rolePolicyIDs) != 2 || rolePolicyIDs["old"] {
		t.Fatalf("role policies after replacement = %+v", rolePolicyIDs)
	}

	bad := httptest.NewRequest(http.MethodPut, "/api/admin/v1/iam/roles/role-editor/permissions", strings.NewReader(`{"permissionIds":["missing"]}`))
	bad.Header.Set("Authorization", "Bearer test")
	bad.Header.Set("Content-Type", "application/json")
	bad.Header.Set("X-Org-ID", "org-a")
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, bad)
	if resp.Code != http.StatusNotFound || !strings.Contains(resp.Body.String(), `"code":10001`) {
		t.Fatalf("missing permission status/body = %d/%s", resp.Code, resp.Body.String())
	}
}

func TestIAMRoleDataScopeAssignmentReplacesScopedRows(t *testing.T) {
	store := iamapp.NewMemoryStore()
	admin := domain.User{ID: "admin", Username: "admin", TenantID: "default", OrgID: "org-a", Active: true}
	role := domain.Role{ID: "role-editor", Name: "Editor", TenantID: "default", OrgID: "org-a", Active: true}
	for _, save := range []func() error{
		func() error { return store.SaveUser(context.Background(), admin) },
		func() error { return store.SaveRole(context.Background(), role) },
		func() error {
			return store.SavePolicy(context.Background(), domain.Policy{Subject: "admin", Method: http.MethodPut, Path: "/api/admin/v1/iam/roles/:id/data-scopes", Effect: domain.EffectAllow})
		},
		func() error {
			return store.SaveDataScope(context.Background(), domain.DataScope{RoleID: role.ID, Domain: "default", OrgID: "org-a", Resource: "old", Scope: domain.ScopeOwn})
		},
	} {
		if err := save(); err != nil {
			t.Fatal(err)
		}
	}
	r := newIAMTestRouter(store, authdomain.Claims{Subject: admin.ID, Type: authdomain.AccessToken, ExpiresAt: time.Now().Add(time.Minute)})
	req := httptest.NewRequest(http.MethodPut, "/api/admin/v1/iam/roles/role-editor/data-scopes", strings.NewReader(`{"scopes":[{"resource":"orders","scope":"custom","ids":["order-1"]},{"resource":"teams","scope":"org","ids":["org-a"]}]}`))
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Org-ID", "org-a")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"code":0`) || strings.Contains(resp.Body.String(), "password") {
		t.Fatalf("data scope assignment status/body = %d/%s", resp.Code, resp.Body.String())
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			ID        string       `json:"id"`
			DataScope domain.Scope `json:"dataScope"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &envelope); err != nil || envelope.Code != 0 || envelope.Data.ID != role.ID || envelope.Data.DataScope != domain.ScopeCustom {
		t.Fatalf("data scope envelope = %s err=%v", resp.Body.String(), err)
	}
	scopes, err := store.ListDataScopes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	resources := map[string]bool{}
	for _, scope := range scopes {
		if scope.RoleID == role.ID {
			resources[scope.Resource] = true
		}
	}
	if len(resources) != 2 || resources["old"] {
		t.Fatalf("data scopes after replacement = %+v", resources)
	}

	bad := httptest.NewRequest(http.MethodPut, "/api/admin/v1/iam/roles/role-editor/data-scopes", strings.NewReader(`{"scopes":[{"resource":"orders","scope":"unknown","ids":[]}]}`))
	bad.Header.Set("Authorization", "Bearer test")
	bad.Header.Set("Content-Type", "application/json")
	bad.Header.Set("X-Org-ID", "org-a")
	resp = httptest.NewRecorder()
	r.ServeHTTP(resp, bad)
	if resp.Code != http.StatusBadRequest || !strings.Contains(resp.Body.String(), `"code":10000`) {
		t.Fatalf("invalid data scope status/body = %d/%s", resp.Code, resp.Body.String())
	}
}

func hasRoleID(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type testPasswordHasher struct{}

func (testPasswordHasher) Hash(password string) (string, error) { return "hash:" + password, nil }
