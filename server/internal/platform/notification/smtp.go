// Package notificationplatform contains concrete notification transports.
package notificationplatform

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	appnotification "example.com/gin-vben-admin/server/internal/application/notification"
)

// SMTPMailer sends application messages over a single SMTP connection. It
// deliberately owns no retry loop or queue; those belong to a later worker
// slice. Errors are reduced to stable provider categories so credentials and
// remote server details never cross the application boundary.
type SMTPMailer struct {
	config      appnotification.SMTPConfig
	dialContext func(context.Context, string, string) (net.Conn, error)
}

// NewSMTPMailer validates configuration without opening a network connection.
// Mailpit uses StartTLS=false and no credentials; production SMTP can opt into
// STARTTLS and AUTH through the same seam.
func NewSMTPMailer(cfg appnotification.SMTPConfig) (*SMTPMailer, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if strings.ContainsAny(cfg.Host+cfg.Username+cfg.Password+cfg.From, "\r\n") {
		return nil, appnotification.ErrInvalidMessage
	}
	if _, err := mail.ParseAddress(strings.TrimSpace(cfg.From)); err != nil {
		return nil, appnotification.ErrInvalidMessage
	}
	return &SMTPMailer{
		config:      cfg,
		dialContext: (&net.Dialer{}).DialContext,
	}, nil
}

// Send implements notification.Mailer. The body is written only to the SMTP
// DATA stream and is never included in an error or status payload.
func (m *SMTPMailer) Send(ctx context.Context, message appnotification.Message) error {
	if m == nil || m.dialContext == nil || ctx == nil {
		return appnotification.ErrInvalidMessage
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	to := strings.TrimSpace(message.To)
	subject := strings.TrimSpace(message.Subject)
	if to == "" || subject == "" || strings.ContainsAny(to+subject, "\r\n") {
		return appnotification.ErrInvalidMessage
	}
	recipient, err := mail.ParseAddress(to)
	if err != nil || strings.TrimSpace(recipient.Address) == "" {
		return appnotification.ErrInvalidMessage
	}

	address := net.JoinHostPort(strings.TrimSpace(m.config.Host), strconv.Itoa(m.config.Port))
	conn, err := m.dialContext(ctx, "tcp", address)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return providerFailure("dial")
	}
	done := make(chan struct{})
	go closeOnContext(ctx, conn, done)
	defer close(done)
	defer conn.Close()

	client, err := smtp.NewClient(conn, strings.TrimSpace(m.config.Host))
	if err != nil {
		return providerFailure("connect")
	}
	defer client.Close()

	if m.config.StartTLS {
		ok, _ := client.Extension("STARTTLS")
		if !ok {
			return providerFailure("starttls")
		}
		tlsConfig := &tls.Config{ServerName: strings.TrimSpace(m.config.Host), MinVersion: tls.VersionTLS12}
		if err := client.StartTLS(tlsConfig); err != nil {
			return providerFailure("starttls")
		}
	}
	if strings.TrimSpace(m.config.Username) != "" {
		auth := smtp.PlainAuth("", m.config.Username, m.config.Password, strings.TrimSpace(m.config.Host))
		if err := client.Auth(auth); err != nil {
			return providerFailure("auth")
		}
	}
	if err := client.Mail(strings.TrimSpace(m.config.From)); err != nil {
		return providerFailure("mail")
	}
	if err := client.Rcpt(recipient.Address); err != nil {
		return providerFailure("recipient")
	}
	writer, err := client.Data()
	if err != nil {
		return providerFailure("data")
	}
	if err := writeMessage(writer, m.config.From, recipient.Address, message); err != nil {
		_ = writer.Close()
		return providerFailure("write")
	}
	if err := writer.Close(); err != nil {
		return providerFailure("commit")
	}
	if err := client.Quit(); err != nil {
		return providerFailure("quit")
	}
	return nil
}

func writeMessage(writer io.Writer, from, to string, message appnotification.Message) error {
	buffer := bufio.NewWriter(writer)
	if _, err := fmt.Fprintf(buffer, "From: %s\r\nTo: %s\r\nSubject: %s\r\n", from, to, mime.QEncoding.Encode("UTF-8", strings.TrimSpace(message.Subject))); err != nil {
		return err
	}
	if !message.CreatedAt.IsZero() {
		if _, err := fmt.Fprintf(buffer, "Date: %s\r\n", message.CreatedAt.UTC().Format(time.RFC1123Z)); err != nil {
			return err
		}
	}
	if id := strings.TrimSpace(message.ID); id != "" && !strings.ContainsAny(id, "\r\n") {
		if _, err := fmt.Fprintf(buffer, "Message-ID: <%s>\r\n", id); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(buffer, "MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n"); err != nil {
		return err
	}
	if _, err := io.WriteString(buffer, strings.ReplaceAll(message.Body, "\n", "\r\n")); err != nil {
		return err
	}
	return buffer.Flush()
}

func closeOnContext(ctx context.Context, conn net.Conn, done <-chan struct{}) {
	select {
	case <-ctx.Done():
		_ = conn.Close()
	case <-done:
	}
}

func providerFailure(stage string) error {
	return fmt.Errorf("%w: smtp %s failed", appnotification.ErrProvider, stage)
}

var _ appnotification.Mailer = (*SMTPMailer)(nil)
