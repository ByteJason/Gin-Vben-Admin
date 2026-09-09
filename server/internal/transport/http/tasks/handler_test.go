package taskshttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/application/jobs"
	tasksapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/tasks"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
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

func TestTaskHandlerEnqueuesListsAndCancelsRun(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := tasksapp.NewService(tasksapp.NewMemoryRepository())
	runs := tasksapp.NewRunService(service, tasksapp.NewMemoryRunRepository(), jobs.NewMemoryQueue(2))
	r := gin.New()
	RegisterRoutes(r, NewHandler(service, runs))
	ctx := taskContext(t, "tenant-a", "org-a")

	create := httptest.NewRequest(http.MethodPost, "/api/admin/v1/tasks", strings.NewReader(`{"name":"Manual run","type":"manual","payloadSchema":{"type":"object"},"timezone":"UTC","timeoutSeconds":10,"maxAttempts":2}`)).WithContext(ctx)
	create.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, create)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var createEnvelope struct {
		Data tasksapp.TaskDefinition `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &createEnvelope); err != nil {
		t.Fatal(err)
	}
	if createEnvelope.Data.ID == "" {
		t.Fatalf("create response missing id: %s", w.Body.String())
	}

	runRequest := httptest.NewRequest(http.MethodPost, "/api/admin/v1/tasks/"+createEnvelope.Data.ID+"/run", strings.NewReader(`{"confirm":true,"payload":{"source":"ui"},"idempotencyKey":"ui-run-1"}`)).WithContext(ctx)
	runRequest.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, runRequest)
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"status":"pending"`) {
		t.Fatalf("run status=%d body=%s", w.Code, w.Body.String())
	}
	var runEnvelope struct {
		Data tasksapp.TaskRun `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &runEnvelope); err != nil {
		t.Fatal(err)
	}

	list := httptest.NewRequest(http.MethodGet, "/api/admin/v1/tasks/"+createEnvelope.Data.ID+"/runs", nil).WithContext(ctx)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, list)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), runEnvelope.Data.ID) {
		t.Fatalf("list status=%d body=%s", w.Code, w.Body.String())
	}

	cancel := httptest.NewRequest(http.MethodPost, "/api/admin/v1/tasks/"+createEnvelope.Data.ID+"/runs/"+runEnvelope.Data.ID+"/cancel", nil).WithContext(ctx)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, cancel)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"cancelled"`) {
		t.Fatalf("cancel status=%d body=%s", w.Code, w.Body.String())
	}

	secondRun, err := runs.Enqueue(ctx, createEnvelope.Data.ID, []byte(`{"source":"retry"}`), "ui-run-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runs.MarkFailed(ctx, secondRun.ID, "provider.unavailable"); err != nil {
		t.Fatal(err)
	}
	retry := httptest.NewRequest(http.MethodPost, "/api/admin/v1/tasks/"+createEnvelope.Data.ID+"/runs/"+secondRun.ID+"/retry", nil).WithContext(ctx)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, retry)
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"status":"pending"`) {
		t.Fatalf("retry status=%d body=%s", w.Code, w.Body.String())
	}
	logs := httptest.NewRequest(http.MethodGet, "/api/admin/v1/tasks/"+createEnvelope.Data.ID+"/runs/"+secondRun.ID+"/logs", nil).WithContext(ctx)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, logs)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "provider.unavailable") {
		t.Fatalf("logs status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestTaskPagesPreviewAndUnavailableRun(t *testing.T) {
	service := tasksapp.NewService(tasksapp.NewMemoryRepository())
	ctx := taskContext(t, "page-tenant", "")
	created, err := service.Create(ctx, tasksapp.TaskDefinition{Name: "Paged", Type: "manual", PayloadSchema: json.RawMessage(`{}`), Cron: "@hourly", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	RegisterRoutes(r, NewHandler(service))
	for _, path := range []string{"/api/admin/v1/tasks?page=1&pageSize=10", "/api/admin/v1/tasks/preview?cron=%40hourly&timezone=UTC", "/api/admin/v1/tasks/methods"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil).WithContext(ctx))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		if strings.Contains(path, "page=") && !strings.Contains(w.Body.String(), `"total":1`) {
			t.Fatal(w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/admin/v1/tasks/"+created.ID+"/run", strings.NewReader(`{"confirm":true}`)).WithContext(ctx))
	if w.Code != 503 {
		t.Fatalf("missing worker falsely accepted: %d", w.Code)
	}
	runs := tasksapp.NewRunService(service, tasksapp.NewMemoryRunRepository(), jobs.NewMemoryQueue(1))
	_, _ = runs.Enqueue(ctx, created.ID, nil, "log-key")
	r2 := gin.New()
	RegisterRoutes(r2, NewHandler(service, runs))
	w = httptest.NewRecorder()
	r2.ServeHTTP(w, httptest.NewRequest("GET", "/api/admin/v1/tasks/runs?page=1&pageSize=10", nil).WithContext(ctx))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":1`) {
		t.Fatalf("global runs %d %s", w.Code, w.Body.String())
	}
}
