package notification

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

type runtimeMailer struct {
	mu       sync.Mutex
	messages []Message
	err      error
}

type blockingRuntimeMailer struct {
	started  chan struct{}
	release  chan struct{}
	once     sync.Once
	mu       sync.Mutex
	messages []Message
}

type countingFailingRuntimeMailer struct {
	calls atomic.Int32
	err   error
}

func (m *countingFailingRuntimeMailer) Send(_ context.Context, _ Message) error {
	m.calls.Add(1)
	return m.err
}

func (m *blockingRuntimeMailer) Send(_ context.Context, message Message) error {
	m.once.Do(func() { close(m.started) })
	<-m.release
	m.mu.Lock()
	m.messages = append(m.messages, message)
	m.mu.Unlock()
	return nil
}

func (m *runtimeMailer) Send(_ context.Context, message Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	message.Body = "[provider-body-redacted-in-test-record]"
	m.messages = append(m.messages, message)
	return nil
}

func runtimeContext() context.Context {
	ctx := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"})
	return WithContextMetadata(ctx, ContextMetadata{CallerKey: "system.admin", Locale: "zh-CN", TraceID: "trace-1"})
}

func runtimeTemplate() Template {
	return Template{
		Key: "security.password-changed", Purpose: "security.password-changed", DefaultLocale: "en-US",
		Variables: []string{"name"}, Enabled: true, Published: true,
		Locales: map[string]TemplateLocale{
			"zh-CN": {Locale: "zh-CN", Subject: "密码已修改", Body: "你好 {{.name}}"},
			"en-US": {Locale: "en-US", Subject: "Password changed", Body: "Hello {{.name}}"},
		},
	}
}

func TestRuntimeSendsPublishedLocalizedTemplateAndHonorsIdempotency(t *testing.T) {
	mailer := &runtimeMailer{}
	runtime := NewRuntime(RuntimeConfig{Mailer: mailer, StrictRegistration: true, HashKey: []byte("runtime-test-key")})
	if err := runtime.SetCaller(Caller{Key: "system.admin", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetTemplate(runtimeTemplate()); err != nil {
		t.Fatal(err)
	}
	request := NotificationRequest{CallerKey: "system.admin", Purpose: "security.password-changed", Recipients: []Recipient{{Address: "USER@example.test", Kind: "to"}}, Variables: map[string]string{"name": "Alice"}, Locale: "zh-CN", IdempotencyKey: "event-1", Mode: SendModeProduction}
	first, err := runtime.Send(runtimeContext(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtime.Send(runtimeContext(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first.Status != DeliverySent || len(mailer.messages) != 1 {
		t.Fatalf("first=%+v second=%+v messages=%d", first, second, len(mailer.messages))
	}
	message := mailer.messages[0]
	if message.To != "user@example.test" || message.Subject != "密码已修改" || message.Body != "[provider-body-redacted-in-test-record]" {
		t.Fatalf("provider message=%+v", message)
	}
	request.Variables = map[string]string{"name": "Bob"}
	if _, err := runtime.Send(runtimeContext(), request); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("changed idempotency payload error=%v", err)
	}
}

func TestRuntimeConcurrentIdempotentSendDispatchesOnce(t *testing.T) {
	mailer := &runtimeMailer{}
	runtime := NewRuntime(RuntimeConfig{Mailer: mailer, HashKey: []byte("runtime-test-key")})
	if err := runtime.SetTemplate(runtimeTemplate()); err != nil {
		t.Fatal(err)
	}
	request := NotificationRequest{Purpose: "security.password-changed", Recipients: []Recipient{{Address: "user@example.test"}}, Variables: map[string]string{"name": "Alice"}, IdempotencyKey: "same-event"}
	const workers = 8
	results := make(chan SendResult, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := runtime.Send(runtimeContext(), request)
			results <- result
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	var first SendResult
	for result := range results {
		if first.MessageID == "" {
			first = result
		}
		if result.MessageID != first.MessageID || result.Status != DeliverySent {
			t.Fatalf("inconsistent result=%+v first=%+v", result, first)
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent send error=%v", err)
		}
	}
	if len(mailer.messages) != 1 {
		t.Fatalf("provider dispatch count=%d", len(mailer.messages))
	}
}

func TestRuntimeIdempotentSendReplaysMissingMailerFailure(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{HashKey: []byte("runtime-test-key")})
	if err := runtime.SetCaller(Caller{Key: "system.admin", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetTemplate(runtimeTemplate()); err != nil {
		t.Fatal(err)
	}
	request := NotificationRequest{
		CallerKey: "system.admin", Purpose: "security.password-changed",
		Recipients: []Recipient{{Address: "user@example.test"}}, Variables: map[string]string{"name": "Alice"},
		Locale: "en-US", IdempotencyKey: "missing-mailer", Mode: SendModeProduction,
	}
	first, firstErr := runtime.Send(runtimeContext(), request)
	second, secondErr := runtime.Send(runtimeContext(), request)
	if first.MessageID == "" || first.Status != DeliveryFailed || !errors.Is(firstErr, ErrProvider) {
		t.Fatalf("first=%+v err=%v", first, firstErr)
	}
	if second != first || !errors.Is(secondErr, ErrProvider) {
		t.Fatalf("second=%+v err=%v first=%+v", second, secondErr, first)
	}
}

func TestRuntimeConcurrentIdempotentIssueSharesInFlightChallenge(t *testing.T) {
	mailer := &blockingRuntimeMailer{started: make(chan struct{}), release: make(chan struct{})}
	runtime := NewRuntime(RuntimeConfig{
		Mailer: mailer, HashKey: []byte("runtime-test-key"),
		CodeGenerator: func(int, string) (string, error) { return "246810", nil },
	})
	if err := runtime.SetCaller(Caller{Key: "system.admin", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetTemplate(Template{Key: "verify", Purpose: "verify", Enabled: true, Published: true, Variables: []string{"code", "expires_in"}, Locales: map[string]TemplateLocale{
		"en-US": {Locale: "en-US", Subject: "Code", Body: "{{.code}}"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetVerificationPolicy(VerificationPolicy{Key: "verify", Purpose: "verify", CallerKey: "system.admin"}); err != nil {
		t.Fatal(err)
	}
	ctx := WithContextMetadata(
		tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a"}),
		ContextMetadata{CallerKey: "system.admin", Locale: "en-US"},
	)
	request := IssueRequest{CallerKey: "system.admin", Purpose: "verify", Recipient: "user@example.test", IdempotencyKey: "same-challenge"}
	type issueResult struct {
		ref ChallengeRef
		err error
	}
	firstCh := make(chan issueResult, 1)
	go func() {
		ref, err := runtime.Issue(ctx, request)
		firstCh <- issueResult{ref: ref, err: err}
	}()
	select {
	case <-mailer.started:
	case <-time.After(time.Second):
		t.Fatal("first issuance did not reach provider")
	}
	secondCh := make(chan issueResult, 1)
	go func() {
		ref, err := runtime.Issue(ctx, request)
		secondCh <- issueResult{ref: ref, err: err}
	}()
	select {
	case result := <-secondCh:
		t.Fatalf("joined issuance returned before provider completion: %+v", result)
	case <-time.After(20 * time.Millisecond):
	}
	close(mailer.release)
	first := <-firstCh
	second := <-secondCh
	if first.err != nil || second.err != nil || first.ref.ID == "" || first.ref.ID != second.ref.ID || first.ref.Status != "active" || second.ref.Status != "active" {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	mailer.mu.Lock()
	count := len(mailer.messages)
	mailer.mu.Unlock()
	if count != 1 {
		t.Fatalf("provider dispatch count=%d", count)
	}
}

func TestRuntimeIdempotentIssueReplaysProviderFailure(t *testing.T) {
	providerErr := errors.New("smtp fixture failed")
	mailer := &countingFailingRuntimeMailer{err: providerErr}
	var generated atomic.Int32
	runtime := NewRuntime(RuntimeConfig{
		Mailer: mailer, HashKey: []byte("runtime-test-key"),
		CodeGenerator: func(int, string) (string, error) {
			generated.Add(1)
			return "246810", nil
		},
	})
	if err := runtime.SetCaller(Caller{Key: "system.admin", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetTemplate(Template{Key: "verify", Purpose: "verify", Enabled: true, Published: true, Variables: []string{"code", "expires_in"}, Locales: map[string]TemplateLocale{
		"en-US": {Locale: "en-US", Subject: "Code", Body: "{{.code}}"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetVerificationPolicy(VerificationPolicy{Key: "verify", Purpose: "verify", CallerKey: "system.admin"}); err != nil {
		t.Fatal(err)
	}
	ctx := WithContextMetadata(
		tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a"}),
		ContextMetadata{CallerKey: "system.admin", Locale: "en-US"},
	)
	request := IssueRequest{CallerKey: "system.admin", Purpose: "verify", Recipient: "user@example.test", IdempotencyKey: "failed-challenge"}
	first, firstErr := runtime.Issue(ctx, request)
	if first.ID == "" || first.Status != "send_failed" || !errors.Is(firstErr, ErrProvider) || !errors.Is(firstErr, providerErr) {
		t.Fatalf("first ref=%+v err=%v", first, firstErr)
	}
	second, secondErr := runtime.Issue(ctx, request)
	if second != first || !errors.Is(secondErr, ErrProvider) || !errors.Is(secondErr, providerErr) {
		t.Fatalf("second ref=%+v err=%v first=%+v", second, secondErr, first)
	}
	if got := mailer.calls.Load(); got != 1 {
		t.Fatalf("provider dispatch count=%d", got)
	}
	if got := generated.Load(); got != 1 {
		t.Fatalf("code generator count=%d", got)
	}
}

func TestRuntimePolicyUpdateRemovesStaleLookupAliases(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{StrictRegistration: true})
	if err := runtime.SetVerificationPolicy(VerificationPolicy{Key: "verify", CallerKey: "caller-a", Purpose: "old-purpose"}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetVerificationPolicy(VerificationPolicy{Key: "verify", CallerKey: "caller-b", Purpose: "new-purpose"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.resolvePolicy(context.Background(), "caller-a", "old-purpose"); !errors.Is(err, ErrPolicyNotFound) {
		t.Fatalf("stale caller/purpose alias resolved: %v", err)
	}
	updated, err := runtime.resolvePolicy(context.Background(), "caller-b", "new-purpose")
	if err != nil || updated.Key != "verify" || updated.CallerKey != "caller-b" || updated.Purpose != "new-purpose" {
		t.Fatalf("updated policy=%+v err=%v", updated, err)
	}
}

func TestRuntimeVerificationUsesDigestAndConsumesAtomically(t *testing.T) {
	mailer := &runtimeMailer{}
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	runtime := NewRuntime(RuntimeConfig{
		Mailer: mailer, HashKey: []byte("runtime-test-key"), Clock: func() time.Time { return now },
		CodeGenerator: func(int, string) (string, error) { return "246810", nil },
	})
	if err := runtime.SetTemplate(Template{Key: "email_change", Enabled: true, Published: true, Variables: []string{"code", "expires_in"}, Locales: map[string]TemplateLocale{"en-US": {Locale: "en-US", Subject: "Code", Body: "{{.code}} ({{.expires_in}})"}}}); err != nil {
		t.Fatal(err)
	}
	challenge, err := runtime.Issue(runtimeContext(), IssueRequest{CallerKey: "auth.email-change", Purpose: "email_change", Recipient: "user@example.test", IdempotencyKey: "challenge-1"})
	if err != nil {
		t.Fatal(err)
	}
	if challenge.Status != "active" || len(mailer.messages) != 1 {
		t.Fatalf("challenge=%+v messages=%d", challenge, len(mailer.messages))
	}
	if err := runtime.Verify(runtimeContext(), VerifyRequest{ChallengeID: challenge.ID, Code: "000000"}); !errors.Is(err, ErrVerificationCodeIncorrect) {
		t.Fatalf("wrong code error=%v", err)
	}
	if err := runtime.Verify(runtimeContext(), VerifyRequest{ChallengeID: challenge.ID, Code: "246810"}); err != nil {
		t.Fatalf("correct code error=%v", err)
	}
	if err := runtime.Verify(runtimeContext(), VerifyRequest{ChallengeID: challenge.ID, Code: "246810"}); !errors.Is(err, ErrVerificationConsumed) {
		t.Fatalf("second consume error=%v", err)
	}
	status, err := runtime.ChallengeStatus(challenge.ID)
	if err != nil || status.Status != "consumed" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestRuntimeVerificationUsesScopeInheritanceForTenantChallenges(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{
		Mailer: &runtimeMailer{}, HashKey: []byte("scope-test-key"),
		CodeGenerator: func(int, string) (string, error) { return "246810", nil },
	})
	if err := runtime.SetCaller(Caller{Key: "system.admin", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetTemplate(Template{Key: "verify", Purpose: "verify", Enabled: true, Published: true, Variables: []string{"code", "expires_in"}, Locales: map[string]TemplateLocale{
		"en-US": {Locale: "en-US", Subject: "Code", Body: "{{.code}}"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetVerificationPolicy(VerificationPolicy{Key: "verify", Purpose: "verify", CallerKey: "system.admin"}); err != nil {
		t.Fatal(err)
	}
	tenantContext := WithContextMetadata(
		tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a"}),
		ContextMetadata{CallerKey: "system.admin", Locale: "en-US"},
	)
	challenge, err := runtime.Issue(tenantContext, IssueRequest{Purpose: "verify", Recipient: "user@example.test", IdempotencyKey: "scope-challenge"})
	if err != nil {
		t.Fatal(err)
	}
	organizationContext := WithContextMetadata(
		tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-a", Organization: "org-a"}),
		ContextMetadata{CallerKey: "system.admin", Locale: "en-US"},
	)
	if err := runtime.Verify(organizationContext, VerifyRequest{ChallengeID: challenge.ID, Code: "246810"}); err != nil {
		t.Fatalf("tenant challenge should be visible to child organization: %v", err)
	}
	otherTenantContext := tenant.WithContext(context.Background(), tenant.Context{TenantID: "tenant-b", Organization: "org-b"})
	if _, err := runtime.ChallengeStatusFor(otherTenantContext, challenge.ID); !errors.Is(err, tenant.ErrCrossTenant) {
		t.Fatalf("cross-tenant status error=%v", err)
	}
}

func TestRuntimeRejectsMissingVariablesAndDisabledCallers(t *testing.T) {
	runtime := NewRuntime(RuntimeConfig{Mailer: &runtimeMailer{}, StrictRegistration: true})
	if err := runtime.SetCaller(Caller{Key: "disabled", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Send(context.Background(), NotificationRequest{CallerKey: "disabled", Purpose: "x", Recipients: []Recipient{{Address: "user@example.test"}}}); !errors.Is(err, ErrCallerDisabled) {
		t.Fatalf("disabled caller error=%v", err)
	}
	if err := runtime.SetCaller(Caller{Key: "enabled", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetTemplate(Template{Key: "x", Enabled: true, Published: true, Variables: []string{"name"}, Locales: map[string]TemplateLocale{"en-US": {Subject: "x", Body: "{{.name}}"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Send(context.Background(), NotificationRequest{CallerKey: "enabled", Purpose: "x", Recipients: []Recipient{{Address: "user@example.test"}}}); !errors.Is(err, ErrTemplateVariableMissing) {
		t.Fatalf("missing variable error=%v", err)
	}
}

func TestRuntimeSeedsBuiltInDefaultsWithoutOverwritingCustomState(t *testing.T) {
	runtime := NewMemoryRuntime(&runtimeMailer{})
	if err := runtime.SeedBuiltInDefaults(); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Caller("system.admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Template("auth.email-change"); err != nil {
		t.Fatal(err)
	}
	custom, err := runtime.Template("security.password-changed")
	if err != nil {
		t.Fatal(err)
	}
	custom.Locales["en-US"] = TemplateLocale{Locale: "en-US", Subject: "custom", Body: "custom"}
	if err := runtime.SetTemplate(custom); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SeedBuiltInDefaults(); err != nil {
		t.Fatal(err)
	}
	updated, _ := runtime.Template("security.password-changed")
	if updated.Locales["en-US"].Subject != "custom" {
		t.Fatalf("custom template was overwritten: %+v", updated)
	}
}
