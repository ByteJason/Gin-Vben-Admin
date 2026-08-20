package bootstrap

import (
	"net/http"

	"example.com/gin-vben-admin/server/internal/transport/http/router"
)

func NewHTTPServer(addr string) *http.Server {
	return &http.Server{Addr: addr, Handler: router.NewRouter()}
}
