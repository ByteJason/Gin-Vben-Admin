package mail

import (
	"context"
	"testing"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/application/notification"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

func TestServiceSenderAdaptsTemplatePortToLegacyDelivery(t *testing.T) {
	cipher := &testCipher{}
	accounts := NewMemoryAccountRepository()
	messages := NewMemoryMessageRepository()
	provider := &testProvider{}
	service := NewService(accounts, messages, provider, Config{Cipher: cipher})
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a"})
	if _, err := service.CreateAccount(ctx, AccountInput{Name: "primary", Enabled: true, Host: "mailpit", Port: 1025, FromEmail: "noreply@example.test"}); err != nil {
		t.Fatal(err)
	}
	runtime := notification.NewRuntime(notification.RuntimeConfig{Mailer: nil})
	_ = runtime
	sender := NewServiceSender(service, MapTemplateRenderer{Templates: map[string]TemplateView{"welcome": {Subject: "Welcome", Body: "Hello"}}})
	result, err := sender.Send(ctx, SendRequest{TemplateKey: "welcome", Recipients: []Recipient{{Address: "user@example.test"}}, IdempotencyKey: "welcome-1"})
	if err != nil || result.Status != notification.DeliverySent || result.MessageID == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
