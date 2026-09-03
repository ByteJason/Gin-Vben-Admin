package notification

import (
	"context"
	"testing"
)

type portNotificationStub struct{}

func (portNotificationStub) Send(context.Context, NotificationRequest) (SendResult, error) {
	return SendResult{}, nil
}
func (portNotificationStub) Issue(context.Context, IssueRequest) (ChallengeRef, error) {
	return ChallengeRef{}, nil
}
func (portNotificationStub) Verify(context.Context, VerifyRequest) error { return nil }

var _ NotificationService = portNotificationStub{}
var _ VerificationCodeService = portNotificationStub{}

func TestPortsExposeStableDeliveryShapes(t *testing.T) {
	if SendModeProduction != "Production" || SendModeAdminTest != "AdminTest" {
		t.Fatal("send mode values changed")
	}
	if DeliveryQueued != "queued" || DeliverySent != "sent" || DeliveryFailed != "failed" {
		t.Fatal("delivery status values changed")
	}
}
