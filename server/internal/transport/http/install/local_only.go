package install

import (
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/response"
)

const localInstallerRequiredCode = 10009

// LocalInstallerOnly keeps the unauthenticated first-install surface on the
// machine that owns the checkout. It deliberately uses RemoteAddr and a
// loopback Host rather than proxy headers, so X-Forwarded-For and DNS rebinding
// cannot turn a remote request into a local installer request. Browser
// mutations additionally require same-origin JSON, excluding simple CSRF
// requests while retaining curl/CLI recovery requests that omit Origin.
func LocalInstallerOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !loopbackPeer(c.Request.RemoteAddr) || !loopbackAuthority(c.Request.Host) {
			response.Error(c, http.StatusForbidden, localInstallerRequiredCode, "local installer access required")
			c.Abort()
			return
		}
		if installerMutation(c.Request.Method) {
			mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
			if err != nil || mediaType != "application/json" {
				response.Error(c, http.StatusUnsupportedMediaType, 10000, "application/json required")
				c.Abort()
				return
			}
			requestScheme := "http"
			if c.Request.TLS != nil {
				requestScheme = "https"
			}
			if origin := strings.TrimSpace(c.GetHeader("Origin")); origin != "" && !sameLoopbackOrigin(origin, requestScheme, c.Request.Host) {
				response.Error(c, http.StatusForbidden, localInstallerRequiredCode, "same-origin installer request required")
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

func loopbackPeer(remoteAddress string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddress))
	if err != nil {
		return false
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func loopbackAuthority(authority string) bool {
	host := strings.TrimSpace(authority)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	host = strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func sameLoopbackOrigin(origin, requestScheme, requestAuthority string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != requestScheme || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	return loopbackAuthority(parsed.Host) && strings.EqualFold(parsed.Host, requestAuthority)
}

func installerMutation(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
