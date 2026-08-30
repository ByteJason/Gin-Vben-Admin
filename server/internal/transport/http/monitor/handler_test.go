package monitorhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	iamapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/iam"
	monitorapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/monitor"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/gin-gonic/gin"
)

type uncheckedIAMAccess struct{ user domain.User }

type canonicalOnlyIAMAccess struct {
	user  domain.User
	paths []string
}

type scopeObservingResourceProbe struct{ organization chan string }

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

func (a *canonicalOnlyIAMAccess) GetAuthorizationUser(context.Context, string) (domain.User, error) {
	return a.user, nil
}
func (a *canonicalOnlyIAMAccess) ResolveSubject(ctx context.Context, user domain.User) (domain.Subject, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return domain.Subject{}, err
	}
	return domain.Subject{UserID: user.ID, Domain: scope.TenantID}, nil
}
func (a *canonicalOnlyIAMAccess) Authorize(_ context.Context, _ domain.Subject, request domain.Request) (bool, error) {
	a.paths = append(a.paths, request.Path)
	return request.Path == serverStatusPath, nil
}

func (p scopeObservingResourceProbe) CPU(ctx context.Context) (monitorapp.HostMetric, error) {
	scope, _ := tenant.RequireContext(ctx)
	p.organization <- scope.Organization
	return monitorapp.HostMetric{Status: monitorapp.StatusOK}, nil
}

func (scopeObservingResourceProbe) Memory(context.Context) (monitorapp.HostMetric, error) {
	return monitorapp.HostMetric{Status: monitorapp.StatusOK}, nil
}

func (scopeObservingResourceProbe) Disk(context.Context, string, monitorapp.MetricScope) (monitorapp.HostMetric, error) {
	return monitorapp.HostMetric{Status: monitorapp.StatusOK}, nil
}

func TestMonitorHandlerRequiresIAMPermissionAndAllowsSuperadminWildcard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name       string
		roleID     string
		policy     *domain.Policy
		wantStatus int
	}{
		{
			name: "explicit ops monitor permission", roleID: "role-ops",
			policy:     &domain.Policy{RoleID: "role-ops", PermissionID: PermissionRead, Domain: "tenant-a", Method: http.MethodGet, Path: basePath, Effect: domain.EffectAllow},
			wantStatus: http.StatusOK,
		},
		{
			name: "superadmin wildcard", roleID: "role-super-admin",
			policy:     &domain.Policy{RoleID: "role-super-admin", Domain: "", Method: "*", Path: "*", Effect: domain.EffectAllow},
			wantStatus: http.StatusOK,
		},
		{name: "platform admin identity without IAM policy", roleID: "role-unprivileged", wantStatus: http.StatusForbidden},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := iamapp.NewMemoryStore()
			if err := store.SaveUser(context.Background(), domain.User{ID: "u1", Username: "admin", TenantID: "tenant-a", Active: true, RoleIDs: []string{tt.roleID}}); err != nil {
				t.Fatal(err)
			}
			if err := store.SaveRole(context.Background(), domain.Role{ID: tt.roleID, Name: tt.roleID, TenantID: "tenant-a", Active: true}); err != nil {
				t.Fatal(err)
			}
			if tt.policy != nil {
				if err := store.SavePolicy(context.Background(), *tt.policy); err != nil {
					t.Fatal(err)
				}
			}
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("auth_claims", authdomain.Claims{Subject: "u1"})
				ctx := tenant.WithContext(c.Request.Context(), tenant.Context{TenantID: "tenant-a", PlatformAdmin: true})
				c.Request = c.Request.WithContext(ctx)
				c.Next()
			})
			RegisterRoutes(router, NewHandlerWithIAM(monitorapp.NewService(monitorapp.Config{}), iamapp.NewService(store)))
			request := httptest.NewRequest(http.MethodGet, basePath, nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tt.wantStatus {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "policy") {
				t.Fatalf("authorization internals leaked: %s", response.Body.String())
			}
		})
	}
}

func TestMonitorHandlerUsesOnlyActiveRolesAndPreservesDirectUserGrant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name       string
		roleActive bool
		direct     bool
		wantStatus int
	}{
		{name: "active role grant", roleActive: true, wantStatus: http.StatusOK},
		{name: "disabled role grant", wantStatus: http.StatusForbidden},
		{name: "direct user grant survives disabled role", direct: true, wantStatus: http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := iamapp.NewMemoryStore()
			if err := store.SaveUser(context.Background(), domain.User{
				ID: "u1", Username: "operator", TenantID: "tenant-a", Active: true, RoleIDs: []string{"role-ops"},
			}); err != nil {
				t.Fatal(err)
			}
			if err := store.SaveRole(context.Background(), domain.Role{
				ID: "role-ops", Name: "Operations", TenantID: "tenant-a", Active: tt.roleActive,
			}); err != nil {
				t.Fatal(err)
			}
			policy := domain.Policy{Domain: "tenant-a", Method: http.MethodGet, Path: basePath, Effect: domain.EffectAllow}
			if tt.direct {
				policy.Subject = "u1"
			} else {
				policy.RoleID = "role-ops"
			}
			if err := store.SavePolicy(context.Background(), policy); err != nil {
				t.Fatal(err)
			}
			service := iamapp.NewService(store)
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("auth_claims", authdomain.Claims{Subject: "u1"})
				c.Request = c.Request.WithContext(tenant.WithContext(c.Request.Context(), tenant.Context{TenantID: "tenant-a"}))
				c.Next()
			})
			RegisterRoutes(router, NewHandlerWithIAM(monitorapp.NewService(monitorapp.Config{}), service))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, basePath, nil))
			if response.Code != tt.wantStatus {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestMonitorHandlerLocalModeRequiresPlatformAdminAndNoAuthenticatedPrincipal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name          string
		platformAdmin bool
		claims        bool
		wantStatus    int
	}{
		{name: "local platform admin", platformAdmin: true, wantStatus: http.StatusOK},
		{name: "local tenant admin", wantStatus: http.StatusForbidden},
		{name: "authenticated route must wire IAM", platformAdmin: true, claims: true, wantStatus: http.StatusServiceUnavailable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(func(c *gin.Context) {
				if tt.claims {
					c.Set("auth_claims", authdomain.Claims{Subject: "u1"})
				}
				ctx := tenant.WithContext(c.Request.Context(), tenant.Context{TenantID: "tenant-a", PlatformAdmin: tt.platformAdmin})
				c.Request = c.Request.WithContext(ctx)
				c.Next()
			})
			RegisterRoutes(router, NewHandler(monitorapp.NewService(monitorapp.Config{})))
			request := httptest.NewRequest(http.MethodGet, basePath, nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tt.wantStatus {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func TestServerStatusRouteKeepsMonitorCompatibilityAndCanonicalHints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		ctx := tenant.WithContext(c.Request.Context(), tenant.Context{TenantID: "tenant-a", PlatformAdmin: true})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	RegisterRoutes(router, NewHandler(monitorapp.NewService(monitorapp.Config{DataSource: "fixture", IsSynthetic: true})))
	for _, path := range []string{basePath, serverStatusPath} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("path=%s status=%d body=%s", path, response.Code, response.Body.String())
		}
		for _, fragment := range []string{`"dataSource":"fixture"`, `"isSynthetic":true`, `"timestamp":`, `"refreshIntervalSeconds":10`} {
			if !strings.Contains(response.Body.String(), fragment) {
				t.Fatalf("path=%s response missing %s: %s", path, fragment, response.Body.String())
			}
		}
	}
}

func TestServerStatusAuthorizationFallsBackToCanonicalPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("auth_claims", authdomain.Claims{Subject: "u1"})
		c.Request = c.Request.WithContext(tenant.WithContext(c.Request.Context(), tenant.Context{TenantID: "tenant-a"}))
		c.Next()
	})
	access := &canonicalOnlyIAMAccess{user: domain.User{ID: "u1", TenantID: "tenant-a", Active: true}}
	RegisterRoutes(router, NewHandlerWithIAM(monitorapp.NewService(monitorapp.Config{}), access))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, serverStatusPath, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(access.paths) != 2 || access.paths[0] != basePath || access.paths[1] != serverStatusPath {
		t.Fatalf("authorization paths = %#v", access.paths)
	}
}

func TestMonitorHandlerRejectsCrossTenantAndCrossOrganizationPrincipal(t *testing.T) {
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
		RegisterRoutes(router, NewHandlerWithIAM(monitorapp.NewService(monitorapp.Config{}), access))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, basePath, nil))
		if response.Code != http.StatusForbidden {
			t.Fatalf("scope=%#v status=%d body=%s", scope, response.Code, response.Body.String())
		}
	}
}

func TestMonitorHandlerAllowsTenantWidePrincipalWithinScopedOrganization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("auth_claims", authdomain.Claims{Subject: "u1"})
		scope := tenant.Context{TenantID: "tenant-a", Organization: "org-b"}
		c.Request = c.Request.WithContext(tenant.WithContext(c.Request.Context(), scope))
		c.Next()
	})
	access := uncheckedIAMAccess{user: domain.User{ID: "u1", Active: true, TenantID: "tenant-a"}}
	RegisterRoutes(router, NewHandlerWithIAM(monitorapp.NewService(monitorapp.Config{}), access))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, basePath, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestMonitorHandlerNarrowsMissingOrganizationToBoundPrincipal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	organizations := make(chan string, 1)
	service := monitorapp.NewService(monitorapp.Config{Resources: scopeObservingResourceProbe{organization: organizations}})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("auth_claims", authdomain.Claims{Subject: "u1"})
		c.Request = c.Request.WithContext(tenant.WithContext(c.Request.Context(), tenant.Context{TenantID: "tenant-a"}))
		c.Next()
	})
	access := uncheckedIAMAccess{user: domain.User{ID: "u1", Active: true, TenantID: "tenant-a", OrgID: "org-a"}}
	RegisterRoutes(router, NewHandlerWithIAM(service, access))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, basePath, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if organization := <-organizations; organization != "org-a" {
		t.Fatalf("downstream organization=%q, want org-a", organization)
	}
}
