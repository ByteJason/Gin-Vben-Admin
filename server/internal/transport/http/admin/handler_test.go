package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	auditapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/audit"
	appauth "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/auth"
	dictionaryapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/dictionary"
	iamapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/iam"
	settingsapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/settings"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/config"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	iamdomain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
	audithttp "github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/audit"
	authhttp "github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/auth"
	dictionaryhttp "github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/dictionary"
	httpmiddleware "github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/middleware"
	settingshttp "github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/settings"
	"github.com/gin-gonic/gin"
)

type routeAuthService struct{}

func (routeAuthService) Login(context.Context, string, string) (authdomain.TokenPair, error) {
	return authdomain.TokenPair{}, nil
}
func (routeAuthService) Refresh(context.Context, string) (authdomain.TokenPair, error) {
	return authdomain.TokenPair{}, nil
}
func (routeAuthService) Logout(context.Context, string) error { return nil }
func (routeAuthService) VerifyAccess(string) (authdomain.Claims, error) {
	return authdomain.Claims{Subject: "route-test"}, nil
}

var _ appauth.AuthService = routeAuthService{}

func TestRegisterRoutesWithIAMMountsAuxiliaryRoutesUnderAdminPrefixOnce(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authConfig := config.Default().Auth
	authConfig.Enabled = true
	authHandler := authhttp.NewHandler(routeAuthService{}, authConfig)
	settingsHandler := settingshttp.NewHandler(settingsapp.NewService(settingsapp.NewMemoryRepository(), nil, nil, nil))
	auditHandler := audithttp.NewHandler(auditapp.NewService(auditapp.NewMemoryRepository(nil)))

	RegisterRoutesWithIAM(r, authHandler, nil, AuxiliaryRoutes{Settings: settingsHandler, Audit: auditHandler})

	paths := make(map[string]bool)
	for _, route := range r.Routes() {
		paths[route.Method+" "+route.Path] = true
	}
	for _, want := range []string{
		"GET /api/admin/v1/settings",
		"GET /api/admin/v1/settings/:key",
		"GET /api/admin/v1/observability/settings/:key",
		"PUT /api/admin/v1/observability/settings/:key",
		"GET /api/admin/v1/audit/events",
	} {
		if !paths[want] {
			t.Fatalf("route %q is missing; registered routes: %#v", want, paths)
		}
	}
	for path := range paths {
		if len(path) >= len("/api/admin/v1/api/admin/v1") && containsDuplicateAdminPrefix(path) {
			t.Fatalf("auxiliary route duplicated admin prefix: %s", path)
		}
	}
}

func TestRegisterRoutesWithIAMAppliesTenantPolicyToProtectedAuxiliaryRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authConfig := config.Default().Auth
	authConfig.Enabled = true
	authHandler := authhttp.NewHandler(routeAuthService{}, authConfig)
	settingsHandler := settingshttp.NewHandler(settingsapp.NewService(settingsapp.NewMemoryRepository(), nil, nil, nil))
	policy := httpmiddleware.TenantPolicy{Mode: "multi"}
	RegisterRoutesWithIAM(r, authHandler, nil, AuxiliaryRoutes{Settings: settingsHandler, TenantPolicy: &policy})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/settings/site.name", nil)
	req.Header.Set("Authorization", "Bearer test")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest || !strings.Contains(resp.Body.String(), `"code":10000`) {
		t.Fatalf("missing tenant status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestRegisterRoutesWithIAMDeniesEveryAuxiliaryMethodWithoutPolicy(t *testing.T) {
	service := newAuxiliaryIAMService(t)
	router := newDictionaryAuthorizationRouter(t, service, httpmiddleware.TenantPolicy{Mode: "single", DefaultTenantID: "default"})

	for _, tt := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/admin/v1/dictionaries"},
		{method: http.MethodPost, path: "/api/admin/v1/dictionaries", body: `{"code":"regions","nameEnUS":"Regions"}`},
		{method: http.MethodPatch, path: "/api/admin/v1/dictionaries/types/regions", body: `{"code":"regions","nameEnUS":"Regions"}`},
		{method: http.MethodDelete, path: "/api/admin/v1/dictionaries/types/regions"},
	} {
		t.Run(tt.method, func(t *testing.T) {
			request := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			request.Header.Set("Authorization", "Bearer test")
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":30000`) {
				t.Fatalf("%s %s status=%d body=%s", tt.method, tt.path, response.Code, response.Body.String())
			}
		})
	}
}

func TestRegisterRoutesWithIAMAllowsExactAuxiliaryMethodAndFullPathPolicies(t *testing.T) {
	service := newAuxiliaryIAMService(t)
	store := service.Users.(*iamapp.MemoryStore)
	for _, policy := range []iamdomain.Policy{
		{Subject: "route-test", Domain: "default", Method: http.MethodGet, Path: "/api/admin/v1/dictionaries", Effect: iamdomain.EffectAllow},
		{Subject: "route-test", Domain: "default", Method: http.MethodPost, Path: "/api/admin/v1/dictionaries", Effect: iamdomain.EffectAllow},
		{Subject: "route-test", Domain: "default", Method: http.MethodPatch, Path: "/api/admin/v1/dictionaries/types/:code", Effect: iamdomain.EffectAllow},
		{Subject: "route-test", Domain: "default", Method: http.MethodDelete, Path: "/api/admin/v1/dictionaries/types/:code", Effect: iamdomain.EffectAllow},
	} {
		if err := store.SavePolicy(context.Background(), policy); err != nil {
			t.Fatal(err)
		}
	}
	router := newDictionaryAuthorizationRouter(t, service, httpmiddleware.TenantPolicy{Mode: "single", DefaultTenantID: "default"})

	requests := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/admin/v1/dictionaries"},
		{method: http.MethodPost, path: "/api/admin/v1/dictionaries", body: `{"code":"regions","nameEnUS":"Regions"}`},
		{method: http.MethodPatch, path: "/api/admin/v1/dictionaries/types/regions", body: `{"code":"regions","nameEnUS":"Updated regions"}`},
		{method: http.MethodDelete, path: "/api/admin/v1/dictionaries/types/regions"},
	}
	for _, tt := range requests {
		request := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
		request.Header.Set("Authorization", "Bearer test")
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s %s status=%d body=%s", tt.method, tt.path, response.Code, response.Body.String())
		}
	}
}

func TestRegisterRoutesWithIAMRejectsAuxiliaryCrossOrganizationPrincipal(t *testing.T) {
	service := newAuxiliaryIAMService(t)
	store := service.Users.(*iamapp.MemoryStore)
	if err := store.SavePolicy(context.Background(), iamdomain.Policy{
		Subject: "route-test", Domain: "default", Method: http.MethodGet,
		Path: "/api/admin/v1/dictionaries", Effect: iamdomain.EffectAllow,
	}); err != nil {
		t.Fatal(err)
	}
	router := newDictionaryAuthorizationRouter(t, service, httpmiddleware.TenantPolicy{Mode: "multi"})
	request := httptest.NewRequest(http.MethodGet, "/api/admin/v1/dictionaries", nil)
	request.Header.Set("Authorization", "Bearer test")
	request.Header.Set(httpmiddleware.TenantHeader, "default")
	request.Header.Set(httpmiddleware.OrganizationHeader, "org-b")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":30000`) {
		t.Fatalf("cross-organization status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRegisterRoutesWithIAMIsolatesObservabilitySettingsFromGeneralSettings(t *testing.T) {
	service := newAuxiliaryIAMService(t)
	store := service.Users.(*iamapp.MemoryStore)
	for _, policy := range []iamdomain.Policy{
		{Subject: "route-test", Domain: "default", Method: http.MethodGet, Path: "/api/admin/v1/observability/settings/*", Effect: iamdomain.EffectAllow},
		{Subject: "route-test", Domain: "default", Method: http.MethodPut, Path: "/api/admin/v1/observability/settings/*", Effect: iamdomain.EffectAllow},
	} {
		if err := store.SavePolicy(context.Background(), policy); err != nil {
			t.Fatal(err)
		}
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	authConfig := config.Default().Auth
	authConfig.Enabled = true
	authHandler := authhttp.NewHandler(routeAuthService{}, authConfig)
	settingsHandler := settingshttp.NewHandler(settingsapp.NewService(settingsapp.NewMemoryRepository(), nil, nil, nil))
	policy := httpmiddleware.TenantPolicy{Mode: "single", DefaultTenantID: "default"}
	RegisterRoutesWithIAM(router, authHandler, nil, AuxiliaryRoutes{Settings: settingsHandler, IAM: service, TenantPolicy: &policy})

	read := httptest.NewRequest(http.MethodGet, "/api/admin/v1/observability/settings/observability.metrics.enabled", nil)
	read.Header.Set("Authorization", "Bearer test")
	readResponse := httptest.NewRecorder()
	router.ServeHTTP(readResponse, read)
	if readResponse.Code != http.StatusOK {
		t.Fatalf("observability read status=%d body=%s", readResponse.Code, readResponse.Body.String())
	}

	write := httptest.NewRequest(http.MethodPut, "/api/admin/v1/observability/settings/observability.metrics.enabled", strings.NewReader(`{"value":true,"expectedVersion":0}`))
	write.Header.Set("Authorization", "Bearer test")
	write.Header.Set("Content-Type", "application/json")
	writeResponse := httptest.NewRecorder()
	router.ServeHTTP(writeResponse, write)
	if writeResponse.Code != http.StatusOK {
		t.Fatalf("observability write status=%d body=%s", writeResponse.Code, writeResponse.Body.String())
	}

	for _, request := range []*http.Request{
		httptest.NewRequest(http.MethodGet, "/api/admin/v1/settings/security.jwt_secret", nil),
		httptest.NewRequest(http.MethodPut, "/api/admin/v1/settings/file.root", strings.NewReader(`{"value":"\"/tmp/outside\"","expectedVersion":0}`)),
	} {
		request.Header.Set("Authorization", "Bearer test")
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), `"code":30000`) {
			t.Fatalf("general settings %s %s status=%d body=%s", request.Method, request.URL.Path, response.Code, response.Body.String())
		}
	}

	for _, method := range []string{http.MethodGet, http.MethodPut} {
		body := strings.NewReader("")
		if method == http.MethodPut {
			body = strings.NewReader(`{"value":"\"changed\"","expectedVersion":0}`)
		}
		request := httptest.NewRequest(method, "/api/admin/v1/observability/settings/mail.password", body)
		request.Header.Set("Authorization", "Bearer test")
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("non-observability alias %s status=%d body=%s", method, response.Code, response.Body.String())
		}
	}
}

func newAuxiliaryIAMService(t *testing.T) *iamapp.Service {
	t.Helper()
	store := iamapp.NewMemoryStore()
	if err := store.SaveUser(context.Background(), iamdomain.User{
		ID: "route-test", Username: "route-test", TenantID: "default", OrgID: "org-a", Active: true,
	}); err != nil {
		t.Fatal(err)
	}
	return iamapp.NewService(store)
}

func newDictionaryAuthorizationRouter(t *testing.T, iamService *iamapp.Service, policy httpmiddleware.TenantPolicy) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	authConfig := config.Default().Auth
	authConfig.Enabled = true
	authHandler := authhttp.NewHandler(routeAuthService{}, authConfig)
	dictionaryHandler := dictionaryhttp.NewHandler(dictionaryapp.NewService(dictionaryapp.NewMemoryRepository(), nil))
	RegisterRoutesWithIAM(router, authHandler, nil, AuxiliaryRoutes{
		Dictionary:   dictionaryHandler,
		IAM:          iamService,
		TenantPolicy: &policy,
	})
	return router
}

func containsDuplicateAdminPrefix(route string) bool {
	const prefix = "/api/admin/v1/api/admin/v1"
	for index := 0; index+len(prefix) <= len(route); index++ {
		if route[index:index+len(prefix)] == prefix {
			return true
		}
	}
	return false
}
