package filehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	fileapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/file"
	httpmiddleware "github.com/ByteJason/Gin-Vben-Admin/server/internal/transport/http/middleware"
	"github.com/gin-gonic/gin"
)

func TestFileHTTPUploadListDownloadDeleteAndCleanupDryRun(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := fileapp.NewService(fileapp.NewMemoryStore("http://files.local/api/admin/v1/files"), fileapp.Config{MaxBytes: 100, AllowedMIMEs: []string{"text/plain"}})
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
	req = httptest.NewRequest(http.MethodGet, "/api/admin/v1/files", nil)
	req.Header.Set("X-Actor-ID", "u1")
	r.ServeHTTP(res, req)
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
	req = httptest.NewRequest(http.MethodPost, "/api/admin/v1/files/"+envelope.Data.ID+"/signed-url", bytes.NewBufferString(`{"ttlSeconds":60}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Actor-ID", "u1")
	r.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("signed-url status = %d, body = %s", res.Code, res.Body.String())
	}
	var signedEnvelope struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &signedEnvelope); err != nil {
		t.Fatal(err)
	}
	signed, err := url.Parse(signedEnvelope.Data.URL)
	if err != nil || signed.Path != "/api/admin/v1/files/"+envelope.Data.ID+"/download" {
		t.Fatalf("signed download URL = %q, err = %v", signedEnvelope.Data.URL, err)
	}
	res = httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest(http.MethodGet, signed.String(), nil))
	if res.Code != http.StatusOK || res.Body.String() != "hello" {
		t.Fatalf("verified signed download status = %d, body = %q", res.Code, res.Body.String())
	}
	query := signed.Query()
	query.Set("sig", "00")
	signed.RawQuery = query.Encode()
	res = httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest(http.MethodGet, signed.String(), nil))
	if res.Code != http.StatusForbidden {
		t.Fatalf("tampered signed download status = %d, body = %q", res.Code, res.Body.String())
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

func TestFileHTTPUsesGeneratedClientFieldNames(t *testing.T) {
	item := fileapp.File{
		ID: "file-1", Key: "key-1", Name: "logo.png", MIME: "image/png",
		Size: 42, OwnerID: "u1", TenantID: "tenant-a", OrgID: "org-a",
		ACL: fileapp.ACLPublicRead,
	}
	payload, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err = json.Unmarshal(payload, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "key", "name", "mime", "size", "ownerId", "tenantId", "orgId", "acl"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("serialized file is missing %q: %s", key, payload)
		}
	}
	for _, key := range []string{"ID", "Key", "Name", "MIME", "Size", "OwnerID", "TenantID", "OrgID", "ACL"} {
		if _, ok := fields[key]; ok {
			t.Fatalf("serialized file leaked Go field %q: %s", key, payload)
		}
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
