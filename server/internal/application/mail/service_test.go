package mail

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	appnotification "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/notification"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

type testCipher struct{}

func (testCipher) Encrypt(_ context.Context, key string, plaintext []byte) ([]byte, error) {
	return append([]byte(key+":"), plaintext...), nil
}

func (testCipher) Decrypt(_ context.Context, key string, ciphertext []byte) ([]byte, error) {
	prefix := []byte(key + ":")
	if !strings.HasPrefix(string(ciphertext), string(prefix)) {
		return nil, errors.New("ciphertext mismatch")
	}
	return []byte(strings.TrimPrefix(string(ciphertext), string(prefix))), nil
}

type testProvider struct {
	sends  int
	tests  int
	result error
}

func (p *testProvider) Send(context.Context, appnotification.SMTPAccount, appnotification.Message) error {
	p.sends++
	return p.result
}

func (p *testProvider) SendWithResult(_ context.Context, _ appnotification.SMTPAccount, message appnotification.Message) (string, error) {
	p.sends++
	return "provider-" + message.ID, p.result
}

func (p *testProvider) TestConnection(context.Context, appnotification.SMTPAccount) error {
	p.tests++
	return p.result
}

func testMailContext(t *testing.T) context.Context {
	t.Helper()
	scope, err := tenant.NewContext("tenant-a", "org-a", true)
	if err != nil {
		t.Fatal(err)
	}
	return tenant.WithContext(context.Background(), scope)
}

func TestServiceStoresEncryptedBodyAndRedactsViews(t *testing.T) {
	accounts := NewMemoryAccountRepository()
	messages := NewMemoryMessageRepository()
	provider := &testProvider{}
	svc := NewService(accounts, messages, provider, Config{Cipher: testCipher{}, Clock: func() time.Time { return time.Unix(10, 0) }, Selection: appnotification.SMTPSelectionRoundRobin, RetryDelays: []time.Duration{0}})
	ctx := testMailContext(t)
	account, err := svc.CreateAccount(ctx, AccountInput{Name: "primary", Host: "smtp.example.test", Port: 587, Username: "fixture-user", Password: "fixture-password", FromEmail: "no-reply@example.test", Weight: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if account.PasswordConfigured != true || account.PasswordCiphertext != nil {
		t.Fatalf("account leaked password state: %#v", account)
	}
	result, err := svc.Send(ctx, SendInput{Recipients: []string{"recipient@example.test"}, Subject: "hello", Body: "sensitive body"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusSent || result.Body != "" || result.ProviderMessageID == "" {
		t.Fatalf("send result = %#v", result)
	}
	stored, err := messages.Get(ctx, result.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(stored.BodyCiphertext) == "sensitive body" || stored.BodyDigest == "" {
		t.Fatalf("body was not encrypted/digested: %#v", stored)
	}
	detail, err := svc.GetMessage(ctx, result.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Body != "sensitive body" {
		t.Fatalf("authorized detail body = %q", detail.Body)
	}
}

func TestServiceConnectionTestDoesNotCreateMessageAndDoesNotReturnSecret(t *testing.T) {
	accounts := NewMemoryAccountRepository()
	messages := NewMemoryMessageRepository()
	provider := &testProvider{}
	svc := NewService(accounts, messages, provider, Config{Cipher: testCipher{}})
	ctx := testMailContext(t)
	account, err := svc.CreateAccount(ctx, AccountInput{Name: "primary", Host: "smtp.example.test", Port: 465, Username: "fixture-user", Password: "fixture-password", FromEmail: "no-reply@example.test", Weight: 2, Enabled: true, ImplicitTLS: true})
	if err != nil {
		t.Fatal(err)
	}
	testResult, err := svc.TestAccount(ctx, account.ID, "request-fixture")
	if err != nil {
		t.Fatal(err)
	}
	if testResult.Status != "ok" || provider.tests != 1 {
		t.Fatalf("test result=%#v tests=%d", testResult, provider.tests)
	}
	if strings.Contains(testResult.Message, "fixture-password") {
		t.Fatalf("connection result leaked secret: %#v", testResult)
	}
	page, err := svc.ListMessages(ctx, MessageFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 {
		t.Fatalf("connection test created %d messages", page.Total)
	}
}

func TestServiceRejectsDuplicateAccountNameWithinTenant(t *testing.T) {
	accounts := NewMemoryAccountRepository()
	svc := NewService(accounts, NewMemoryMessageRepository(), &testProvider{}, Config{Cipher: testCipher{}})
	ctx := testMailContext(t)
	input := AccountInput{Name: "primary", Host: "smtp.example.test", Port: 25, FromEmail: "no-reply@example.test", Enabled: true}
	if _, err := svc.CreateAccount(ctx, input); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateAccount(ctx, input); !errors.Is(err, ErrAccountConflict) {
		t.Fatalf("duplicate error=%v, want ErrAccountConflict", err)
	}
}

func TestServicePersistsFailedRecordWhenNoEnabledAccountExists(t *testing.T) {
	accounts := NewMemoryAccountRepository()
	messages := NewMemoryMessageRepository()
	svc := NewService(accounts, messages, &testProvider{}, Config{Cipher: testCipher{}, RetryDelays: []time.Duration{0}})
	ctx := testMailContext(t)
	if _, err := svc.CreateAccount(ctx, AccountInput{Name: "disabled", Host: "smtp.example.test", Port: 25, FromEmail: "no-reply@example.test", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	view, err := svc.Send(ctx, SendInput{To: "recipient@example.test", Subject: "failed fixture", Body: "body"})
	if !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("Send() error = %v, want ErrDeliveryFailed", err)
	}
	if view.Status != StatusFailed || view.ID == "" {
		t.Fatalf("failed view = %#v", view)
	}
	page, pageErr := svc.ListMessages(ctx, MessageFilter{})
	if pageErr != nil || page.Total != 1 || page.Items[0].Status != StatusFailed {
		t.Fatalf("failed page = %#v, err=%v", page, pageErr)
	}
}

func TestServiceRequiresPasswordWhenChangingSMTPUsername(t *testing.T) {
	accounts := NewMemoryAccountRepository()
	svc := NewService(accounts, NewMemoryMessageRepository(), &testProvider{}, Config{Cipher: testCipher{}})
	ctx := testMailContext(t)
	account, err := svc.CreateAccount(ctx, AccountInput{Name: "primary", Host: "smtp.example.test", Port: 587, Username: "old-user", Password: "old-password", FromEmail: "no-reply@example.test", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	input := AccountInput{Name: account.Name, Host: account.Host, Port: account.Port, Username: "new-user", FromEmail: account.FromEmail, Enabled: true}
	if _, err := svc.UpdateAccount(ctx, account.ID, input); !errors.Is(err, ErrInvalidAccount) {
		t.Fatalf("UpdateAccount() error = %v, want ErrInvalidAccount", err)
	}
}

func TestMemoryMessageFilterSupportsRecordTabs(t *testing.T) {
	repo := NewMemoryMessageRepository()
	ctx := testMailContext(t)
	now := time.Now().UTC()
	fixtures := []EmailMessage{
		{ID: "business-1", TenantID: "tenant-a", OrgID: "org-a", CallerKey: "auth.login", TemplateKey: "security.login", SMTPAccountID: "smtp-a", Subject: "Login notice", Recipients: []Recipient{{Address: "alice@example.test", Kind: "to"}}, Status: StatusSent, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)},
		{ID: "test-1", TenantID: "tenant-a", OrgID: "org-a", IsTest: true, SMTPAccountID: "smtp-b", Subject: "Template preview", Recipients: []Recipient{{Address: "bob@example.test", Kind: "to"}}, Status: StatusFailed, CreatedAt: now.Add(-2 * time.Hour), UpdatedAt: now.Add(-2 * time.Hour)},
		{ID: "system-1", TenantID: "tenant-a", OrgID: "org-a", SMTPAccountID: "smtp-a", Subject: "System maintenance", Recipients: []Recipient{{Address: "ops@example.test", Kind: "to"}}, Status: StatusPending, CreatedAt: now.Add(-3 * time.Hour), UpdatedAt: now.Add(-3 * time.Hour)},
	}
	for _, fixture := range fixtures {
		if _, err := repo.Create(ctx, fixture); err != nil {
			t.Fatal(err)
		}
	}
	page, err := repo.List(ctx, "tenant-a", "org-a", MessageFilter{Source: "template_test", Keyword: "bob@", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != "test-1" {
		t.Fatalf("filtered page = %#v", page)
	}
	page, err = repo.List(ctx, "tenant-a", "org-a", MessageFilter{CallerKey: "auth.login", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != "business-1" {
		t.Fatalf("caller page = %#v", page)
	}
	from := now.Add(-90 * time.Minute)
	page, err = repo.List(ctx, "tenant-a", "org-a", MessageFilter{AccountID: "smtp-a", From: &from, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || page.Items[0].ID != "business-1" {
		t.Fatalf("account/time page = %#v", page)
	}
}

func TestServiceNilReceiverSendReturnsRepositoryError(t *testing.T) {
	var service *Service
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a"})
	_, err := service.Send(ctx, SendInput{IdempotencyKey: "retry-1", Recipients: []string{"user@example.test"}, Subject: "subject", Body: "body"})
	if !errors.Is(err, ErrRepositoryFailure) {
		t.Fatalf("error=%v, want ErrRepositoryFailure", err)
	}
}
