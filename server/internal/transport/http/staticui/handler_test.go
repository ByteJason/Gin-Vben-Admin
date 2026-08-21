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
