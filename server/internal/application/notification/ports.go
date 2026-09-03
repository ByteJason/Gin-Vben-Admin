package notification

import (
	"context"
	"time"
)

// contextMetadataKey is intentionally private to this package. Bootstrap and
// trusted application adapters can install capability metadata with
// WithContextMetadata; ordinary transport input is never read here.
type contextMetadataKey struct{}

type ContextMetadata struct {
	CallerKey   string
	Locale      string
	TraceID     string
	PrincipalID string
}

func WithContextMetadata(ctx context.Context, metadata ContextMetadata) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, contextMetadataKey{}, metadata)
}

func ContextMetadataFromContext(ctx context.Context) ContextMetadata {
	if ctx == nil {
		return ContextMetadata{}
	}
	metadata, _ := ctx.Value(contextMetadataKey{}).(ContextMetadata)
	return metadata
}

// SendMode selects normal production delivery or an explicitly controlled test.
type SendMode string

const (
	SendModeProduction SendMode = "Production"
	SendModeAdminTest  SendMode = "AdminTest"
	// Production and AdminTest are concise aliases used by integration code.
	Production = SendModeProduction
	AdminTest  = SendModeAdminTest
)

type DeliveryStatus string

const (
	DeliveryQueued       DeliveryStatus = "queued"
	DeliverySent         DeliveryStatus = "sent"
	DeliveryFailed       DeliveryStatus = "failed"
	DeliveryStatusQueued                = DeliveryQueued
	DeliveryStatusSent                  = DeliverySent
	DeliveryStatusFailed                = DeliveryFailed
)

type Recipient struct {
	Address string `json:"address"`
	Kind    string `json:"kind"`
}

type SendResult struct {
	MessageID          string         `json:"messageId"`
	Status             DeliveryStatus `json:"status"`
	PolicyGeneration   string         `json:"policyGeneration,omitempty"`
	TemplateGeneration string         `json:"templateGeneration,omitempty"`
	IsTest             bool           `json:"isTest,omitempty"`
}

type NotificationRequest struct {
	CallerKey      string            `json:"callerKey,omitempty"`
	Purpose        string            `json:"purpose"`
	Recipients     []Recipient       `json:"recipients"`
	Variables      map[string]string `json:"variables,omitempty"`
	Locale         string            `json:"locale,omitempty"`
	IdempotencyKey string            `json:"idempotencyKey,omitempty"`
	Mode           SendMode          `json:"mode,omitempty"`
	// ChallengeID is an internal correlation field populated by Issue; public
	// callers generally leave it empty.
	ChallengeID string `json:"challengeId,omitempty"`
}

type NotificationService interface {
	Send(context.Context, NotificationRequest) (SendResult, error)
}

type IssueRequest struct {
	CallerKey      string            `json:"callerKey,omitempty"`
	Purpose        string            `json:"purpose"`
	Recipient      string            `json:"recipient"`
	Locale         string            `json:"locale,omitempty"`
	Variables      map[string]string `json:"variables,omitempty"`
	IdempotencyKey string            `json:"idempotencyKey,omitempty"`
}

type ChallengeRef struct {
	ID        string    `json:"id"`
	ExpiresAt time.Time `json:"expiresAt"`
	Status    string    `json:"status"`
}

type VerifyRequest struct {
	ChallengeID    string `json:"challengeId"`
	Code           string `json:"code"`
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
}

type VerificationCodeService interface {
	Issue(context.Context, IssueRequest) (ChallengeRef, error)
	Verify(context.Context, VerifyRequest) error
}
