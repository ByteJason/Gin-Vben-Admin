package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	domain "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/iam"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/gin-gonic/gin"
)

type recordingIAMAccess struct {
	user           domain.User
	request        domain.Request
	resolvedScope  tenant.Context
	authorizeAllow bool
}

func (a *recordingIAMAccess) GetAuthorizationUser(context.Context, string) (domain.User, error) {
	return a.user, nil
}

func (a *recordingIAMAccess) ResolveSubject(ctx context.Context, user domain.User) (domain.Subject, error) {
	a.resolvedScope, _ = tenant.FromContext(ctx)
	return domain.Subject{UserID: user.ID, Domain: a.resolvedScope.TenantID}, nil
}

func (a *recordingIAMAccess) Authorize(_ context.Context, _ domain.Subject, request domain.Request) (bool, error) {
	a.request = request
	return a.authorizeAllow, nil
}

func TestIAMAuthorizationUsesMethodFullPathAndBoundPrincipalScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	iam := &recordingIAMAccess{
		user:           domain.User{ID: "u1", TenantID: "tenant-a", OrgID: "org-a", Active: true},
		authorizeAllow: true,
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("auth_claims", authdomain.Claims{Subject: "u1"})
		c.Request = c.Request.WithContext(tenant.WithContext(c.Request.Context(), tenant.Context{TenantID: "tenant-a"}))
		c.Next()
	}, IAMAuthorization(iam))
	router.PATCH("/resources/:id", func(c *gin.Context) {
		scope, _ := tenant.FromContext(c.Request.Context())
		if scope.Organization != "org-a" {
			t.Fatalf("downstream scope=%+v", scope)
		}
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodPatch, "/resources/42?source=test", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if iam.request.Method != http.MethodPatch || iam.request.Path != "/resources/:id" || iam.request.Domain != "tenant-a" {
		t.Fatalf("authorization request=%+v", iam.request)
	}
	if iam.resolvedScope.Organization != "org-a" {
		t.Fatalf("subject resolution scope=%+v", iam.resolvedScope)
	}
}

func TestIAMAuthorizationDeniesBeforeHandlerWhenDecisionIsFalse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	iam := &recordingIAMAccess{
		user: domain.User{ID: "u1", TenantID: "tenant-a", Active: true},
	}
	called := false
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("auth_claims", authdomain.Claims{Subject: "u1"})
		c.Request = c.Request.WithContext(tenant.WithContext(c.Request.Context(), tenant.Context{TenantID: "tenant-a"}))
		c.Next()
	}, IAMAuthorization(iam))
	router.DELETE("/resources/:id", func(c *gin.Context) { called = true })

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/resources/42", nil))
	if response.Code != http.StatusForbidden || called {
		t.Fatalf("status=%d called=%v body=%s", response.Code, called, response.Body.String())
	}
}
