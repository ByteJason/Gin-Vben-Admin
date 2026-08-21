package admin

import (
	"context"
	"testing"

	auditapp "example.com/gin-vben-admin/server/internal/application/audit"
	appauth "example.com/gin-vben-admin/server/internal/application/auth"
	settingsapp "example.com/gin-vben-admin/server/internal/application/settings"
	"example.com/gin-vben-admin/server/internal/config"
	"example.com/gin-vben-admin/server/internal/domain/authdomain"
	audithttp "example.com/gin-vben-admin/server/internal/transport/http/audit"
	authhttp "example.com/gin-vben-admin/server/internal/transport/http/auth"
	settingshttp "example.com/gin-vben-admin/server/internal/transport/http/settings"
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

func containsDuplicateAdminPrefix(route string) bool {
	const prefix = "/api/admin/v1/api/admin/v1"
	for index := 0; index+len(prefix) <= len(route); index++ {
		if route[index:index+len(prefix)] == prefix {
			return true
		}
	}
	return false
}
