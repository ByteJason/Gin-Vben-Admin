package staticui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

func TestInstallerRoutesServeOnlyInstallerFiles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	assets := fstest.MapFS{
		"install/index.html": &fstest.MapFile{Data: []byte("<h1>installer</h1>")},
		"install/app.js":     &fstest.MapFile{Data: []byte("console.log('install')")},
		"private/secret.txt": &fstest.MapFile{Data: []byte("not public")},
	}
	router := gin.New()
	RegisterInstallerRoutes(router, assets)

	for _, item := range []struct {
		path        string
		status      int
		contentType string
		body        string
	}{
		{path: "/install", status: http.StatusOK, contentType: "text/html; charset=utf-8", body: "installer"},
		{path: "/install/app.js", status: http.StatusOK, contentType: "text/javascript; charset=utf-8", body: "console.log"},
		{path: "/install/missing.js", status: http.StatusNotFound},
		{path: "/api/admin/v1/ping", status: http.StatusNotFound},
	} {
		t.Run(item.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, item.path, nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != item.status {
				t.Fatalf("GET %s status = %d, want %d; body=%s", item.path, response.Code, item.status, response.Body.String())
			}
			if item.contentType != "" && response.Header().Get("Content-Type") != item.contentType {
				t.Fatalf("GET %s content-type = %q, want %q", item.path, response.Header().Get("Content-Type"), item.contentType)
			}
			if item.body != "" && !strings.Contains(response.Body.String(), item.body) {
				t.Fatalf("GET %s body = %q, want substring %q", item.path, response.Body.String(), item.body)
			}
		})
	}
}

func TestInstallerHTMLOverridesTheAPIOnlyCSPForSameOriginAssetsAndRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	assets := fstest.MapFS{
		"install/index.html": &fstest.MapFile{Data: []byte("<h1>installer</h1>")},
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		c.Next()
	})
	RegisterInstallerRoutes(router, assets)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/install", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /install status = %d, want 200", response.Code)
	}
	csp := response.Header().Get("Content-Security-Policy")
	for _, directive := range []string{"default-src 'none'", "script-src 'self'", "style-src 'self'", "connect-src 'self'", "form-action 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, directive) {
			t.Fatalf("installer CSP = %q, want directive %q", csp, directive)
		}
	}
}

func TestInstallerRoutesDoNotExposeParentOrDirectories(t *testing.T) {
	gin.SetMode(gin.TestMode)
	assets := fstest.MapFS{
		"install/index.html":    &fstest.MapFile{Data: []byte("installer")},
		"install/assets/app.js": &fstest.MapFile{Data: []byte("app")},
		"secret.txt":            &fstest.MapFile{Data: []byte("secret")},
	}
	router := gin.New()
	RegisterInstallerRoutes(router, assets)

	for _, path := range []string{"/install/assets", "/install/%2e%2e/secret.txt"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404; body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestInstallerRoutesReturnHeadersWithoutBodiesForHEAD(t *testing.T) {
	gin.SetMode(gin.TestMode)
	assets := fstest.MapFS{
		"install/index.html": &fstest.MapFile{Data: []byte("installer")},
		"install/app.js":     &fstest.MapFile{Data: []byte("console.log('install')")},
		"install/styles.css": &fstest.MapFile{Data: []byte("body{}")},
	}
	router := gin.New()
	RegisterInstallerRoutes(router, assets)

	for _, item := range []struct {
		path        string
		contentType string
		length      string
	}{
		{path: "/install", contentType: "text/html; charset=utf-8", length: "9"},
		{path: "/install/app.js", contentType: "text/javascript; charset=utf-8", length: "22"},
		{path: "/install/styles.css", contentType: "text/css; charset=utf-8", length: "6"},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodHead, item.path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("HEAD %s status = %d, want 200", item.path, response.Code)
		}
		if got := response.Header().Get("Content-Type"); got != item.contentType {
			t.Fatalf("HEAD %s content-type = %q, want %q", item.path, got, item.contentType)
		}
		if got := response.Header().Get("Content-Length"); got != item.length {
			t.Fatalf("HEAD %s content-length = %q, want %q", item.path, got, item.length)
		}
		if response.Body.Len() != 0 {
			t.Fatalf("HEAD %s body length = %d, want 0", item.path, response.Body.Len())
		}
	}
}

func TestInstallerRoutesIgnoreUnavailableBundle(t *testing.T) {
	router := gin.New()
	RegisterInstallerRoutes(router, nil)
	request := httptest.NewRequest(http.MethodGet, "/install", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.Code)
	}
}
