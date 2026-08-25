package dashboardhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dashboardapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/dashboard"
	iamapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/iam"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/gin-gonic/gin"
)

type iamReaderStub struct{}

type uncheckedIAMAccess struct{ user domain.User }

func (s uncheckedIAMAccess) GetAuthorizationUser(context.Context, string) (domain.User, error) {
	return s.user, nil
}
func (s uncheckedIAMAccess) ResolveSubject(ctx context.Context, user domain.User) (domain.Subject, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.Subject{}, err
	}
	return domain.Subject{UserID: user.ID, RoleIDs: append([]string(nil), user.RoleIDs...), Domain: scope.TenantID}, nil
}
func (uncheckedIAMAccess) Authorize(context.Context, domain.Subject, domain.Request) (bool, error) {
	return true, nil
}

func (iamReaderStub) ListUsersPage(context.Context, domain.UserListQuery) (domain.UserPage, error) {
	return domain.UserPage{Total: 0}, nil
}

func (iamReaderStub) ListRoles(context.Context) ([]domain.Role, error) { return []domain.Role{}, nil }

func TestDashboardSummaryRequiresPrincipalAndReturnsEnvelopeWithRealZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := dashboardapp.NewService(dashboardapp.Config{IAM: iamReaderStub{}})
	for _, tt := range []struct {
		name       string
		principal  bool
		permission bool
		wantStatus int
	}{
		{name: "overview permission", principal: true, permission: true, wantStatus: http.StatusOK},
		{name: "authenticated without overview permission", principal: true, wantStatus: http.StatusForbidden},
		{name: "missing principal", principal: false, wantStatus: http.StatusUnauthorized},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := iamapp.NewMemoryStore()
			if err := store.SaveUser(context.Background(), domain.User{ID: "u1", Username: "admin", TenantID: "tenant-a", Active: true, RoleIDs: []string{"role-overview"}}); err != nil {
				t.Fatal(err)
			}
			if err := store.SaveRole(context.Background(), domain.Role{ID: "role-overview", Name: "Overview", TenantID: "tenant-a", Active: true}); err != nil {
				t.Fatal(err)
			}
			if tt.permission {
				if err := store.SavePolicy(context.Background(), domain.Policy{RoleID: "role-overview", PermissionID: PermissionOverviewRead, Domain: "tenant-a", Method: http.MethodGet, Path: basePath, Effect: domain.EffectAllow}); err != nil {
					t.Fatal(err)
				}
			}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if tt.principal {
					c.Set("auth_claims", authdomain.Claims{Subject: "u1"})
				}
				c.Request = c.Request.WithContext(tenant.WithContext(c.Request.Context(), tenant.Context{TenantID: "tenant-a"}))
				c.Next()
			})
			RegisterRoutes(router, NewHandlerWithIAM(service, iamapp.NewService(store)))
			request := httptest.NewRequest(http.MethodGet, basePath, nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tt.wantStatus {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if tt.wantStatus == http.StatusOK && (!strings.Contains(response.Body.String(), `"code":0`) || !strings.Contains(response.Body.String(), `"value":0`)) {
				t.Fatalf("response body=%s", response.Body.String())
			}
		})
	}
}

func TestDashboardSummaryRejectsPermissionFromDisabledRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := iamapp.NewMemoryStore()
	if err := store.SaveUser(context.Background(), domain.User{
		ID: "u1", Username: "reader", TenantID: "tenant-a", Active: true, RoleIDs: []string{"role-overview"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRole(context.Background(), domain.Role{
		ID: "role-overview", Name: "Overview", TenantID: "tenant-a", Active: false,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePolicy(context.Background(), domain.Policy{
		RoleID: "role-overview", Domain: "tenant-a", Method: http.MethodGet, Path: basePath, Effect: domain.EffectAllow,
	}); err != nil {
		t.Fatal(err)
	}
	iamService := iamapp.NewService(store)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("auth_claims", authdomain.Claims{Subject: "u1"})
		c.Request = c.Request.WithContext(tenant.WithContext(c.Request.Context(), tenant.Context{TenantID: "tenant-a"}))
		c.Next()
	})
	RegisterRoutes(router, NewHandlerWithIAM(dashboardapp.NewService(dashboardapp.Config{IAM: iamService}), iamService))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, basePath, nil))
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDashboardSummaryRejectsCrossTenantAndCrossOrganizationPrincipal(t *testing.T) {
	service := dashboardapp.NewService(dashboardapp.Config{IAM: iamReaderStub{}})
	for _, scope := range []tenant.Context{
		{TenantID: "tenant-b", Organization: "org-a"},
		{TenantID: "tenant-a", Organization: "org-b"},
	} {
		router := gin.New()
		router.Use(func(c *gin.Context) {
			c.Set("auth_claims", authdomain.Claims{Subject: "u1"})
			c.Request = c.Request.WithContext(tenant.WithContext(c.Request.Context(), scope))
			c.Next()
		})
		access := uncheckedIAMAccess{user: domain.User{ID: "u1", Active: true, TenantID: "tenant-a", OrgID: "org-a"}}
		RegisterRoutes(router, NewHandlerWithIAM(service, access))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, basePath, nil))
		if response.Code != http.StatusForbidden {
			t.Fatalf("scope=%#v status=%d body=%s", scope, response.Code, response.Body.String())
		}
	}
}

func TestDashboardSummaryAllowsTenantWidePrincipalWithinScopedOrganization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := dashboardapp.NewService(dashboardapp.Config{IAM: iamReaderStub{}})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("auth_claims", authdomain.Claims{Subject: "u1"})
		scope := tenant.Context{TenantID: "tenant-a", Organization: "org-b"}
		c.Request = c.Request.WithContext(tenant.WithContext(c.Request.Context(), scope))
		c.Next()
	})
	access := uncheckedIAMAccess{user: domain.User{ID: "u1", Active: true, TenantID: "tenant-a"}}
	RegisterRoutes(router, NewHandlerWithIAM(service, access))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, basePath, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDashboardSummaryNarrowsMissingOrganizationBeforeRepositoryReads(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := iamapp.NewMemoryStore()
	for _, user := range []domain.User{
		{ID: "u1", Username: "admin-a", TenantID: "tenant-a", OrgID: "org-a", Active: true, RoleIDs: []string{"role-overview"}},
		{ID: "u2", Username: "reader-a", TenantID: "tenant-a", OrgID: "org-a", Active: true},
		{ID: "u3", Username: "reader-b", TenantID: "tenant-a", OrgID: "org-b", Active: true},
	} {
		if err := store.SaveUser(context.Background(), user); err != nil {
			t.Fatal(err)
		}
	}
	for _, role := range []domain.Role{
		{ID: "role-overview", Name: "Overview", TenantID: "tenant-a", OrgID: "org-a", Active: true},
		{ID: "role-global", Name: "Global", TenantID: "tenant-a", Active: true},
		{ID: "role-a", Name: "Org A", TenantID: "tenant-a", OrgID: "org-a", Active: true},
		{ID: "role-b", Name: "Org B", TenantID: "tenant-a", OrgID: "org-b", Active: true},
	} {
		if err := store.SaveRole(context.Background(), role); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SavePolicy(context.Background(), domain.Policy{
		RoleID: "role-overview", PermissionID: PermissionOverviewRead, Domain: "tenant-a",
		Method: http.MethodGet, Path: basePath, Effect: domain.EffectAllow,
	}); err != nil {
		t.Fatal(err)
	}
	iamService := iamapp.NewService(store)
	service := dashboardapp.NewService(dashboardapp.Config{IAM: iamService})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("auth_claims", authdomain.Claims{Subject: "u1"})
		c.Request = c.Request.WithContext(tenant.WithContext(c.Request.Context(), tenant.Context{TenantID: "tenant-a"}))
		c.Next()
	})
	RegisterRoutes(router, NewHandlerWithIAM(service, iamService))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, basePath, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Data dashboardapp.Summary `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	users := envelope.Data.Counts.Users
	if users.Value == nil || *users.Value != 2 {
		t.Fatalf("organization-scoped user count=%#v, body=%s", users, response.Body.String())
	}
	roles := envelope.Data.Counts.Roles
	if roles.Value == nil || *roles.Value != 3 {
		t.Fatalf("organization-scoped role count=%#v, body=%s", roles, response.Body.String())
	}
}
