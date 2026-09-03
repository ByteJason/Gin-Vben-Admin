package mail

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"text/template"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/application/notification"
)

// TemplateRenderer is the narrow bridge between template management and the
// legacy account/delivery service. Implementations may resolve a database
// snapshot, the notification Runtime, or a deterministic fixture.
type TemplateRenderer interface {
	Render(context.Context, string, string, map[string]string) (subject, body, generation string, err error)
}

// ServiceSender adapts the existing tenant-scoped Service to the public
// MailSender port without changing the compatibility Send method signature.
// The adapter owns no SMTP credentials and delegates account selection,
// retries, encryption and delivery records to Service.
type ServiceSender struct {
	service  *Service
	renderer TemplateRenderer
}

// NotificationMailer bridges the provider-independent notification runtime to
// the legacy durable mail service used by the bootstrap composition root.
type NotificationMailer struct{ Service *Service }

func (m NotificationMailer) Send(ctx context.Context, message notification.Message) error {
	if m.Service == nil {
		return notification.ErrProvider
	}
	recipients := append([]string(nil), message.Recipients...)
	if len(recipients) == 0 && strings.TrimSpace(message.To) != "" {
		recipients = []string{message.To}
	}
	_, err := m.Service.Send(ctx, SendInput{
		Recipients:         recipients,
		Subject:            message.Subject,
		Body:               message.Body,
		CallerKey:          message.CallerKey,
		TemplateKey:        message.TemplateKey,
		TemplateGeneration: message.TemplateGeneration,
		PolicyGeneration:   message.PolicyGeneration,
		Locale:             message.Locale,
		Mode:               message.Mode,
		IsTest:             message.IsTest,
		ChallengeID:        message.ChallengeID,
		IdempotencyKey:     message.IdempotencyKey,
	})
	return err
}

func NewServiceSender(service *Service, renderer TemplateRenderer) *ServiceSender {
	return &ServiceSender{service: service, renderer: renderer}
}

// NewMailSender is the descriptive constructor used by bootstrap wiring.
func NewMailSender(service *Service, renderer TemplateRenderer) *ServiceSender {
	return NewServiceSender(service, renderer)
}

func (s *ServiceSender) Send(ctx context.Context, request SendRequest) (SendResult, error) {
	if s == nil || s.service == nil || s.renderer == nil || strings.TrimSpace(request.TemplateKey) == "" {
		return SendResult{}, notification.ErrTemplateNotFound
	}
	if len(request.Recipients) == 0 {
		return SendResult{}, notification.ErrInvalidRecipient
	}
	subject, body, generation, err := s.renderer.Render(ctx, strings.TrimSpace(request.TemplateKey), request.Locale, request.Variables)
	if err != nil {
		return SendResult{}, err
	}
	addresses := make([]string, 0, len(request.Recipients))
	for _, recipient := range request.Recipients {
		if strings.TrimSpace(recipient.Address) == "" {
			return SendResult{}, notification.ErrInvalidRecipient
		}
		addresses = append(addresses, strings.TrimSpace(recipient.Address))
	}
	view, err := s.service.Send(ctx, SendInput{
		Recipients:         addresses,
		Subject:            subject,
		Body:               body,
		IdempotencyKey:     request.IdempotencyKey,
		CallerKey:          request.CallerKey,
		TemplateKey:        request.TemplateKey,
		TemplateGeneration: generation,
		Locale:             request.Locale,
		Mode:               request.Mode,
		IsTest:             request.Mode == notification.SendModeAdminTest,
	})
	if err != nil {
		result := SendResult{MessageID: view.ID, Status: notification.DeliveryFailed, TemplateGeneration: generation}
		return result, err
	}
	status := notification.DeliverySent
	if view.Status != StatusSent {
		status = notification.DeliveryFailed
	}
	return SendResult{MessageID: view.ID, Status: status, TemplateGeneration: generation}, nil
}

// MapTemplateRenderer is a small in-memory renderer suitable for development
// and tests. It uses notification's same variable-safe rendering semantics by
// delegating to a Runtime when one is supplied.
type MapTemplateRenderer struct {
	Templates map[string]TemplateView
}

type TemplateView struct {
	Subject string
	Body    string
}

func (r MapTemplateRenderer) Render(_ context.Context, key, _ string, variables map[string]string) (string, string, string, error) {
	value, ok := r.Templates[strings.TrimSpace(key)]
	if !ok {
		return "", "", "", notification.ErrTemplateNotFound
	}
	render := func(source string) (string, error) {
		parsed, err := template.New("mail").Option("missingkey=error").Parse(source)
		if err != nil {
			return "", errors.New("invalid template")
		}
		var output bytes.Buffer
		if err := parsed.Execute(&output, variables); err != nil {
			return "", errors.New("template variables are required")
		}
		return output.String(), nil
	}
	subject, err := render(value.Subject)
	if err != nil {
		return "", "", "", err
	}
	body, err := render(value.Body)
	if err != nil {
		return "", "", "", err
	}
	return subject, body, "", nil
}

var _ MailSender = (*ServiceSender)(nil)
