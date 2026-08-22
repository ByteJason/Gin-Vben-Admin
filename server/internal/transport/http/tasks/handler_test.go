package taskshttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tasksapp "example.com/gin-vben-admin/server/internal/application/tasks"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
	"github.com/gin-gonic/gin"
)

func taskContext(t *testing.T, tenantID, orgID string) context.Context {
	t.Helper()
	scope, err := tenant.NewContext(tenantID, orgID, false)
	if err != nil {
		t.Fatal(err)
	}
	return tenant.WithContext(context.Background(), scope)
}

func TestTaskHandlerCreatesListsAndRequiresManualConfirmation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := tasksapp.NewService(tasksapp.NewMemoryRepository())
	r := gin.New()
	RegisterRoutes(r, NewHandler(service))

	create := httptest.NewRequest(http.MethodPost, "/api/admin/v1/tasks", strings.NewReader(`{"name":"Nightly","type":"manual","payloadSchema":{"type":"object"},"timezone":"UTC","concurrency":1,"timeoutSeconds":30,"maxAttempts":2}`))
	create.Header.Set("Content-Type", "application/json")
	create = create.WithContext(taskContext(t, "tenant-a", "org-a"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, create)
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"tenantId":"tenant-a"`) {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}

	list := httptest.NewRequest(http.MethodGet, "/api/admin/v1/tasks", nil).WithContext(taskContext(t, "tenant-a", "org-a"))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, list)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Nightly") {
		t.Fatalf("list status=%d body=%s", w.Code, w.Body.String())
	}

	run := httptest.NewRequest(http.MethodPost, "/api/admin/v1/tasks/task-unknown/run", strings.NewReader(`{"confirm":false}`))
	run.Header.Set("Content-Type", "application/json")
	run = run.WithContext(taskContext(t, "tenant-a", "org-a"))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, run)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "tasks.manual.confirmationRequired") {
		t.Fatalf("run confirmation status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestTaskHandlerRejectsMissingTenantScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterRoutes(r, NewHandler(tasksapp.NewService(tasksapp.NewMemoryRepository())))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/admin/v1/tasks", nil))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "tenant.context.invalid") {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}
