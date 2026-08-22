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
	ID string `json:"id"`
	To string `json:"to"`
	// Recipients carries the complete envelope recipient set for transports
	// that can deliver one message to several addresses. To remains the
	// backwards-compatible single-recipient field and is used as a fallback.
	Recipients []string  `json:"-"`
	Subject    string    `json:"subject"`
	Body       string    `json:"-"`
	Status     Status    `json:"status"`
	ErrorCode  string    `json:"errorCode,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
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

// SMTPAccount describes one independently selectable SMTP sender.
type SMTPAccount struct {
	Enabled     bool
	Name        string
	TenantID    string
	Host        string
	Port        int
	Username    string
	Password    string
	Weight      int
	FromEmail   string
	FromName    string
	ImplicitTLS bool
}

type SMTPSelection string

const (
	SMTPSelectionWeightedRandom SMTPSelection = "weighted_random"
	SMTPSelectionRoundRobin     SMTPSelection = "round_robin"
)

func (a SMTPAccount) Validate() error {
	from := strings.TrimSpace(a.FromEmail)
	if from == "" || strings.TrimSpace(a.Host) == "" || a.Port <= 0 || a.Port > 65535 || a.Weight < 0 || strings.ContainsAny(a.Host+a.Username+a.Password+a.FromEmail+a.FromName+a.Name+a.TenantID, "\r\n") {
		return ErrInvalidMessage
	}
	if _, err := mail.ParseAddress(from); err != nil {
		return ErrInvalidMessage
	}
	return nil
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
