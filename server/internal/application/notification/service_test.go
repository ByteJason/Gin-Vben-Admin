package notification

import (
	"context"
	"errors"
	"testing"
)

type recordingMailer struct{ messages []Message }

func (m *recordingMailer) Send(_ context.Context, message Message) error {
	m.messages = append(m.messages, message)
	return nil
}

func TestServiceSendsAndAuditsMessageWithoutExposingBodyInStatus(t *testing.T) {
	mailer := &recordingMailer{}
	service := NewService(mailer)
	message, err := service.Send(context.Background(), SendInput{To: "user@example.com", Subject: "Welcome", Body: "secret body"})
	if err != nil || message.Status != StatusSent || len(mailer.messages) != 1 {
		t.Fatalf("message=%+v err=%v mailer=%+v", message, err, mailer.messages)
	}
	if message.Body != "" {
		t.Fatalf("message status leaked body: %+v", message)
	}
}

func TestServiceRejectsInvalidRecipientAndProviderErrors(t *testing.T) {
	service := NewService(failingMailer{err: errors.New("provider down")})
	if _, err := service.Send(context.Background(), SendInput{To: "", Subject: "x"}); !errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("invalid recipient error=%v", err)
	}
	message, err := service.Send(context.Background(), SendInput{To: "u@example.com", Subject: "x"})
	if err == nil || message.Status != StatusFailed {
		t.Fatalf("provider result message=%+v err=%v", message, err)
	}
}

type failingMailer struct{ err error }

func (m failingMailer) Send(context.Context, Message) error { return m.err }
