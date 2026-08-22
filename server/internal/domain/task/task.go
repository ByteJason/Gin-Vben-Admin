// Package task defines persisted, declarative task definitions.
package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidType          = errors.New("task type is not allowed")
	ErrInvalidPayloadSchema = errors.New("payload schema must be a JSON object")
	ErrInvalidCron          = errors.New("cron expression is invalid")
	ErrInvalidTimezone      = errors.New("timezone is invalid")
	ErrInvalidConcurrency   = errors.New("concurrency policy is invalid")
)

// TaskDefinition is a persisted declaration; execution is handled by a separate worker.
type TaskDefinition struct {
	ID                string          `json:"id"`
	TenantID          string          `json:"tenantId"`
	OrgID             string          `json:"orgId,omitempty"`
	Name              string          `json:"name"`
	Type              string          `json:"type"`
	PayloadSchema     json.RawMessage `json:"payloadSchema"`
	Cron              string          `json:"cron,omitempty"`
	Timezone          string          `json:"timezone"`
	Enabled           bool            `json:"enabled"`
	Concurrency       int             `json:"concurrency"`
	ConcurrencyPolicy string          `json:"concurrencyPolicy"`
	Timeout           time.Duration   `json:"-"`
	TimeoutSeconds    int             `json:"timeoutSeconds"`
	MaxAttempts       int             `json:"maxAttempts"`
	IdempotencyKey    string          `json:"idempotencyKey,omitempty"`
	DeletedAt         *time.Time      `json:"deletedAt,omitempty"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
}

var allowedTypes = map[string]struct{}{"manual": {}, "http": {}, "webhook": {}}

func (d TaskDefinition) Validate() error {
	if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.TenantID) == "" || strings.TrimSpace(d.Name) == "" {
		return errors.New("id, tenant_id and name are required")
	}
	if _, ok := allowedTypes[strings.ToLower(strings.TrimSpace(d.Type))]; !ok {
		return ErrInvalidType
	}
	var schema any
	if len(bytes.TrimSpace(d.PayloadSchema)) == 0 || json.Unmarshal(d.PayloadSchema, &schema) != nil {
		return ErrInvalidPayloadSchema
	}
	if _, ok := schema.(map[string]any); !ok {
		return ErrInvalidPayloadSchema
	}
	if strings.TrimSpace(d.Cron) != "" {
		if n := len(strings.Fields(d.Cron)); n < 5 || n > 7 {
			return fmt.Errorf("%w: expected 5-7 fields", ErrInvalidCron)
		}
	}
	if strings.TrimSpace(d.Timezone) == "" {
		return ErrInvalidTimezone
	}
	if _, err := time.LoadLocation(d.Timezone); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTimezone, err)
	}
	if d.Concurrency < 1 || (d.Timeout <= 0 && d.TimeoutSeconds <= 0) || d.MaxAttempts < 1 {
		return errors.New("concurrency, timeout and max_attempts must be positive")
	}
	policy := strings.ToLower(strings.TrimSpace(d.ConcurrencyPolicy))
	if policy == "" {
		policy = "forbid"
	}
	if policy != "allow" && policy != "forbid" && policy != "replace" {
		return ErrInvalidConcurrency
	}
	return nil
}
