package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authdomain "example.com/gin-vben-admin/server/internal/domain/authdomain"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
	"github.com/gin-gonic/gin"
)

func TestTenantContextRequiresTenantInMultiMode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(TenantContext(TenantPolicy{Mode: "multi"}))
	r.GET("/", func(c *gin.Context) {
		t.Fatalf("handler must not run without tenant")
	})

	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/", nil))
	if resp.Code != http.StatusBadRequest || !strings.Contains(resp.Body.String(), `"code":10000`) {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestTenantContextDefaultsSingleModeAndStoresOrganization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(TenantContext(TenantPolicy{Mode: "single", DefaultTenantID: "default"}))
	r.GET("/", func(c *gin.Context) {
		scope, err := tenant.RequireContext(c.Request.Context())
		if err != nil {
			t.Fatalf("scope error: %v", err)
		}
		if scope.TenantID != "default" || scope.Organization != "org-a" || scope.PlatformAdmin {
			t.Fatalf("unexpected scope: %+v", scope)
		}
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Org-ID", "org-a")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestTenantContextRejectsCrossTenantHeaderUnlessResolvedPlatformAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newRouter := func(platformAdmin bool) *gin.Engine {
		r := gin.New()
		r.Use(TenantContext(TenantPolicy{
			Mode:            "single",
			DefaultTenantID: "default",
			IsPlatformAdmin: func(*gin.Context) bool { return platformAdmin },
		}))
		r.GET("/", func(c *gin.Context) {
			scope, err := tenant.RequireContext(c.Request.Context())
			if err != nil {
				t.Fatalf("scope error: %v", err)
			}
			if !scope.PlatformAdmin || scope.TenantID != "tenant-b" {
				t.Fatalf("unexpected admin scope: %+v", scope)
			}
			c.Status(http.StatusNoContent)
		})
		return r
	}

	request := func(r *gin.Engine) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Tenant-ID", "tenant-b")
		resp := httptest.NewRecorder()
		r.ServeHTTP(resp, req)
		return resp
	}
	if resp := request(newRouter(false)); resp.Code != http.StatusForbidden || !strings.Contains(resp.Body.String(), `"code":30000`) {
		t.Fatalf("non-admin status=%d body=%s", resp.Code, resp.Body.String())
	}
	if resp := request(newRouter(true)); resp.Code != http.StatusNoContent {
		t.Fatalf("platform-admin status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestTenantContextDoesNotTrustPlatformAdminHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(TenantContext(TenantPolicy{Mode: "single", DefaultTenantID: "default"}))
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Tenant-ID", "tenant-b")
	req.Header.Set("X-Platform-Admin", "true")
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestTenantContextResolvesConfiguredPlatformAdminSubject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Tenant-ID", "tenant-b")
	// Claims are installed by the authentication middleware in production;
	// this fixture sets the same server-side context value directly.
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("auth_claims", authdomain.Claims{Subject: "admin-subject"})
		c.Next()
	}, TenantContext(TenantPolicy{Mode: "multi", PlatformAdminSubjects: []string{"admin-subject"}}))
	r.GET("/", func(c *gin.Context) {
		scope, err := tenant.RequireContext(c.Request.Context())
		if err != nil || !scope.PlatformAdmin {
			t.Fatalf("scope=%+v err=%v", scope, err)
		}
		c.Status(http.StatusNoContent)
	})
	resp := httptest.NewRecorder()
	r.ServeHTTP(resp, req)
	if resp.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
}
