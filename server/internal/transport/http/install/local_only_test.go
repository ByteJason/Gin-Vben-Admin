package install

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestLocalInstallerOnlyRequiresLoopbackPeerAndLoopbackHost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(LocalInstallerOnly())
	router.GET("/install", func(c *gin.Context) { c.Status(http.StatusOK) })

	for _, testCase := range []struct {
		name       string
		remoteAddr string
		host       string
		want       int
	}{
		{name: "ipv4 loopback", remoteAddr: "127.0.0.1:54321", host: "127.0.0.1:8080", want: http.StatusOK},
		{name: "ipv6 loopback", remoteAddr: "[::1]:54321", host: "[::1]:8080", want: http.StatusOK},
		{name: "localhost", remoteAddr: "127.0.0.1:54321", host: "localhost:8080", want: http.StatusOK},
		{name: "remote peer", remoteAddr: "192.0.2.20:54321", host: "127.0.0.1:8080", want: http.StatusForbidden},
		{name: "dns rebinding host", remoteAddr: "127.0.0.1:54321", host: "attacker.example:8080", want: http.StatusForbidden},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://"+testCase.host+"/install", nil)
			request.RemoteAddr = testCase.remoteAddr
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != testCase.want {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, testCase.want, response.Body.String())
			}
		})
	}
}

func TestLocalInstallerOnlyRequiresSameOriginJSONForMutations(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(LocalInstallerOnly())
	router.POST("/api/system/install/v1/apply", func(c *gin.Context) { c.Status(http.StatusAccepted) })

	for _, testCase := range []struct {
		name        string
		contentType string
		origin      string
		want        int
	}{
		{name: "same origin JSON", contentType: "application/json; charset=utf-8", origin: "http://127.0.0.1:8080", want: http.StatusAccepted},
		{name: "non-browser JSON", contentType: "application/json", want: http.StatusAccepted},
		{name: "simple cross-site content type", contentType: "text/plain", origin: "http://attacker.example", want: http.StatusUnsupportedMediaType},
		{name: "cross-site origin", contentType: "application/json", origin: "http://attacker.example", want: http.StatusForbidden},
		{name: "different local port", contentType: "application/json", origin: "http://127.0.0.1:9090", want: http.StatusForbidden},
		{name: "different scheme", contentType: "application/json", origin: "https://127.0.0.1:8080", want: http.StatusForbidden},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/system/install/v1/apply", strings.NewReader(`{}`))
			request.RemoteAddr = "127.0.0.1:54321"
			request.Header.Set("Content-Type", testCase.contentType)
			if testCase.origin != "" {
				request.Header.Set("Origin", testCase.origin)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != testCase.want {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, testCase.want, response.Body.String())
			}
		})
	}
}
