package staticui

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

var contentTypes = map[string]string{
	".css":  "text/css; charset=utf-8",
	".html": "text/html; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".json": "application/json; charset=utf-8",
	".svg":  "image/svg+xml",
}

// RegisterInstallerRoutes mounts only the installer subtree of a generated
// bundle. It does not register a catch-all route or expose directory listings.
func RegisterInstallerRoutes(router gin.IRoutes, assets fs.FS) {
	if assets == nil {
		return
	}
	installer, err := fs.Sub(assets, "install")
	if err != nil {
		return
	}

	serveIndex := func(c *gin.Context) {
		serveFile(c, installer, "index.html")
	}
	serveAsset := func(c *gin.Context) {
		requested := strings.TrimPrefix(c.Param("asset"), "/")
		if !safeAssetPath(requested) {
			c.Status(http.StatusNotFound)
			return
		}
		serveFile(c, installer, requested)
	}

	router.GET("/install", serveIndex)
	router.HEAD("/install", serveIndex)
	router.GET("/install/*asset", serveAsset)
	router.HEAD("/install/*asset", serveAsset)
}

func safeAssetPath(requested string) bool {
	if requested == "" || strings.Contains(requested, "\\") {
		return false
	}
	for _, segment := range strings.Split(requested, "/") {
		if segment == ".." || segment == "." || segment == "" {
			return false
		}
	}
	cleaned := path.Clean(requested)
	return cleaned == requested && fs.ValidPath(cleaned)
}

func serveFile(c *gin.Context, assets fs.FS, name string) {
	info, err := fs.Stat(assets, name)
	if err != nil || !info.Mode().IsRegular() {
		c.Status(http.StatusNotFound)
		return
	}
	contents, err := fs.ReadFile(assets, name)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	contentType := contentTypes[path.Ext(name)]
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Header("Cache-Control", "no-cache")
	c.Data(http.StatusOK, contentType, contents)
}
