package notificationplatform

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	appnotification "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/notification"
)

// TestSMTPRuntimeFixtureAcceptance is opt-in. It consumes only runtime
// SMTP_FIXTURE_* variables, sends one uniquely identified message, and never
// prints credentials or message content. Without a fixture it remains a
// deterministic skip so normal unit runs use the local SMTP fixture.
func TestSMTPRuntimeFixtureAcceptance(t *testing.T) {
	host := strings.TrimSpace(os.Getenv("SMTP_FIXTURE_HOST"))
	username := strings.TrimSpace(os.Getenv("SMTP_FIXTURE_USERNAME"))
	password := os.Getenv("SMTP_FIXTURE_PASSWORD")
	from := strings.TrimSpace(os.Getenv("SMTP_FIXTURE_FROM_EMAIL"))
	recipient := strings.TrimSpace(os.Getenv("SMTP_ACCEPTANCE_RECIPIENT"))
	if host == "" || username == "" || password == "" || from == "" || recipient == "" {
		t.Skip("SMTP_FIXTURE_* and SMTP_ACCEPTANCE_RECIPIENT are not configured")
	}
	port := 465
	if raw := strings.TrimSpace(os.Getenv("SMTP_FIXTURE_PORT")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 65535 {
			t.Fatal("SMTP_FIXTURE_PORT is invalid")
		}
		port = parsed
	}
	implicitTLS := strings.EqualFold(strings.TrimSpace(os.Getenv("SMTP_FIXTURE_IMPLICIT_TLS")), "true")
	account := appnotification.SMTPAccount{Enabled: true, Name: "SMTP_FIXTURE", Host: host, Port: port, Username: username, Password: password, Weight: 1, FromEmail: from, FromName: os.Getenv("SMTP_FIXTURE_FROM_NAME"), ImplicitTLS: implicitTLS}
	mailer, err := NewSMTPMailerFromAccount(account)
	if err != nil {
		t.Fatal("SMTP fixture configuration rejected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := mailer.TestConnection(ctx); err != nil {
		t.Fatalf("SMTP fixture connection failed: %T", err)
	}
	message := appnotification.Message{ID: fmt.Sprintf("smtp-fixture-%d", time.Now().UnixNano()), To: recipient, Recipients: []string{recipient}, Subject: "SMTP fixture acceptance", Body: "SMTP_FIXTURE_ACCEPTANCE"}
	if err := mailer.Send(ctx, message); err != nil {
		t.Fatalf("SMTP fixture delivery failed: %T", err)
	}
}
