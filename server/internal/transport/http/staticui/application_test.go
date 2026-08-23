package staticui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	"github.com/gin-gonic/gin"
)

func TestApplicationRoutesServeSelectedUIWhenInstalled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	assets := fstest.MapFS{
		"admin/antd/index.html":     &fstest.MapFile{Data: []byte("<h1>antd</h1>")},
		"admin/antd/assets/app.js":  &fstest.MapFile{Data: []byte("console.log('antd')")},
		"admin/ele/index.html":      &fstest.MapFile{Data: []byte("<h1>ele</h1>")},
		"admin/ele/assets/app.js":   &fstest.MapFile{Data: []byte("console.log('ele')")},
		"admin/naive/index.html":    &fstest.MapFile{Data: []byte("<h1>naive</h1>")},
		"admin/naive/assets/app.js": &fstest.MapFile{Data: []byte("console.log('naive')")},
	}
	status := staticStatusStub{status: installer.Status{Installed: true, State: installer.StateInstalled, SelectedUI: "ele"}}
	router := gin.New()
	RegisterApplicationRoutes(router, assets, status)

	for _, item := range []struct {
		path string
		want string
	}{
		{path: "/", want: "ele"},
		{path: "/assets/app.js", want: "console.log('ele')"},
		{path: "/dashboard", want: "ele"},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, item.path, nil))
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), item.want) {
			t.Fatalf("GET %s status=%d body=%q", item.path, recorder.Code, recorder.Body.String())
		}
	}
}

func TestApplicationRoutesBlockUninstalledInstance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	assets := fstest.MapFS{"admin/antd/index.html": &fstest.MapFile{Data: []byte("antd")}}
	status := staticStatusStub{status: installer.Status{Installed: false, State: installer.StateUninstalled}}
	router := gin.New()
	RegisterApplicationRoutes(router, assets, status)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusLocked {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusLocked, recorder.Body.String())
	}
}

type staticStatusStub struct {
	status installer.Status
	err    error
}

func (s staticStatusStub) Status(context.Context) (installer.Status, error) {
	return s.status, s.err
}
