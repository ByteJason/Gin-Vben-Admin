package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/staticui"
	"github.com/gin-gonic/gin"
)

func TestLoadInstallerAssetsServesTheInstallerAtItsPublicRoute(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	mustWriteAsset(t, filepath.Join(directory, "index.html"), "<h1>temporary installer</h1>")
	mustWriteAsset(t, filepath.Join(directory, "app.js"), "console.log('installer')")
	assets, err := loadInstallerAssets(directory)
	if err != nil {
		t.Fatalf("loadInstallerAssets() error = %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	staticui.RegisterInstallerRoutes(router, assets)
	for _, testCase := range []struct {
		path string
		want string
	}{
		{path: "/install", want: "temporary installer"},
		{path: "/install/app.js", want: "console.log"},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, testCase.path, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), testCase.want) {
			t.Fatalf("GET %s = %d %q", testCase.path, response.Code, response.Body.String())
		}
	}
}

func TestLoadInstallerAssetsRejectsMissingIndexAndSymlinks(t *testing.T) {
	t.Parallel()

	if _, err := loadInstallerAssets(t.TempDir()); err == nil {
		t.Fatal("missing index error = nil")
	}

	target := t.TempDir()
	mustWriteAsset(t, filepath.Join(target, "index.html"), "fixture")
	parent := t.TempDir()
	link := filepath.Join(parent, "dist")
	if err := os.Symlink(target, link); err != nil {
		if os.IsPermission(err) {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Fatalf("Symlink() error = %v", err)
	}
	if _, err := loadInstallerAssets(link); err == nil {
		t.Fatal("symlink asset directory error = nil")
	}
}

func mustWriteAsset(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
