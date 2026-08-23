package mailhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mailapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/mail"
	appnotification "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/notification"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
	"github.com/gin-gonic/gin"
)

type fixtureProvider struct{}

func (fixtureProvider) Send(context.Context, appnotification.SMTPAccount, appnotification.Message) error {
	return nil
}
func (fixtureProvider) TestConnection(context.Context, appnotification.SMTPAccount) error { return nil }

type fixtureCipher struct{}

func (fixtureCipher) Encrypt(_ context.Context, _ string, value []byte) ([]byte, error) {
	return append([]byte("cipher:"), value...), nil
}
func (fixtureCipher) Decrypt(_ context.Context, _ string, value []byte) ([]byte, error) {
	return []byte(strings.TrimPrefix(string(value), "cipher:")), nil
}

func TestMailRoutesRedactAccountPasswordAndPersistMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	accounts := mailapp.NewMemoryAccountRepository()
	messages := mailapp.NewMemoryMessageRepository()
	service := mailapp.NewService(accounts, messages, fixtureProvider{}, mailapp.Config{Cipher: fixtureCipher{}})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		scope, _ := tenant.NewContext("tenant-a", "", true)
		c.Request = c.Request.WithContext(tenant.WithContext(c.Request.Context(), scope))
		c.Next()
	})
	RegisterRoutes(router, NewHandler(service))

	create := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/admin/v1/mail/accounts", strings.NewReader(`{"name":"primary","enabled":true,"host":"smtp.example.test","port":587,"username":"fixture-user","password":"fixture-password","weight":2,"fromEmail":"no-reply@example.test"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(create, request)
	if create.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	if strings.Contains(create.Body.String(), "fixture-password") {
		t.Fatalf("create response leaked password: %s", create.Body.String())
	}
	var envelope struct {
		Data mailapp.Account `json:"data"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/api/admin/v1/mail/accounts", nil))
	if list.Code != http.StatusOK || strings.Contains(list.Body.String(), "fixture-password") {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}

	send := httptest.NewRecorder()
	sendRequest := httptest.NewRequest(http.MethodPost, "/api/admin/v1/mail/messages", strings.NewReader(`{"recipients":["recipient@example.test"],"subject":"fixture","body":"secret body"}`))
	sendRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(send, sendRequest)
	if send.Code != http.StatusOK || strings.Contains(send.Body.String(), "secret body") {
		t.Fatalf("send status=%d body=%s", send.Code, send.Body.String())
	}
}
