package notificationplatform

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	appnotification "example.com/gin-vben-admin/server/internal/application/notification"
)

func TestSMTPMailerDeliversMessageToIsolatedFixture(t *testing.T) {
	fixture := newSMTPFixture(t)
	mailer, err := NewSMTPMailer(appnotification.SMTPConfig{
		Host: "127.0.0.1",
		Port: 1025,
		From: "no-reply@example.test",
	})
	if err != nil {
		t.Fatalf("NewSMTPMailer() error = %v", err)
	}
	mailer.dialContext = fixture.dial

	err = mailer.Send(context.Background(), appnotification.Message{
		ID:      "fixture-message-1",
		To:      "user@example.test",
		Subject: "Password reset",
		Body:    "reset token fixture",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	message := fixture.waitMessage(t)
	for _, want := range []string{
		"from:<no-reply@example.test>",
		"rcpt:<user@example.test>",
		"Subject: Password reset",
		"reset token fixture",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("fixture message missing %q: %s", want, message)
		}
	}
}

func TestSMTPMailerDeliversAllRecipientsInOneTransaction(t *testing.T) {
	fixture := newSMTPFixture(t)
	mailer, err := NewSMTPMailer(appnotification.SMTPConfig{
		Host: "127.0.0.1",
		Port: 1025,
		From: "no-reply@example.test",
	})
	if err != nil {
		t.Fatalf("NewSMTPMailer() error = %v", err)
	}
	mailer.dialContext = fixture.dial

	err = mailer.Send(context.Background(), appnotification.Message{
		ID:         "fixture-message-multi",
		To:         "first@example.test",
		Recipients: []string{"first@example.test", "second@example.test"},
		Subject:    "multiple recipients",
		Body:       "fixture body",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	message := fixture.waitMessage(t)
	for _, want := range []string{"rcpt:<first@example.test>", "rcpt:<second@example.test>"} {
		if !strings.Contains(message, want) {
			t.Fatalf("fixture message missing %q: %s", want, message)
		}
	}
}

func TestSMTPMailerRejectsHeaderInjectionAndDoesNotExposePassword(t *testing.T) {
	_, err := NewSMTPMailer(appnotification.SMTPConfig{
		Host:     "127.0.0.1",
		Port:     2525,
		Username: "fixture-user",
		Password: "fixture-secret",
		From:     "no-reply@example.test",
	})
	if err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	mailer, _ := NewSMTPMailer(appnotification.SMTPConfig{Host: "127.0.0.1", Port: 2525, From: "no-reply@example.test"})
	err = mailer.Send(context.Background(), appnotification.Message{To: "user@example.test", Subject: "bad\r\nBcc: leak@example.test", Body: "x"})
	if !errors.Is(err, appnotification.ErrInvalidMessage) {
		t.Fatalf("header injection error = %v, want ErrInvalidMessage", err)
	}
	if strings.Contains(err.Error(), "fixture-secret") {
		t.Fatalf("SMTP error exposed password: %v", err)
	}
}

func TestSMTPMailerHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	mailer, err := NewSMTPMailer(appnotification.SMTPConfig{Host: "127.0.0.1", Port: 2525, From: "no-reply@example.test"})
	if err != nil {
		t.Fatalf("NewSMTPMailer() error = %v", err)
	}
	if err := mailer.Send(ctx, appnotification.Message{To: "user@example.test", Subject: "x", Body: "y"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Send() error = %v, want context.Canceled", err)
	}
}

func TestSMTPPoolSelectionIsDeterministicAndWeighted(t *testing.T) {
	p, err := NewSMTPPoolMailer(SMTPPoolConfig{Selection: appnotification.SMTPSelectionRoundRobin, Accounts: []appnotification.SMTPAccount{
		{Enabled: true, Name: "a", Host: "a.test", Port: 25, FromEmail: "a@example.test"},
		{Enabled: true, Name: "b", Host: "b.test", Port: 25, FromEmail: "b@example.test"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.pick()[0].Name; got != "a" {
		t.Fatalf("first round-robin=%q", got)
	}
	if got := p.pick()[0].Name; got != "b" {
		t.Fatalf("second round-robin=%q", got)
	}
	p, err = NewSMTPPoolMailer(SMTPPoolConfig{Accounts: []appnotification.SMTPAccount{
		{Enabled: true, Name: "a", Host: "a.test", Port: 25, Weight: 3, FromEmail: "a@example.test"},
		{Enabled: true, Name: "b", Host: "b.test", Port: 25, Weight: 1, FromEmail: "b@example.test"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	p.SetRNG(func(int) int { return 3 })
	if got := p.pick()[0].Name; got != "b" {
		t.Fatalf("weighted pick=%q", got)
	}
}

func TestSMTPPoolCapsAttemptsAndCoolsFailedAccounts(t *testing.T) {
	p, err := NewSMTPPoolMailer(SMTPPoolConfig{Selection: appnotification.SMTPSelectionRoundRobin, Accounts: []appnotification.SMTPAccount{
		{Enabled: true, Name: "a", TenantID: "tenant", Host: "a.test", Port: 587, Username: "user-a", Password: "pass-a", FromEmail: "a@example.test"},
		{Enabled: true, Name: "b", TenantID: "tenant", Host: "b.test", Port: 587, Username: "user-b", Password: "pass-b", FromEmail: "b@example.test"},
		{Enabled: true, Name: "c", TenantID: "tenant", Host: "c.test", Port: 587, Username: "user-c", Password: "pass-c", FromEmail: "c@example.test"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	p.SetRetryPolicy(3, []time.Duration{0})
	p.SetCooldown(time.Minute)
	now := time.Now()
	p.SetClock(func() time.Time { return now })
	var dials int
	p.SetDialContext(func(context.Context, string, string) (net.Conn, error) {
		dials++
		return nil, errors.New("fixture dial failure")
	})
	msg := appnotification.Message{To: "user@example.test", Subject: "fixture", Body: "fixture"}
	if err := p.Send(context.Background(), msg); !errors.Is(err, appnotification.ErrProvider) {
		t.Fatalf("Send() error = %v, want provider error", err)
	}
	if dials != 3 {
		t.Fatalf("dials=%d, want max attempts 3", dials)
	}
	if err := p.Send(context.Background(), msg); !errors.Is(err, appnotification.ErrProvider) {
		t.Fatalf("cooled Send() error = %v, want provider error", err)
	}
	if dials != 3 {
		t.Fatalf("cooled dials=%d, want no additional attempts", dials)
	}
}

type smtpFixture struct {
	client net.Conn
	server net.Conn
	seen   chan string
	once   sync.Once
}

func newSMTPFixture(t *testing.T) *smtpFixture {
	t.Helper()
	client, server := net.Pipe()
	fixture := &smtpFixture{client: client, server: server, seen: make(chan string, 1)}
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})
	go fixture.serve(t)
	return fixture
}

func (f *smtpFixture) dial(context.Context, string, string) (net.Conn, error) {
	var connection net.Conn
	f.once.Do(func() { connection = f.client })
	if connection == nil {
		return nil, errors.New("fixture connection already consumed")
	}
	return connection, nil
}

func (f *smtpFixture) serve(t *testing.T) {
	t.Helper()
	conn := f.server
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	write := func(line string) {
		_, _ = fmt.Fprintf(writer, "%s\r\n", line)
		_ = writer.Flush()
	}
	write("220 fixture ESMTP")
	var body strings.Builder
	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			return
		}
		trimmed := strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(trimmed, "EHLO"), strings.HasPrefix(trimmed, "HELO"):
			write("250-fixture")
			write("250-8BITMIME")
			write("250 OK")
		case strings.HasPrefix(trimmed, "MAIL FROM:"):
			body.WriteString("from:")
			body.WriteString(strings.TrimSpace(strings.TrimPrefix(trimmed, "MAIL FROM:")))
			body.WriteByte('\n')
			write("250 OK")
		case strings.HasPrefix(trimmed, "RCPT TO:"):
			body.WriteString("rcpt:")
			body.WriteString(strings.TrimSpace(strings.TrimPrefix(trimmed, "RCPT TO:")))
			body.WriteByte('\n')
			write("250 OK")
		case trimmed == "DATA":
			write("354 End data with <CR><LF>.<CR><LF>")
			for {
				dataLine, dataErr := reader.ReadString('\n')
				if dataErr != nil {
					return
				}
				dataLine = strings.TrimRight(dataLine, "\r\n")
				if dataLine == "." {
					break
				}
				body.WriteString(dataLine)
				body.WriteByte('\n')
			}
			f.seen <- body.String()
			write("250 queued")
		case trimmed == "QUIT":
			write("221 bye")
			return
		default:
			write("250 OK")
		}
	}
}

func (f *smtpFixture) waitMessage(t *testing.T) string {
	t.Helper()
	select {
	case message := <-f.seen:
		return message
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SMTP fixture message")
		return ""
	}
}
