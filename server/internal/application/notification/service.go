// Package notification defines provider-independent messaging contracts. SMTP
// and other transports implement Mailer; this layer owns validation and safe
// delivery status semantics, not network configuration or retries.
package notification

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
)

var (
	ErrInvalidMessage = errors.New("invalid notification message")
	ErrProvider       = errors.New("notification provider unavailable")
)

type Status string

const (
	StatusPending Status = "pending"
	StatusSent    Status = "sent"
	StatusFailed  Status = "failed"
)

type Message struct {
	ID        string    `json:"id"`
	To        string    `json:"to"`
	Subject   string    `json:"subject"`
	Body      string    `json:"-"`
	Status    Status    `json:"status"`
	ErrorCode string    `json:"errorCode,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type SendInput struct {
	To      string
	Subject string
	Body    string
}

type Mailer interface {
	Send(context.Context, Message) error
}

type Service struct{ mailer Mailer }

func NewService(mailer Mailer) *Service { return &Service{mailer: mailer} }

func (s *Service) Send(ctx context.Context, input SendInput) (Message, error) {
	if s == nil || s.mailer == nil || !strings.Contains(input.To, "@") || strings.TrimSpace(input.Subject) == "" {
		return Message{}, ErrInvalidMessage
	}
	id, err := newID()
	if err != nil {
		return Message{}, fmt.Errorf("create notification id: %w", err)
	}
	m := Message{ID: id, To: strings.TrimSpace(input.To), Subject: strings.TrimSpace(input.Subject), Body: input.Body, Status: StatusPending, CreatedAt: time.Now().UTC()}
	if err := s.mailer.Send(ctx, m); err != nil {
		m.Status = StatusFailed
		m.ErrorCode = "provider_unavailable"
		m.Body = ""
		return m, errors.Join(ErrProvider, err)
	}
	m.Status = StatusSent
	m.Body = ""
	return m, nil
}

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	StartTLS bool
}

func (c SMTPConfig) Validate() error {
	if strings.TrimSpace(c.Host) == "" || c.Port <= 0 || c.Port > 65535 || strings.ContainsAny(c.Host+c.Username+c.Password+c.From, "\r\n") {
		return ErrInvalidMessage
	}
	if _, err := mail.ParseAddress(strings.TrimSpace(c.From)); err != nil {
		return ErrInvalidMessage
	}
	return nil
}

func newID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}
