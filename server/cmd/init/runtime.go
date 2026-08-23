package main

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/middleware"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/staticui"
	"github.com/gin-gonic/gin"
)

func withInstallerAssets(base http.Handler, assets fs.FS) http.Handler {
	installer := gin.New()
	installer.Use(gin.Recovery(), middleware.SecurityHeaders())
	staticui.RegisterInstallerRoutes(installer, assets)
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/install" || strings.HasPrefix(request.URL.Path, "/install/") {
			installer.ServeHTTP(response, request)
			return
		}
		if base == nil {
			http.NotFound(response, request)
			return
		}
		base.ServeHTTP(response, request)
	})
}
