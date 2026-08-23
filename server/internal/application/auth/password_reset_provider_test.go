package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/application/auth"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
)

func TestMemoryPasswordResetProviderDeliversAndConsumesTokenOnce(t *testing.T) {
	var deliveredIdentifier, deliveredToken string
	provider := auth.NewMemoryPasswordResetProvider(time.Minute, func(_ context.Context, identifier, token string) error {
		deliveredIdentifier = identifier
		deliveredToken = token
		return nil
	})

	if err := provider.Request(context.Background(), "alice"); err != nil {
		t.Fatalf("Request() error = %v", err)
	}
	if deliveredIdentifier != "alice" || len(deliveredToken) < 32 {
		t.Fatalf("delivery = identifier:%q token-length:%d", deliveredIdentifier, len(deliveredToken))
	}
	identifier, err := provider.Consume(context.Background(), deliveredToken)
	if err != nil || identifier != "alice" {
		t.Fatalf("Consume() = %q, %v", identifier, err)
	}
	if _, err := provider.Consume(context.Background(), deliveredToken); !errors.Is(err, authdomain.ErrPasswordResetInvalid) {
		t.Fatalf("second Consume() error = %v, want ErrPasswordResetInvalid", err)
	}
}

func TestMemoryPasswordResetProviderRemovesTokenWhenDeliveryFails(t *testing.T) {
	deliveryErr := errors.New("delivery unavailable")
	var deliveredToken string
	provider := auth.NewMemoryPasswordResetProvider(time.Minute, func(_ context.Context, _ string, token string) error {
		deliveredToken = token
		return deliveryErr
	})

	if err := provider.Request(context.Background(), "alice"); !errors.Is(err, deliveryErr) {
		t.Fatalf("Request() error = %v, want delivery error", err)
	}
	if _, err := provider.Consume(context.Background(), deliveredToken); !errors.Is(err, authdomain.ErrPasswordResetInvalid) {
		t.Fatalf("Consume(delivery-failed token) error = %v", err)
	}
}
