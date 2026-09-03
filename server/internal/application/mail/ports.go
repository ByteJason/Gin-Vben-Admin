package mail

import (
	"context"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/application/notification"
)

type SendMode = notification.SendMode
type DeliveryStatus = notification.DeliveryStatus
type Recipient = notification.Recipient
type SendResult = notification.SendResult

type SendRequest struct {
	CallerKey      string
	TemplateKey    string
	Recipients     []Recipient
	Variables      map[string]string
	Locale         string
	IdempotencyKey string
	Mode           SendMode
}

type MailSender interface {
	Send(context.Context, SendRequest) (SendResult, error)
}
