package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var (
	ErrExecutorNotRegistered     = errors.New("task executor is not registered")
	ErrExecutorAlreadyRegistered = errors.New("task executor is already registered")
	ErrSSRFBlocked               = errors.New("task http target is blocked")
	ErrInvalidHTTPPayload        = errors.New("task http payload is invalid")
)

type ExecutionResult struct{ ResultSummary, RedactedOutput string }
type RegisteredMethod func(context.Context, json.RawMessage) (ExecutionResult, error)

// MethodExecutor contains only explicitly registered business methods. The key
// comes from a persisted task definition, never from an executable expression.
type MethodExecutor struct {
	mu      sync.RWMutex
	methods map[string]RegisteredMethod
}

func NewMethodExecutor() *MethodExecutor {
	return &MethodExecutor{methods: map[string]RegisteredMethod{}}
}
func (e *MethodExecutor) Register(key string, method RegisteredMethod) error {
	key = strings.TrimSpace(key)
	if e == nil || key == "" || method == nil {
		return ErrExecutorNotRegistered
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.methods == nil {
		e.methods = map[string]RegisteredMethod{}
	}
	if _, exists := e.methods[key]; exists {
		return ErrExecutorAlreadyRegistered
	}
	e.methods[key] = method
	return nil
}
func (e *MethodExecutor) Keys() []string {
	keys := []string{}
	if e == nil {
		return keys
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	for key := range e.methods {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
func (e *MethodExecutor) Has(key string) bool {
	if e == nil {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.methods[strings.TrimSpace(key)] != nil
}
func (e *MethodExecutor) ExecuteByKey(ctx context.Context, key string, payload json.RawMessage) (ExecutionResult, error) {
	if e == nil {
		return ExecutionResult{}, ErrExecutorNotRegistered
	}
	e.mu.RLock()
	method := e.methods[strings.TrimSpace(key)]
	e.mu.RUnlock()
	if method == nil {
		return ExecutionResult{}, ErrExecutorNotRegistered
	}
	result, err := method(ctx, append(json.RawMessage(nil), payload...))
	result.ResultSummary = RedactOutput(result.ResultSummary)
	result.RedactedOutput = RedactOutput(result.RedactedOutput)
	return result, err
}

// Execute retains the legacy envelope adapter for existing registered callers.
func (e *MethodExecutor) Execute(ctx context.Context, payload json.RawMessage) (ExecutionResult, error) {
	var input struct {
		MethodKey string          `json:"methodKey"`
		Payload   json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(payload, &input) != nil {
		return ExecutionResult{}, ErrExecutorNotRegistered
	}
	if len(input.Payload) == 0 {
		input.Payload = json.RawMessage(`{}`)
	}
	return e.ExecuteByKey(ctx, input.MethodKey, input.Payload)
}

func SensitiveKey(key string) bool {
	key = strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(key))
	for _, fragment := range []string{"authorization", "password", "passwd", "secret", "token", "cookie", "apikey", "credential", "privatekey"} {
		if strings.Contains(key, fragment) {
			return true
		}
	}
	return false
}

var credentialPattern = regexp.MustCompile(`(?i)(bearer\s+)[^\s"<>]+|((?:password|secret|token|api[_-]?key|authorization)\s*[=:]\s*)[^\s,;]+`)

// RedactOutput handles structured secret fields, known request values, and
// common plaintext credential patterns. All stored diagnostics are bounded.
func RedactOutput(value string, secrets ...string) string {
	var structured any
	if json.Unmarshal([]byte(value), &structured) == nil {
		redactValue(structured)
		if raw, err := json.Marshal(structured); err == nil {
			value = string(raw)
		}
	}
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	value = credentialPattern.ReplaceAllString(value, "${1}${2}[REDACTED]")
	const max = 32768
	if len(value) > max {
		value = strings.ToValidUTF8(value[:max], "") + "\n[truncated]"
	}
	return value
}
func redactValue(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			if SensitiveKey(key) {
				v[key] = "[REDACTED]"
			} else {
				redactValue(item)
			}
		}
	case []any:
		for _, item := range v {
			redactValue(item)
		}
	}
}
