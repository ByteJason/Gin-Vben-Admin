package mail

import (
	"context"
	"testing"
)

type portMailStub struct{}

func (portMailStub) Send(context.Context, SendRequest) (SendResult, error) { return SendResult{}, nil }

var _ MailSender = portMailStub{}

func TestMailPortAliasesNotificationDTOs(t *testing.T) {
	var recipient Recipient
	recipient.Address = "user@example.test"
	if recipient.Address == "" {
		t.Fatal("recipient alias unavailable")
	}
}
