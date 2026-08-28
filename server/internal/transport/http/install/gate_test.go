package install

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	installer "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/installer"
	"github.com/gin-gonic/gin"
)

func TestInstallationGateBlocksBusinessRoutesUntilInstalled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	status := statusProviderStub{status: installer.Status{State: installer.StateUninstalled, Installed: false}}
	router := gin.New()
	router.Use(InstallationGate(status))
	router.GET("/api/admin/v1/ping", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/v1/ping", nil))
	if recorder.Code != http.StatusLocked {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusLocked, recorder.Body.String())
	}
	var body struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != 10008 {
		t.Fatalf("code = %d, want 10008", body.Code)
	}
}

func TestInstallationGateAllowsInstallerAndHealthRoutesBeforeInstall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	status := statusProviderStub{status: installer.Status{State: installer.StateUninstalled, Installed: false}}
	router := gin.New()
	router.Use(InstallationGate(status))
	router.GET("/install", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.GET("/health/live", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for _, path := range []string{"/install", "/health/live"} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("GET %s status = %d, want 204", path, recorder.Code)
		}
	}
}

func TestInstallationGateAllowsBusinessRoutesAfterInstall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	status := statusProviderStub{status: installer.Status{State: installer.StateInstalled, Installed: true}}
	router := gin.New()
	router.Use(InstallationGate(status))
	router.GET("/api/admin/v1/ping", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/v1/ping", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", recorder.Code)
	}
}

func TestInstallationGateAllowsBusinessRoutesWhileInstalledUISwitchIsPending(t *testing.T) {
	gin.SetMode(gin.TestMode)
	status := statusProviderStub{status: installer.Status{
		State:     installer.StateInstalled,
		Installed: true,
		Phase:     installer.InstallationPhaseUIPrepare,
		UIAction:  installer.UIPreparationActionPrepare,
	}}
	router := gin.New()
	router.Use(InstallationGate(status))
	router.GET("/api/admin/v1/ping", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/v1/ping", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 during an independent UI switch", recorder.Code)
	}
}
