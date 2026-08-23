package staticui

import (
	"context"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/response"
)

type InstallationStatusProvider interface {
	Status(context.Context) (installer.Status, error)
}

// RegisterApplicationRoutes serves the selected embedded management UI from
// Gin's fallback route. Explicit API, health, and installer routes always win.
func RegisterApplicationRoutes(router *gin.Engine, assets fs.FS, status InstallationStatusProvider) {
	if router == nil || assets == nil || status == nil {
		return
	}
	router.NoRoute(func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Status(http.StatusNotFound)
			return
		}
		snapshot, err := status.Status(c.Request.Context())
		if err != nil {
			response.Error(c, http.StatusInternalServerError, 50000, "installation state unavailable")
			return
		}
		if !snapshot.Installed {
			response.Error(c, http.StatusLocked, 10008, "installation required")
			return
		}
		ui, ok := selectedUI(snapshot)
		if !ok {
			response.Error(c, http.StatusInternalServerError, 50000, "installation state unavailable")
			return
		}
		application, err := fs.Sub(assets, path.Join("admin", ui))
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		requested := strings.TrimPrefix(c.Request.URL.Path, "/")
		if requested == "" {
			serveFile(c, application, "index.html")
			return
		}
		if reservedApplicationPath(requested) || !safeAssetPath(requested) {
			c.Status(http.StatusNotFound)
			return
		}
		if info, err := fs.Stat(application, requested); err == nil && info.Mode().IsRegular() {
			serveFile(c, application, requested)
			return
		}
		if path.Ext(requested) == "" {
			serveFile(c, application, "index.html")
			return
		}
		c.Status(http.StatusNotFound)
	})
}

func selectedUI(status installer.Status) (string, bool) {
	ui := string(status.SelectedUI)
	switch ui {
	case "antd", "ele", "naive":
		return ui, true
	default:
		return "", false
	}
}

func reservedApplicationPath(requested string) bool {
	return requested == "api" || strings.HasPrefix(requested, "api/") ||
		requested == "health" || strings.HasPrefix(requested, "health/") ||
		requested == "install" || strings.HasPrefix(requested, "install/")
}
