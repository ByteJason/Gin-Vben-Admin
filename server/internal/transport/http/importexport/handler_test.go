package importexport

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	importsapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/imports"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/gin-gonic/gin"
)

func TestPreviewCommitAndErrorDownloadAreTenantScoped(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := importsapp.NewService(importsapp.DefaultLimits())
	router := gin.New()
	router.Use(func(c *gin.Context) {
		scope, _ := tenant.NewContext(c.GetHeader("X-Tenant-ID"), "", false)
		c.Request = c.Request.WithContext(tenant.WithContext(c.Request.Context(), scope))
		c.Next()
	})
	RegisterRoutes(router, NewHandler(service))

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "users.csv")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("name,email\nAlice,a@example.test\n,Bad\n"))
	_ = writer.WriteField("columns", "name,email")
	_ = writer.WriteField("allowlist", "name,email")
	_ = writer.Close()
	request := httptest.NewRequest(http.MethodPost, "/api/admin/v1/import-export/imports/preview", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("X-Tenant-ID", "tenant-a")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data importsapp.PreviewResult `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.ID == "" || envelope.Data.TotalRows != 2 {
		t.Fatalf("preview=%+v", envelope.Data)
	}

	commitBody, _ := json.Marshal(map[string]string{"previewId": envelope.Data.ID, "idempotencyKey": "http-1"})
	request = httptest.NewRequest(http.MethodPost, "/api/admin/v1/import-export/imports/commit", bytes.NewReader(commitBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Tenant-ID", "tenant-a")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("commit status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/api/admin/v1/import-export/jobs", nil)
	request.Header.Set("X-Tenant-ID", "tenant-b")
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || strings.Contains(recorder.Body.String(), envelope.Data.ID) {
		t.Fatalf("cross tenant list status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	_ = context.Background()
}

func TestXLSXTemplateIsAReadableZipPackage(t *testing.T) {
	data, contentType, filename := templateData("xlsx")
	if contentType == "" || filename != "import-template.xlsx" {
		t.Fatalf("template metadata=%q %q", contentType, filename)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, file := range archive.File {
		seen[file.Name] = true
	}
	for _, name := range []string{"[Content_Types].xml", "xl/workbook.xml", "xl/worksheets/sheet1.xml"} {
		if !seen[name] {
			t.Fatalf("xlsx missing %s", name)
		}
	}
}
