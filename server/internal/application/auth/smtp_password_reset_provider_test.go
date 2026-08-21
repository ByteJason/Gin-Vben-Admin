package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	appauth "example.com/gin-vben-admin/server/internal/application/auth"
	appnotification "example.com/gin-vben-admin/server/internal/application/notification"
	"example.com/gin-vben-admin/server/internal/domain/authdomain"
)

type resetRecordingMailer struct {
	messages []appnotification.Message
	err      error
}

func (m *resetRecordingMailer) Send(_ context.Context, message appnotification.Message) error {
	if m.err != nil {
		return m.err
	}
	m.messages = append(m.messages, message)
	return nil
}

func TestSMTPPasswordResetProviderDeliversOneTimeTokenToProfileEmail(t *testing.T) {
	mailer := &resetRecordingMailer{}
	notifications := appnotification.NewService(mailer)
	provider := appauth.NewSMTPPasswordResetProvider(time.Minute, notifications)

	if err := provider.RequestTo(context.Background(), "alice", "alice@example.test"); err != nil {
		t.Fatalf("RequestTo() error = %v", err)
	}
	if len(mailer.messages) != 1 {
		t.Fatalf("delivered messages = %d, want 1", len(mailer.messages))
	}
	message := mailer.messages[0]
	if message.To != "alice@example.test" || message.Status != appnotification.StatusPending {
		t.Fatalf("delivered message = %+v", message)
	}
	if !strings.Contains(message.Body, "alice") || strings.TrimSpace(message.Body) == "" {
		t.Fatalf("reset body missing identifier: %q", message.Body)
	}
	token := tokenFromBody(message.Body)
	if token == "" {
		t.Fatalf("reset body did not contain a token: %q", message.Body)
	}
	identifier, err := provider.Consume(context.Background(), token)
	if err != nil || identifier != "alice" {
		t.Fatalf("Consume() = %q, %v", identifier, err)
	}
	if _, err := provider.Consume(context.Background(), token); !errors.Is(err, authdomain.ErrPasswordResetInvalid) {
		t.Fatalf("second Consume() error = %v", err)
	}
}

func TestSMTPPasswordResetProviderRemovesTokenWhenNotificationFails(t *testing.T) {
	mailer := &resetRecordingMailer{err: errors.New("fixture unavailable")}
	provider := appauth.NewSMTPPasswordResetProvider(time.Minute, appnotification.NewService(mailer))

	if err := provider.RequestTo(context.Background(), "alice", "alice@example.test"); err == nil {
		t.Fatal("RequestTo() error = nil, want delivery failure")
	}
	if _, err := provider.Consume(context.Background(), "fixture-token"); !errors.Is(err, authdomain.ErrPasswordResetInvalid) {
		t.Fatalf("Consume() error = %v, want invalid token", err)
	}
}

func TestPasswordResetUsesProfileEmailWithRecipientProvider(t *testing.T) {
	repo := &recordingRecoveryRepo{user: authdomain.User{ID: "1", Identifier: "alice", Email: "Alice@Example.TEST", Active: true}}
	provider := &recipientProvider{}
	svc := appauth.NewService(repo, nil, nil, nil)
	svc.SetPasswordResetProvider(provider)

	if err := svc.RequestPasswordReset(context.Background(), " alice "); err != nil {
		t.Fatalf("RequestPasswordReset() error = %v", err)
	}
	if provider.identifier != "alice" || provider.recipient != "Alice@Example.TEST" {
		t.Fatalf("recipient delivery = identifier:%q recipient:%q", provider.identifier, provider.recipient)
	}
}

type recipientProvider struct {
	identifier string
	recipient  string
}

func (p *recipientProvider) Request(context.Context, string) error {
	return errors.New("legacy path used")
}
func (p *recipientProvider) RequestTo(_ context.Context, identifier, recipient string) error {
	p.identifier = identifier
	p.recipient = recipient
	return nil
}
func (p *recipientProvider) Consume(context.Context, string) (string, error) {
	return "", authdomain.ErrPasswordResetInvalid
}

func tokenFromBody(body string) string {
	const prefix = "One-time token: "
	index := strings.Index(body, prefix)
	if index < 0 {
		return ""
	}
	return strings.TrimSpace(strings.Split(strings.TrimSpace(body[index+len(prefix):]), "\n")[0])
}
