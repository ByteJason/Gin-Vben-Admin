package integration

import (
	"context"
	"os"
	"strconv"
	"testing"

	appnotification "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/notification"
	notificationplatform "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/notification"
)

// TestSMTPMailpitFixture is opt-in so the default integration suite remains
// offline. Run it with the isolated deploy/compose.mailpit.yaml profile and
// SMTP_INTEGRATION=1; no production SMTP endpoint is ever selected implicitly.
func TestSMTPMailpitFixture(t *testing.T) {
	if os.Getenv("SMTP_INTEGRATION") != "1" {
		t.Skip("set SMTP_INTEGRATION=1 to run the isolated Mailpit fixture")
	}
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := 1025
	if value := os.Getenv("SMTP_PORT"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			t.Fatalf("SMTP_PORT = %q: %v", value, err)
		}
		port = parsed
	}
	mailer, err := notificationplatform.NewSMTPMailer(appnotification.SMTPConfig{Host: host, Port: port, From: "no-reply@example.test"})
	if err != nil {
		t.Fatalf("NewSMTPMailer() error = %v", err)
	}
	service := appnotification.NewService(mailer)
	message, err := service.Send(context.Background(), appnotification.SendInput{
		To:      "fixture-recipient@example.test",
		Subject: "Mailpit fixture",
		Body:    "isolated SMTP verification",
	})
	if err != nil || message.Status != appnotification.StatusSent {
		t.Fatalf("Mailpit Send() message=%+v err=%v", message, err)
	}
}
