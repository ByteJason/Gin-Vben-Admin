package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	appnotification "example.com/gin-vben-admin/server/internal/application/notification"
	"example.com/gin-vben-admin/server/internal/domain/authdomain"
)

const defaultPasswordResetSubject = "Password reset"

// SMTPPasswordResetProvider combines the hash-only, one-time token store with
// the provider-independent notification service. It performs no retries and
// removes a token whenever notification delivery fails.
type SMTPPasswordResetProvider struct {
	store *MemoryPasswordResetProvider
}

// NewSMTPPasswordResetProvider constructs an email delivery provider. The
// notification service may be nil in a disabled/development composition; such
// requests fail closed and never leave a consumable token behind.
func NewSMTPPasswordResetProvider(ttl time.Duration, notifications *appnotification.Service) *SMTPPasswordResetProvider {
	store := NewMemoryPasswordResetProviderWithRecipient(ttl, func(ctx context.Context, identifier, recipient, token string) error {
		if notifications == nil {
			return authdomain.ErrDependencyUnavailable
		}
		_, err := notifications.Send(ctx, appnotification.SendInput{
			To:      recipient,
			Subject: defaultPasswordResetSubject,
			Body:    passwordResetBody(identifier, token),
		})
		return err
	})
	return &SMTPPasswordResetProvider{store: store}
}

func (p *SMTPPasswordResetProvider) Request(ctx context.Context, identifier string) error {
	if p == nil || p.store == nil {
		return authdomain.ErrDependencyUnavailable
	}
	if strings.Contains(identifier, "@") {
		return p.store.RequestTo(ctx, identifier, identifier)
	}
	return p.store.Request(ctx, identifier)
}

func (p *SMTPPasswordResetProvider) RequestTo(ctx context.Context, identifier, recipient string) error {
	if p == nil || p.store == nil {
		return authdomain.ErrDependencyUnavailable
	}
	return p.store.RequestTo(ctx, identifier, strings.TrimSpace(recipient))
}

func (p *SMTPPasswordResetProvider) Consume(ctx context.Context, token string) (string, error) {
	if p == nil || p.store == nil {
		return "", authdomain.ErrPasswordResetInvalid
	}
	return p.store.Consume(ctx, token)
}

func passwordResetBody(identifier, token string) string {
	return fmt.Sprintf("A password reset was requested for %s.\nOne-time token: %s\nThis token can be used once and expires soon.", strings.TrimSpace(identifier), token)
}

var _ PasswordResetProvider = (*SMTPPasswordResetProvider)(nil)
var _ PasswordResetRecipientProvider = (*SMTPPasswordResetProvider)(nil)
