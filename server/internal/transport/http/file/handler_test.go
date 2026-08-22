package filehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	fileapp "example.com/gin-vben-admin/server/internal/application/file"
	httpmiddleware "example.com/gin-vben-admin/server/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
)

func TestFileHTTPUploadListDownloadDeleteAndCleanupDryRun(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := fileapp.NewService(fileapp.NewMemoryStore("http://files.local"), fileapp.Config{MaxBytes: 100, AllowedMIMEs: []string{"text/plain"}})
	r := gin.New()
	r.Use(httpmiddleware.TenantContext(httpmiddleware.TenantPolicy{Mode: "single", DefaultTenantID: "tenant-a"}))
	RegisterRoutes(r, NewHandler(svc))

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "note.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("hello"))
	_ = writer.WriteField("acl", "private")
	_ = writer.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/files/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Actor-ID", "u1")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body = %s", res.Code, res.Body.String())
	}
	var envelope struct {
		Data fileapp.File `json:"data"`
	}
	if err = json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.ID == "" {
		t.Fatalf("upload data = %#v", envelope.Data)
	}

	res = httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/admin/v1/files", nil))
	if res.Code != http.StatusOK || !bytes.Contains(res.Body.Bytes(), []byte(envelope.Data.ID)) {
		t.Fatalf("list status = %d, body = %s", res.Code, res.Body.String())
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/admin/v1/files/"+envelope.Data.ID+"/download", nil)
	req.Header.Set("X-Actor-ID", "u1")
	r.ServeHTTP(res, req)
	if res.Code != http.StatusOK || res.Body.String() != "hello" {
		t.Fatalf("download status = %d, body = %q", res.Code, res.Body.String())
	}

	res = httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/admin/v1/files/cleanup/dry-run?ageSeconds=1", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("dry-run status = %d, body = %s", res.Code, res.Body.String())
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/admin/v1/files/"+envelope.Data.ID, nil)
	req.Header.Set("X-Actor-ID", "u1")
	r.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", res.Code, res.Body.String())
	}
}

func TestFileHTTPRequiresTenantContext(t *testing.T) {
	svc := fileapp.NewService(fileapp.NewMemoryStore(""), fileapp.Config{})
	r := gin.New()
	RegisterRoutes(r, NewHandler(svc))
	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/admin/v1/files", nil))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("missing tenant status = %d", res.Code)
	}
	_ = context.Background()
}
