package install

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/response"
)

const installationRequiredCode = 10008

// InstallationGate keeps business routes closed until the first-install
// transaction has atomically published its marker. Installer and health
// endpoints remain reachable so an operator can complete or diagnose setup.
func InstallationGate(status StatusProvider) gin.HandlerFunc {
	return func(c *gin.Context) {
		if status == nil || installationRoute(c.Request.URL.Path) {
			c.Next()
			return
		}
		snapshot, err := status.Status(c.Request.Context())
		if err != nil {
			response.Error(c, http.StatusInternalServerError, 50000, "installation state unavailable")
			c.Abort()
			return
		}
		if !snapshot.Installed {
			response.Error(c, http.StatusLocked, installationRequiredCode, "installation required")
			c.Abort()
			return
		}
		c.Next()
	}
}

func installationRoute(path string) bool {
	return path == "/install" || strings.HasPrefix(path, "/install/") ||
		path == "/api/system/install/v1" || strings.HasPrefix(path, "/api/system/install/v1/") ||
		path == "/health/live" || path == "/health/ready"
}
