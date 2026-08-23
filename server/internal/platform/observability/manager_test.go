package observabilityplatform

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	domainobs "github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/observability"
)

func TestManagerReloadsCollectorsWithoutChangingRouterReference(t *testing.T) {
	manager, err := NewManager(domainobs.DefaultConfig())
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	if got := manager.CollectorCount(); got != 0 {
		t.Fatalf("CollectorCount() = %d, want 0", got)
	}

	enabled := domainobs.DefaultConfig()
	enabled.MetricsEnabled = true
	enabled.MetricsEndpoint = "http://127.0.0.1:8080/metrics"
	if err := manager.Reload(enabled); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	manager.RecordHTTP(http.MethodGet, "/health/live", http.StatusOK, time.Millisecond, "request-1")

	response := httptest.NewRecorder()
	manager.ServeMetrics(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK || manager.CollectorCount() != 1 {
		t.Fatalf("reloaded manager status=%d collectors=%d body=%s", response.Code, manager.CollectorCount(), response.Body.String())
	}

	invalid := enabled
	invalid.MetricsEndpoint = "not-an-absolute-url"
	if err := manager.Reload(invalid); err == nil {
		t.Fatal("Reload(invalid) error = nil")
	}
	if got := manager.CollectorCount(); got != 1 {
		t.Fatalf("invalid reload replaced working runtime, collectors=%d", got)
	}
}

func TestManagerRejectsReloadAfterClose(t *testing.T) {
	manager, err := NewManager(domainobs.DefaultConfig())
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := manager.Reload(domainobs.DefaultConfig()); err == nil {
		t.Fatal("Reload() after Close error = nil")
	}
}
