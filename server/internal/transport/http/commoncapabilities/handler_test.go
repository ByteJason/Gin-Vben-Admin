package commoncapabilities

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	fileapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/file"
	notification "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/notification"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/gin-gonic/gin"
)

type recordingNotificationMailer struct {
	messages []notification.Message
}

func (m *recordingNotificationMailer) Send(_ context.Context, message notification.Message) error {
	m.messages = append(m.messages, message)
	return nil
}

func TestRoutesRegisterAndDisabledDependencyIs503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterRoutes(r, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/notification/templates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRoutesMirrorFrozenCompatibilityAliases(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterRoutes(r, nil)
	want := map[string]bool{
		"PUT /api/admin/v1/notification/callers/:id":                       false,
		"PUT /api/admin/v1/notification/verification-policies/:policy_key": false,
		"PUT /api/admin/v1/media/library/:id":                              false,
		"DELETE /api/admin/v1/media/library/:id":                           false,
		"GET /api/admin/v1/media/library/:id/usage":                        false,
	}
	for _, route := range r.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for route, found := range want {
		if !found {
			t.Fatalf("missing frozen route %s", route)
		}
	}
}

func TestTemplateWriteAndTestUsesIdempotency(t *testing.T) {
	rt := notification.NewMemoryRuntime(nil)
	_ = rt.SetCaller(notification.Caller{Key: "caller", Enabled: true})
	_ = rt.SetTemplate(notification.Template{Key: "welcome", Published: true, Enabled: true, Locales: map[string]notification.TemplateLocale{"en-US": {Subject: "Hi", Body: "Hello {{.name}}"}}, Variables: []string{"name"}})
	r := gin.New()
	RegisterRoutes(r, NewHandler(rt, nil))
	ctx := tenant.WithContext(httptest.NewRequest(http.MethodPost, "/", nil).Context(), tenant.Context{TenantID: "t", Organization: "o"})
	_ = ctx
	body := `{"callerKey":"caller","recipients":[{"address":"u@example.test"}],"variables":{"name":"N"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/notification/templates/welcome/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected provider 503, got %d", w.Code)
	}
}

func TestTemplateTestFillsBuiltInVerificationVariables(t *testing.T) {
	mailer := &recordingNotificationMailer{}
	rt := notification.NewMemoryRuntime(mailer)
	if err := rt.SeedBuiltInDefaults(); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	RegisterRoutes(r, NewHandler(rt, nil))
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/notification/templates/auth.email-change/test", strings.NewReader(`{"recipient":"u@example.test","locale":"zh-CN","variables":{}}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var envelope struct {
		Data notification.SendResult `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.Data.IsTest || envelope.Data.Status != notification.DeliverySent || len(mailer.messages) != 1 {
		t.Fatalf("result=%+v messages=%d", envelope.Data, len(mailer.messages))
	}
	if !strings.Contains(mailer.messages[0].Body, "123456") || !strings.Contains(mailer.messages[0].Body, "10 分钟") {
		t.Fatalf("sample variables were not rendered: %q", mailer.messages[0].Body)
	}
}

func TestTemplateTestReturnsVariableDiagnostic(t *testing.T) {
	rt := notification.NewMemoryRuntime(&recordingNotificationMailer{})
	if err := rt.SeedBuiltInDefaults(); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	RegisterRoutes(r, NewHandler(rt, nil))
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/notification/templates/auth.email-change/test", strings.NewReader(`{"recipient":"u@example.test","locale":"zh-CN","variables":{"unexpected":"value"}}`)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var envelope struct {
		Errors []struct {
			MessageKey string         `json:"messageKey"`
			Params     map[string]any `json:"params"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Errors) != 1 || envelope.Errors[0].MessageKey != "notification.template.variableInvalid" || envelope.Errors[0].Params["variable"] != "unexpected" {
		t.Fatalf("diagnostic=%#v", envelope.Errors)
	}
}

func TestListMediaAcceptsSharedClientMimeExactAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)
	legacy := fileapp.NewService(fileapp.NewMemoryStore("http://memory.invalid/objects"), fileapp.Config{AllowedMIMEs: []string{"image/png", "text/plain"}})
	catalog := fileapp.NewCatalog(legacy)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	if _, err := catalog.Upload(ctx, fileapp.UploadInput{Data: []byte("png"), Size: 3, Name: "logo.png", MIME: "image/png"}); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.Upload(ctx, fileapp.UploadInput{Data: []byte("txt"), Size: 3, Name: "readme.txt", MIME: "text/plain"}); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	RegisterRoutes(r, NewHandler(nil, catalog))
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/media/library?mimeExact=image/png", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `logo.png`) || strings.Contains(w.Body.String(), `readme.txt`) {
		t.Fatalf("response=%s", w.Body.String())
	}
}

func TestListMediaRejectsNegativeOffset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	legacy := fileapp.NewService(fileapp.NewMemoryStore("http://memory.invalid/objects"), fileapp.Config{AllowedMIMEs: []string{"image/png"}})
	catalog := fileapp.NewCatalog(legacy)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	r := gin.New()
	RegisterRoutes(r, NewHandler(nil, catalog))
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/media/library?offset=-1", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestOpenMediaSupportsSingleByteRangeAndRejectsMultipleRanges(t *testing.T) {
	gin.SetMode(gin.TestMode)
	legacy := fileapp.NewService(fileapp.NewMemoryStore("http://memory.invalid/objects"), fileapp.Config{AllowedMIMEs: []string{"text/plain"}})
	catalog := fileapp.NewCatalog(legacy)
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	resource, err := catalog.Upload(ctx, fileapp.UploadInput{Data: []byte("abcdef"), Size: 6, Name: "note.txt", MIME: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	RegisterRoutes(r, NewHandler(nil, catalog))
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/media/library/"+resource.ID+"/open", nil).WithContext(ctx)
	req.Header.Set("Range", "bytes=1-3")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusPartialContent || w.Body.String() != "bcd" {
		t.Fatalf("range status=%d body=%q headers=%v", w.Code, w.Body.String(), w.Header())
	}
	if got := w.Header().Get("Content-Range"); got != "bytes 1-3/6" {
		t.Fatalf("content-range=%q", got)
	}
	multi := httptest.NewRequest(http.MethodGet, "/api/admin/v1/media/library/"+resource.ID+"/open", nil).WithContext(ctx)
	multi.Header.Set("Range", "bytes=0-1,3-4")
	multiW := httptest.NewRecorder()
	r.ServeHTTP(multiW, multi)
	if multiW.Code != http.StatusRequestedRangeNotSatisfiable || multiW.Header().Get("Content-Range") != "bytes */6" {
		t.Fatalf("multiple range status=%d headers=%v", multiW.Code, multiW.Header())
	}
}
