package notification

// This file contains the first executable implementation of the public
// notification ports.  The legacy Service above remains available for the
// password-reset compatibility path; Runtime is the reusable seam used by new
// callers and by the management-side test-send flow.  It deliberately keeps
// provider details behind Mailer and stores only digests for verification
// codes.

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

var (
	ErrCallerNotFound            = errors.New("caller not found")
	ErrCallerDisabled            = errors.New("caller disabled")
	ErrTemplateNotFound          = errors.New("template not found")
	ErrTemplateUnpublished       = errors.New("template is not published")
	ErrTemplateVariableMissing   = errors.New("template variable missing")
	ErrTemplateVariableInvalid   = errors.New("template variable invalid")
	ErrInvalidRecipient          = errors.New("invalid notification recipient")
	ErrIdempotencyConflict       = errors.New("notification idempotency conflict")
	ErrVerificationRateLimited   = errors.New("verification rate limited")
	ErrVerificationExpired       = errors.New("verification challenge expired")
	ErrVerificationLocked        = errors.New("verification challenge locked")
	ErrVerificationNotActive     = errors.New("verification challenge is not active")
	ErrVerificationConsumed      = errors.New("verification challenge consumed")
	ErrVerificationCodeIncorrect = errors.New("verification code incorrect")
	ErrVerificationNotFound      = errors.New("verification challenge not found")
	ErrPolicyNotFound            = errors.New("verification policy not found")
	ErrInvalidPolicy             = errors.New("invalid verification policy")
	ErrCallerSystemOwned         = errors.New("system caller cannot be deleted")
)

// Caller is the final-state record configured by the management UI.  The
// runtime uses Key as the stable integration identifier; display fields are
// never trusted from a request body.
type Caller struct {
	Key          string   `json:"callerKey"`
	Name         string   `json:"name"`
	Module       string   `json:"module,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Enabled      bool     `json:"enabled"`
	SystemOwned  bool     `json:"systemOwned"`
}

// TemplateLocale is one language variant of a template. Subject and Body use
// Go's text/template syntax and receive only the variables declared by the
// parent Template.
type TemplateLocale struct {
	Locale  string `json:"locale"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// Template is an immutable-at-send-time final-state template definition.
// Generation is an internal correlation value and is not a business version
// exposed to callers.
type Template struct {
	Key           string                    `json:"templateKey"`
	Purpose       string                    `json:"purpose"`
	DefaultLocale string                    `json:"defaultLocale"`
	Variables     []string                  `json:"variables"`
	Locales       map[string]TemplateLocale `json:"locales"`
	Enabled       bool                      `json:"enabled"`
	Published     bool                      `json:"published"`
	Generation    string                    `json:"generation,omitempty"`
}

// VerificationPolicy bounds the configurable code behavior. Zero values are
// filled with the documented defaults by normalizePolicy.
type VerificationPolicy struct {
	Key         string        `json:"policyKey"`
	CallerKey   string        `json:"callerKey,omitempty"`
	Purpose     string        `json:"purpose,omitempty"`
	Length      int           `json:"codeLength"`
	Charset     string        `json:"charset"`
	TTL         time.Duration `json:"-"`
	MaxFailures int           `json:"maxFailures"`
	ResendAfter time.Duration `json:"-"`
	HourlyLimit int           `json:"hourlyLimit"`
}

const (
	DefaultVerificationLength      = 6
	DefaultVerificationCharset     = "0123456789"
	DefaultVerificationTTL         = 10 * time.Minute
	DefaultVerificationMaxFailures = 5
	DefaultVerificationResendAfter = time.Minute
	DefaultVerificationHourlyLimit = 5
)

// RuntimeConfig wires the provider and deterministic seams. Mailer is
// intentionally the existing provider-neutral interface, so SMTP, a queue
// relay, or a test double can be selected by bootstrap without changing
// callers.
type RuntimeConfig struct {
	Mailer             Mailer
	Clock              func() time.Time
	Random             io.Reader
	CodeGenerator      func(length int, charset string) (string, error)
	HashKey            []byte
	DefaultLocale      string
	RequireTenant      bool
	StrictRegistration bool
}

type Runtime struct {
	mailer             Mailer
	clock              func() time.Time
	random             io.Reader
	codeGenerator      func(int, string) (string, error)
	hashKey            []byte
	defaultLocale      string
	requireTenant      bool
	strictRegistration bool

	mu          sync.RWMutex
	callers     map[string]Caller
	templates   map[string]Template
	policies    map[string]VerificationPolicy
	challenges  map[string]*runtimeChallenge
	idempotency map[string]runtimeIdempotency
	issueFlight map[string]*runtimeChallengeFlight
	issued      map[string][]time.Time
	generation  uint64
	// issueMu serializes challenge reservation for the same in-process runtime.
	// It is intentionally separate from mu so code generation and provider
	// calls never happen while the registry lock is held.
	issueMu    sync.Mutex
	sendIdemMu sync.Mutex
	verifyIdem map[string]runtimeVerificationIdempotency
}

type runtimeChallenge struct {
	ref         ChallengeRef
	scopeKey    string
	caller      string
	purpose     string
	recipient   string
	codeDigest  []byte
	failures    int
	policy      VerificationPolicy
	idempotency string
	payloadHash string
}

type runtimeIdempotency struct {
	payloadHash string
	result      SendResult
	challenge   ChallengeRef
	// err is retained for challenge idempotency so a retry observes the same
	// provider outcome instead of turning a failed send into a false success.
	err error
}

// runtimeChallengeFlight closes the window between reserving a challenge and
// persisting its final send result. Requests that reuse the same idempotency
// key wait on this flight instead of creating a second challenge.
type runtimeChallengeFlight struct {
	payloadHash string
	done        chan struct{}
	result      ChallengeRef
	err         error
}

type runtimeVerificationIdempotency struct {
	payloadHash string
	// outcome is "ok" or one of the stable verification error names. Only the
	// digest and outcome are retained; plaintext codes are never stored.
	outcome string
}

// NewRuntime returns a reusable in-process implementation. It is safe for
// concurrent requests and can be hot-updated through the Set*/Delete* methods.
func NewRuntime(cfg RuntimeConfig) *Runtime {
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	reader := cfg.Random
	if reader == nil {
		reader = rand.Reader
	}
	key := append([]byte(nil), cfg.HashKey...)
	if len(key) == 0 {
		// A process-local key is preferable to ever retaining a plaintext code;
		// bootstrap should provide a persisted secret in production.
		key = make([]byte, 32)
		_, _ = io.ReadFull(reader, key)
	}
	locale := normalizeLocale(cfg.DefaultLocale)
	if locale == "" {
		locale = "zh-CN"
	}
	r := &Runtime{
		mailer:             cfg.Mailer,
		clock:              clock,
		random:             reader,
		hashKey:            key,
		defaultLocale:      locale,
		requireTenant:      cfg.RequireTenant,
		strictRegistration: cfg.StrictRegistration,
		callers:            make(map[string]Caller),
		templates:          make(map[string]Template),
		policies:           make(map[string]VerificationPolicy),
		challenges:         make(map[string]*runtimeChallenge),
		idempotency:        make(map[string]runtimeIdempotency),
		issueFlight:        make(map[string]*runtimeChallengeFlight),
		issued:             make(map[string][]time.Time),
		verifyIdem:         make(map[string]runtimeVerificationIdempotency),
	}
	if cfg.CodeGenerator != nil {
		r.codeGenerator = cfg.CodeGenerator
	}
	return r
}

// NewMemoryRuntime is a convenience constructor for local fixtures. It has
// no network provider; callers can still register templates and exercise
// validation/verification behavior with a recording Mailer.
func NewMemoryRuntime(mailer Mailer) *Runtime {
	return NewRuntime(RuntimeConfig{Mailer: mailer})
}

// SeedBuiltInDefaults installs the system caller, common notification
// templates and verification policies used by a fresh management instance.
// Existing final state is preserved, so operators can call it on every
// startup/reconcile without overwriting UI changes.
func (r *Runtime) SeedBuiltInDefaults() error {
	if r == nil {
		return ErrProvider
	}
	defaultCallers := []Caller{
		{Key: "system.admin", Name: "System administration", Module: "system", Enabled: true, SystemOwned: true},
		{Key: "security.password-changed", Name: "Password changed notification", Module: "security", Enabled: true, SystemOwned: true},
		{Key: "security.unusual-login", Name: "Unusual login notification", Module: "security", Enabled: true, SystemOwned: true},
		{Key: "auth.email-change", Name: "Email change verification", Module: "auth", Enabled: true, SystemOwned: true},
		{Key: "auth.password-reset", Name: "Password reset verification", Module: "auth", Enabled: true, SystemOwned: true},
	}
	for _, caller := range defaultCallers {
		if _, err := r.Caller(caller.Key); errors.Is(err, ErrCallerNotFound) {
			if err := r.SetCaller(caller); err != nil {
				return err
			}
		}
	}
	defaultTemplates := []Template{
		{Key: "security.password-changed", Purpose: "security.password-changed", DefaultLocale: "zh-CN", Variables: []string{"display_name"}, Enabled: true, Published: true, Locales: map[string]TemplateLocale{
			"zh-CN": {Locale: "zh-CN", Subject: "密码已修改", Body: "你好 {{.display_name}}，你的密码已成功修改。"},
			"en-US": {Locale: "en-US", Subject: "Password changed", Body: "Hello {{.display_name}}, your password was changed successfully."},
		}},
		{Key: "security.unusual-login", Purpose: "security.unusual-login", DefaultLocale: "zh-CN", Variables: []string{"display_name", "location"}, Enabled: true, Published: true, Locales: map[string]TemplateLocale{
			"zh-CN": {Locale: "zh-CN", Subject: "检测到异地登录", Body: "你好 {{.display_name}}，检测到来自 {{.location}} 的登录。"},
			"en-US": {Locale: "en-US", Subject: "Unusual login detected", Body: "Hello {{.display_name}}, a login from {{.location}} was detected."},
		}},
		{Key: "auth.email-change", Purpose: "email_change", DefaultLocale: "zh-CN", Variables: []string{"code", "expires_in"}, Enabled: true, Published: true, Locales: map[string]TemplateLocale{
			"zh-CN": {Locale: "zh-CN", Subject: "邮箱验证码", Body: "验证码 {{.code}}，{{.expires_in}} 内有效。"},
			"en-US": {Locale: "en-US", Subject: "Email verification code", Body: "Your code is {{.code}} and expires in {{.expires_in}}."},
		}},
		{Key: "auth.password-reset", Purpose: "password_reset", DefaultLocale: "zh-CN", Variables: []string{"code", "expires_in"}, Enabled: true, Published: true, Locales: map[string]TemplateLocale{
			"zh-CN": {Locale: "zh-CN", Subject: "重置密码验证码", Body: "验证码 {{.code}}，{{.expires_in}} 内有效。"},
			"en-US": {Locale: "en-US", Subject: "Password reset code", Body: "Your code is {{.code}} and expires in {{.expires_in}}."},
		}},
	}
	for _, tmpl := range defaultTemplates {
		if _, err := r.Template(tmpl.Key); errors.Is(err, ErrTemplateNotFound) {
			if err := r.SetTemplate(tmpl); err != nil {
				return err
			}
		}
	}
	for _, policy := range []VerificationPolicy{
		{Key: "auth.email-change", Purpose: "email_change", CallerKey: "auth.email-change"},
		{Key: "auth.password-reset", Purpose: "password_reset", CallerKey: "auth.password-reset"},
	} {
		found := false
		for _, candidate := range r.ListVerificationPolicies() {
			if candidate.CallerKey == policy.CallerKey && candidate.Purpose == policy.Purpose {
				found = true
				break
			}
		}
		if !found {
			if err := r.SetVerificationPolicy(policy); err != nil {
				return err
			}
		}
	}
	return nil
}

// SetMailer attaches the transport after bootstrap has constructed the
// runtime/template registry. It is safe before requests begin.
func (r *Runtime) SetMailer(mailer Mailer) {
	if r != nil {
		r.mu.Lock()
		r.mailer = mailer
		r.mu.Unlock()
	}
}

// SetCaller atomically replaces a caller's final state and bumps an internal
// generation. The returned copy is detached from the runtime maps.
func (r *Runtime) SetCaller(caller Caller) error {
	return r.SetCallerFor(context.Background(), caller)
}

// SetCallerFor stores a caller in the scope represented by ctx. A caller
// registered without a tenant context is a system default; tenant and
// organization registrations override that default for their own scope.
func (r *Runtime) SetCallerFor(ctx context.Context, caller Caller) error {
	if r == nil {
		return ErrCallerNotFound
	}
	caller.Key = strings.TrimSpace(caller.Key)
	caller.Name = strings.TrimSpace(caller.Name)
	caller.Module = strings.TrimSpace(caller.Module)
	if caller.Name == "" {
		caller.Name = caller.Key
	}
	if !validStableKey(caller.Key) || caller.Name == "" || len(caller.Name) > 191 {
		return ErrCallerNotFound
	}
	if strings.ContainsAny(caller.Name+caller.Module, "\r\n") {
		return ErrCallerNotFound
	}
	caller.Capabilities = append([]string(nil), caller.Capabilities...)
	if len(caller.Capabilities) > 64 {
		return ErrCallerNotFound
	}
	scopeKey := runtimeScopeKey(ctx)
	r.mu.Lock()
	key := registryKey(scopeKey, caller.Key)
	if previous, exists := r.callers[key]; exists && previous.SystemOwned {
		caller.SystemOwned = true
	}
	r.callers[key] = caller
	r.generation++
	r.mu.Unlock()
	return nil
}

func (r *Runtime) ListCallers() []Caller {
	return r.ListCallersFor(context.Background())
}

func (r *Runtime) ListCallersFor(ctx context.Context) []Caller {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	allScopes := make(map[string]struct{}, len(r.callers))
	for key := range r.callers {
		if index := strings.IndexByte(key, '\x00'); index >= 0 {
			allScopes[key[:index]] = struct{}{}
		}
	}
	scopes := registryScopes(ctx, allScopes)
	seen := make(map[string]struct{})
	out := make([]Caller, 0, len(r.callers))
	for _, scope := range scopes {
		for key, value := range r.callers {
			if key != registryKey(scope, value.Key) {
				continue
			}
			if _, exists := seen[value.Key]; exists {
				continue
			}
			value.Capabilities = append([]string(nil), value.Capabilities...)
			out = append(out, value)
			seen[value.Key] = struct{}{}
		}
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (r *Runtime) Caller(key string) (Caller, error) {
	return r.CallerFor(context.Background(), key)
}

func (r *Runtime) CallerFor(ctx context.Context, key string) (Caller, error) {
	if r == nil {
		return Caller{}, ErrCallerNotFound
	}
	r.mu.RLock()
	trimmed := strings.TrimSpace(key)
	var v Caller
	ok := false
	allScopes := make(map[string]struct{}, len(r.callers))
	for mapKey := range r.callers {
		if index := strings.IndexByte(mapKey, '\x00'); index >= 0 {
			allScopes[mapKey[:index]] = struct{}{}
		}
	}
	for _, scope := range registryScopes(ctx, allScopes) {
		if candidate, exists := r.callers[registryKey(scope, trimmed)]; exists {
			v, ok = candidate, true
			break
		}
	}
	r.mu.RUnlock()
	if !ok {
		return Caller{}, ErrCallerNotFound
	}
	v.Capabilities = append([]string(nil), v.Capabilities...)
	return v, nil
}

func (r *Runtime) DeleteCaller(key string) error {
	return r.DeleteCallerFor(context.Background(), key)
}

func (r *Runtime) DeleteCallerFor(ctx context.Context, key string) error {
	if r == nil {
		return ErrCallerNotFound
	}
	key = strings.TrimSpace(key)
	scopeKey := runtimeScopeKey(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	registry := registryKey(scopeKey, key)
	caller, ok := r.callers[registry]
	if !ok {
		return ErrCallerNotFound
	}
	if caller.SystemOwned {
		return ErrCallerSystemOwned
	}
	delete(r.callers, registry)
	r.generation++
	return nil
}

func (r *Runtime) SetTemplate(value Template) error {
	return r.SetTemplateFor(context.Background(), value)
}

func (r *Runtime) SetTemplateFor(ctx context.Context, value Template) error {
	if r == nil {
		return ErrTemplateNotFound
	}
	value.Key = strings.TrimSpace(value.Key)
	value.Purpose = strings.TrimSpace(value.Purpose)
	if !validStableKey(value.Key) {
		return ErrTemplateNotFound
	}
	if value.Purpose == "" {
		value.Purpose = value.Key
	}
	value.DefaultLocale = normalizeLocale(value.DefaultLocale)
	value.Variables = normalizeVariables(value.Variables)
	value.Locales = cloneLocales(value.Locales)
	if len(value.Locales) == 0 {
		return ErrTemplateUnpublished
	}
	if value.DefaultLocale == "" {
		value.DefaultLocale = r.defaultLocale
	}
	if value.Generation == "" {
		value.Generation = r.nextGeneration()
	}
	scopeKey := runtimeScopeKey(ctx)
	r.mu.Lock()
	r.templates[registryKey(scopeKey, value.Key)] = value
	r.mu.Unlock()
	return nil
}

func (r *Runtime) ListTemplates() []Template {
	return r.ListTemplatesFor(context.Background())
}

func (r *Runtime) ListTemplatesFor(ctx context.Context) []Template {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	allScopes := registryScopeSet(r.templates)
	scopes := registryScopes(ctx, allScopes)
	seen := make(map[string]struct{})
	out := make([]Template, 0, len(r.templates))
	for _, scope := range scopes {
		for key, value := range r.templates {
			if key != registryKey(scope, value.Key) {
				continue
			}
			if _, exists := seen[value.Key]; exists {
				continue
			}
			value.Variables = append([]string(nil), value.Variables...)
			value.Locales = cloneLocales(value.Locales)
			out = append(out, value)
			seen[value.Key] = struct{}{}
		}
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (r *Runtime) Template(key string) (Template, error) {
	return r.TemplateFor(context.Background(), key)
}

func (r *Runtime) TemplateFor(ctx context.Context, key string) (Template, error) {
	if r == nil {
		return Template{}, ErrTemplateNotFound
	}
	r.mu.RLock()
	trimmed := strings.TrimSpace(key)
	v := Template{}
	ok := false
	for _, scope := range registryScopes(ctx, registryScopeSet(r.templates)) {
		if candidate, exists := r.templates[registryKey(scope, trimmed)]; exists {
			v, ok = candidate, true
			break
		}
	}
	r.mu.RUnlock()
	if !ok {
		return Template{}, ErrTemplateNotFound
	}
	v.Variables = append([]string(nil), v.Variables...)
	v.Locales = cloneLocales(v.Locales)
	return v, nil
}

func (r *Runtime) ListVerificationPolicies() []VerificationPolicy {
	return r.ListVerificationPoliciesFor(context.Background())
}

func (r *Runtime) ListVerificationPoliciesFor(ctx context.Context) []VerificationPolicy {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	unique := make(map[string]VerificationPolicy, len(r.policies))
	for _, scope := range registryScopes(ctx, registryScopeSet(r.policies)) {
		for mapKey, v := range r.policies {
			if !strings.HasPrefix(mapKey, strings.TrimSpace(scope)+"\x00") {
				continue
			}
			identity := policyLookupKey(v.CallerKey, v.Purpose)
			if strings.TrimSpace(v.Purpose) == "" {
				identity = policyLookupKey(v.CallerKey, v.Key)
			}
			if _, exists := unique[identity]; exists {
				continue
			}
			unique[identity] = v
		}
	}
	r.mu.RUnlock()
	out := make([]VerificationPolicy, 0, len(unique))
	for _, v := range unique {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// VerificationPolicyFor resolves one policy by its stable management key or
// purpose in the caller's visible scope chain. It is intentionally read-only
// and returns a detached value so PATCH handlers can merge omitted fields
// without exposing the runtime map.
func (r *Runtime) VerificationPolicyFor(ctx context.Context, key string) (VerificationPolicy, error) {
	if r == nil {
		return VerificationPolicy{}, ErrPolicyNotFound
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return VerificationPolicy{}, ErrPolicyNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, scope := range registryScopes(ctx, registryScopeSet(r.policies)) {
		prefix := strings.TrimSpace(scope) + "\x00"
		keys := make([]string, 0)
		for mapKey, candidate := range r.policies {
			if !strings.HasPrefix(mapKey, prefix) {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(candidate.Key), key) || strings.EqualFold(strings.TrimSpace(candidate.Purpose), key) {
				keys = append(keys, mapKey)
			}
		}
		if len(keys) == 0 {
			continue
		}
		sort.Strings(keys)
		value := r.policies[keys[0]]
		return value, nil
	}
	return VerificationPolicy{}, ErrPolicyNotFound
}

func (r *Runtime) DeleteTemplate(key string) error {
	return r.DeleteTemplateFor(context.Background(), key)
}

func (r *Runtime) DeleteTemplateFor(ctx context.Context, key string) error {
	if r == nil {
		return ErrTemplateNotFound
	}
	key = strings.TrimSpace(key)
	registry := registryKey(runtimeScopeKey(ctx), key)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.templates[registry]; !ok {
		return ErrTemplateNotFound
	}
	delete(r.templates, registry)
	r.generation++
	return nil
}

func (r *Runtime) SetVerificationPolicy(policy VerificationPolicy) error {
	return r.SetVerificationPolicyFor(context.Background(), policy)
}

func (r *Runtime) SetVerificationPolicyFor(ctx context.Context, policy VerificationPolicy) error {
	if r == nil {
		return ErrPolicyNotFound
	}
	originalKey := strings.TrimSpace(policy.Key)
	policy = normalizePolicy(policy)
	if originalKey == "" {
		return ErrPolicyNotFound
	}
	if !validStableKey(originalKey) || policy.Key == "" {
		return ErrInvalidPolicy
	}
	if policy.Purpose == "" {
		policy.Purpose = policy.Key
	}
	scopeKey := runtimeScopeKey(ctx)
	r.mu.Lock()
	// A policy is addressed by one stable management key, while purpose and
	// caller are lookup aliases. Remove aliases from the previous final state
	// before writing the new ones; otherwise changing purpose/caller would
	// leave an unreachable-looking stale policy active under its old alias.
	prefix := registryKey(scopeKey, "")
	for mapKey, existing := range r.policies {
		if strings.HasPrefix(mapKey, prefix) && existing.Key == policy.Key {
			delete(r.policies, mapKey)
		}
	}
	r.policies[registryKey(scopeKey, policyLookupKey(policy.CallerKey, policy.Purpose))] = policy
	// Retain a key alias so management clients can address a policy by its
	// stable policy_key even when its purpose is a separate label.
	r.policies[registryKey(scopeKey, policyLookupKey(policy.CallerKey, policy.Key))] = policy
	r.generation++
	r.mu.Unlock()
	return nil
}

// Generation returns the internal runtime snapshot generation for diagnostics
// and cache invalidation. It is not required in a business request.
func (r *Runtime) Generation() string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	value := r.generation
	r.mu.RUnlock()
	return fmt.Sprintf("g-%d", value)
}

// Send implements NotificationService. The notification purpose is used as
// the template key unless a template was registered under a separate key
// with the same purpose; this keeps notification call sites free of template
// storage details.
func (r *Runtime) Send(ctx context.Context, request NotificationRequest) (SendResult, error) {
	if r == nil {
		return SendResult{}, ErrProvider
	}
	if err := r.checkScope(ctx); err != nil {
		return SendResult{}, err
	}
	callerKey, err := effectiveCaller(ctx, request.CallerKey)
	if err != nil {
		return SendResult{}, err
	}
	if err := r.checkCallerFor(ctx, callerKey); err != nil {
		return SendResult{}, err
	}
	recipients, err := normalizeNotificationRecipients(request.Recipients)
	if err != nil {
		return SendResult{}, err
	}
	templateKey := strings.TrimSpace(request.Purpose)
	if templateKey == "" {
		return SendResult{}, ErrTemplateNotFound
	}
	tmpl, err := r.resolveTemplateFor(ctx, templateKey, request.Locale)
	if err != nil {
		return SendResult{}, err
	}
	locale := effectiveLocale(ctx, request.Locale, r.defaultLocale)
	subject, body, err := renderTemplate(tmpl, locale, request.Variables, r.defaultLocale)
	if err != nil {
		return SendResult{}, err
	}
	payloadHash := notificationPayloadHash(callerKey, templateKey, recipients, request.Variables, locale, request.Mode)
	// Include the rendered snapshot in the idempotency fingerprint. A template
	// edit must not make a retry with the same client key silently return the
	// result of a different subject/body generation.
	payloadHash = notificationRenderedPayloadHash(payloadHash, tmpl.Generation, subject, body)
	policyGeneration := r.Generation()
	idempotencyKey := strings.TrimSpace(request.IdempotencyKey)
	scopeKey := ""
	if scope, ok := tenant.FromContext(ctx); ok {
		scopeKey = scope.TenantID + ":" + scope.Organization
	}
	idemStoreKey := ""
	if idempotencyKey != "" {
		idemStoreKey = scopeKey + ":" + callerKey + ":" + idempotencyKey
		// Serialize only keyed sends, outside the runtime state lock. Holding
		// the state lock while invoking a provider can deadlock a provider that
		// records diagnostics or hot-reloads a template.
		r.sendIdemMu.Lock()
		defer r.sendIdemMu.Unlock()
		r.mu.Lock()
		previous, exists := r.idempotency[idemStoreKey]
		if exists {
			r.mu.Unlock()
			if previous.payloadHash != payloadHash {
				return SendResult{}, ErrIdempotencyConflict
			}
			if previous.result.Status == DeliveryFailed {
				return previous.result, ErrProvider
			}
			return previous.result, nil
		}
		r.mu.Unlock()
	}
	id, err := newRuntimeID(r.random)
	if err != nil {
		return SendResult{}, fmt.Errorf("create notification id: %w", err)
	}
	message := Message{
		ID:                 id,
		To:                 recipients[0].Address,
		Subject:            subject,
		Body:               body,
		Recipients:         recipientAddresses(recipients),
		CallerKey:          callerKey,
		TemplateKey:        templateKey,
		TemplateGeneration: tmpl.Generation,
		PolicyGeneration:   policyGeneration,
		Locale:             locale,
		Mode:               request.Mode,
		IsTest:             request.Mode == SendModeAdminTest,
		ChallengeID:        strings.TrimSpace(request.ChallengeID),
		IdempotencyKey:     idempotencyKey,
		Status:             StatusPending,
		CreatedAt:          r.clock().UTC(),
	}
	result := SendResult{MessageID: id, Status: DeliveryQueued, PolicyGeneration: policyGeneration, TemplateGeneration: tmpl.Generation, IsTest: request.Mode == SendModeAdminTest}
	if request.Mode == SendModeAdminTest || request.Mode == SendModeProduction || request.Mode == "" {
		mailer := r.mailerSnapshot()
		if mailer == nil {
			result.Status = DeliveryFailed
			if idempotencyKey != "" {
				r.mu.Lock()
				r.idempotency[idemStoreKey] = runtimeIdempotency{payloadHash: payloadHash, result: result}
				r.mu.Unlock()
			}
			return result, ErrProvider
		}
		if err := mailer.Send(ctx, message); err != nil {
			result.Status = DeliveryFailed
			if idempotencyKey != "" {
				r.mu.Lock()
				r.idempotency[idemStoreKey] = runtimeIdempotency{payloadHash: payloadHash, result: result}
				r.mu.Unlock()
			}
			return result, errors.Join(ErrProvider, err)
		}
		result.Status = DeliverySent
	} else {
		return SendResult{}, ErrInvalidMessage
	}
	if idempotencyKey != "" {
		r.mu.Lock()
		r.idempotency[idemStoreKey] = runtimeIdempotency{payloadHash: payloadHash, result: result}
		r.mu.Unlock()
	}
	return result, nil
}

func (r *Runtime) mailerSnapshot() Mailer {
	r.mu.RLock()
	mailer := r.mailer
	r.mu.RUnlock()
	return mailer
}

// Render exposes the provider-neutral template snapshot to the mail adapter
// without exposing mutable template storage or SMTP details. It performs the
// same locale fallback and variable allow-list checks as Send.
func (r *Runtime) Render(ctx context.Context, key, locale string, variables map[string]string) (string, string, string, error) {
	if r == nil {
		return "", "", "", ErrTemplateNotFound
	}
	tmpl, err := r.resolveTemplateFor(ctx, strings.TrimSpace(key), locale)
	if err != nil {
		return "", "", "", err
	}
	subject, body, err := renderTemplate(tmpl, effectiveLocale(ctx, locale, r.defaultLocale), variables, r.defaultLocale)
	if err != nil {
		return "", "", "", err
	}
	return subject, body, tmpl.Generation, nil
}

// Issue implements VerificationCodeService. It reserves the challenge before
// sending, so concurrent requests cannot create two current codes for the
// same recipient/purpose. A failed provider transitions the challenge to
// send_failed and leaves only the digest in memory.
func (r *Runtime) Issue(ctx context.Context, request IssueRequest) (ChallengeRef, error) {
	if r == nil {
		return ChallengeRef{}, ErrProvider
	}
	if err := r.checkScope(ctx); err != nil {
		return ChallengeRef{}, err
	}
	purpose := strings.TrimSpace(request.Purpose)
	if purpose == "" {
		return ChallengeRef{}, ErrPolicyNotFound
	}
	callerKey, err := effectiveCaller(ctx, request.CallerKey)
	if err != nil {
		return ChallengeRef{}, err
	}
	if err := r.checkCallerFor(ctx, callerKey); err != nil {
		return ChallengeRef{}, err
	}
	recipient, err := normalizeRecipient(request.Recipient)
	if err != nil {
		return ChallengeRef{}, err
	}
	policy, err := r.resolvePolicy(ctx, callerKey, purpose)
	if err != nil {
		return ChallengeRef{}, err
	}
	scopeKey := ""
	if scope, ok := tenant.FromContext(ctx); ok {
		scopeKey = scope.TenantID + ":" + scope.Organization
	}
	idempotencyKey := strings.TrimSpace(request.IdempotencyKey)
	locale := effectiveLocale(ctx, request.Locale, r.defaultLocale)
	payloadHash := notificationPayloadHash(callerKey, purpose, []Recipient{{Address: recipient, Kind: "to"}}, request.Variables, locale, SendModeProduction)
	r.issueMu.Lock()
	challengeIdemKey := ""
	if idempotencyKey != "" {
		challengeIdemKey = scopeKey + ":challenge:" + callerKey + ":" + idempotencyKey
		r.mu.RLock()
		previous, exists := r.idempotency[challengeIdemKey]
		flight := r.issueFlight[challengeIdemKey]
		r.mu.RUnlock()
		if exists {
			if previous.payloadHash != payloadHash {
				r.issueMu.Unlock()
				return ChallengeRef{}, ErrIdempotencyConflict
			}
			r.issueMu.Unlock()
			return previous.challenge, previous.err
		}
		if flight != nil {
			if flight.payloadHash != payloadHash {
				r.issueMu.Unlock()
				return ChallengeRef{}, ErrIdempotencyConflict
			}
			// Do not retain the reservation mutex while waiting for the
			// provider. Other recipients can reserve independently.
			r.issueMu.Unlock()
			<-flight.done
			return flight.result, flight.err
		}
	}
	// Generate after the idempotency/flight check. A retry that joins an
	// in-flight challenge must not invoke a custom generator a second time.
	// The registry lock is not held while application callbacks execute.
	code, genErr := r.generateCode(policy)
	if genErr != nil {
		r.issueMu.Unlock()
		return ChallengeRef{}, genErr
	}
	now := r.clock().UTC()
	limitKey := scopeKey + ":" + callerKey + ":" + purpose + ":" + recipient
	r.mu.Lock()
	issued := r.issued[limitKey]
	cutoff := now.Add(-time.Hour)
	kept := issued[:0]
	for _, timestamp := range issued {
		if timestamp.After(cutoff) {
			kept = append(kept, timestamp)
		}
	}
	if policy.HourlyLimit > 0 && len(kept) >= policy.HourlyLimit {
		r.issued[limitKey] = kept
		r.mu.Unlock()
		r.issueMu.Unlock()
		return ChallengeRef{}, ErrVerificationRateLimited
	}
	if len(kept) > 0 && now.Before(kept[len(kept)-1].Add(policy.ResendAfter)) {
		r.issued[limitKey] = kept
		r.mu.Unlock()
		r.issueMu.Unlock()
		return ChallengeRef{}, ErrVerificationRateLimited
	}
	// Supersede every prior current challenge for this tuple.
	for _, challenge := range r.challenges {
		if challenge.scopeKey == scopeKey && challenge.caller == callerKey && challenge.purpose == purpose && challenge.recipient == recipient {
			switch challenge.ref.Status {
			case "active", "pending_send":
				challenge.ref.Status = "superseded"
			}
		}
	}
	id, idErr := newRuntimeID(r.random)
	if idErr != nil {
		r.mu.Unlock()
		r.issueMu.Unlock()
		return ChallengeRef{}, idErr
	}
	ref := ChallengeRef{ID: id, ExpiresAt: now.Add(policy.TTL), Status: "pending_send"}
	challenge := &runtimeChallenge{ref: ref, scopeKey: scopeKey, caller: callerKey, purpose: purpose, recipient: recipient, codeDigest: r.digest(code), policy: policy, idempotency: idempotencyKey, payloadHash: payloadHash}
	r.challenges[id] = challenge
	if challengeIdemKey != "" {
		r.issueFlight[challengeIdemKey] = &runtimeChallengeFlight{payloadHash: payloadHash, done: make(chan struct{})}
	}
	r.issued[limitKey] = append(kept, now)
	r.mu.Unlock()
	// The challenge reservation is complete. Provider delivery happens outside
	// issueMu and mu so a slow SMTP provider cannot block unrelated issuance.
	r.issueMu.Unlock()

	vars := cloneStringMap(request.Variables)
	vars["code"] = code
	vars["expires_in"] = formatTTL(policy.TTL)
	result, sendErr := r.Send(ctx, NotificationRequest{CallerKey: callerKey, Purpose: purpose, Recipients: []Recipient{{Address: recipient, Kind: "to"}}, Variables: vars, Locale: locale, IdempotencyKey: "challenge:" + id, Mode: SendModeProduction, ChallengeID: id})
	r.mu.Lock()
	if current, ok := r.challenges[id]; ok {
		// A later issuance may supersede this reservation while the provider
		// call is in flight. Never let the slow completion resurrect a
		// superseded challenge; the newest reservation remains authoritative.
		if current.ref.Status == "pending_send" {
			if sendErr != nil {
				current.ref.Status = "send_failed"
			} else {
				current.ref.Status = "active"
			}
		}
	}
	finalRef := r.challenges[id].ref
	if challengeIdemKey != "" {
		// Publish the final state before waking requests that joined this
		// idempotent issuance while the provider was in flight.
		r.idempotency[challengeIdemKey] = runtimeIdempotency{payloadHash: payloadHash, challenge: finalRef, err: sendErr}
		if flight := r.issueFlight[challengeIdemKey]; flight != nil {
			flight.result = finalRef
			flight.err = sendErr
			close(flight.done)
			delete(r.issueFlight, challengeIdemKey)
		}
	}
	r.mu.Unlock()
	if sendErr != nil {
		return finalRef, sendErr
	}
	_ = result
	return finalRef, nil
}

func (r *Runtime) Verify(ctx context.Context, request VerifyRequest) error {
	if r == nil {
		return ErrProvider
	}
	if err := r.checkScope(ctx); err != nil {
		return err
	}
	id := strings.TrimSpace(request.ChallengeID)
	if id == "" {
		return ErrVerificationNotFound
	}
	code := strings.TrimSpace(request.Code)
	idempotencyKey := strings.TrimSpace(request.IdempotencyKey)
	scopeKey := runtimeScopeKey(ctx)
	verifyKey, payloadHash := "", ""
	if idempotencyKey != "" {
		verifyKey = scopeKey + ":verify:" + id + ":" + idempotencyKey
		payloadHash = hex.EncodeToString(r.digest(code))
		r.mu.RLock()
		previous, exists := r.verifyIdem[verifyKey]
		r.mu.RUnlock()
		if exists {
			if previous.payloadHash != payloadHash {
				return ErrIdempotencyConflict
			}
			return verificationOutcomeError(previous.outcome)
		}
	}
	now := r.clock().UTC()
	r.mu.Lock()
	// A keyed retry can arrive between the optimistic read above and this
	// state lock. Re-check under the lock so concurrent retries replay the
	// original outcome instead of incrementing failures twice or turning a
	// successful verification into a consumed error.
	if verifyKey != "" {
		if previous, exists := r.verifyIdem[verifyKey]; exists {
			if previous.payloadHash != payloadHash {
				r.mu.Unlock()
				return ErrIdempotencyConflict
			}
			err := verificationOutcomeError(previous.outcome)
			r.mu.Unlock()
			return err
		}
	}
	challenge, ok := r.challenges[id]
	if !ok {
		r.mu.Unlock()
		return ErrVerificationNotFound
	}
	if !challengeScopeVisible(ctx, challenge.scopeKey) {
		r.mu.Unlock()
		return tenant.ErrCrossTenant
	}
	remember := func(outcome string) error {
		if verifyKey != "" {
			r.verifyIdem[verifyKey] = runtimeVerificationIdempotency{payloadHash: payloadHash, outcome: outcome}
		}
		return verificationOutcomeError(outcome)
	}
	if !now.Before(challenge.ref.ExpiresAt) {
		challenge.ref.Status = "expired"
		err := remember("expired")
		r.mu.Unlock()
		return err
	}
	switch challenge.ref.Status {
	case "consumed":
		err := remember("consumed")
		r.mu.Unlock()
		return err
	case "expired":
		err := remember("expired")
		r.mu.Unlock()
		return err
	case "superseded", "send_failed":
		err := remember("not_active")
		r.mu.Unlock()
		return err
	case "pending_send":
		err := remember("not_active")
		r.mu.Unlock()
		return err
	case "active":
	default:
		err := remember("not_active")
		r.mu.Unlock()
		return err
	}
	digest := r.digest(code)
	if !hmac.Equal(digest, challenge.codeDigest) {
		challenge.failures++
		if challenge.failures >= challenge.policy.MaxFailures {
			challenge.ref.Status = "locked"
			err := remember("locked")
			r.mu.Unlock()
			return err
		}
		err := remember("incorrect")
		r.mu.Unlock()
		return err
	}
	challenge.ref.Status = "consumed"
	if verifyKey != "" {
		r.verifyIdem[verifyKey] = runtimeVerificationIdempotency{payloadHash: payloadHash, outcome: "ok"}
	}
	r.mu.Unlock()
	return nil
}

// ChallengeStatus is useful to management diagnostics and tests while keeping
// code digests and plaintext codes private.
func (r *Runtime) ChallengeStatus(id string) (ChallengeRef, error) {
	if r == nil {
		return ChallengeRef{}, ErrVerificationNotFound
	}
	now := r.clock().UTC()
	r.mu.Lock()
	challenge, ok := r.challenges[strings.TrimSpace(id)]
	if ok {
		if !now.Before(challenge.ref.ExpiresAt) && (challenge.ref.Status == "active" || challenge.ref.Status == "pending_send") {
			challenge.ref.Status = "expired"
		}
		ref := challenge.ref
		r.mu.Unlock()
		return ref, nil
	}
	r.mu.Unlock()
	return ChallengeRef{}, ErrVerificationNotFound
}

// ChallengeStatusFor applies the same tenant boundary as Verify before
// returning diagnostic state to a caller.
func (r *Runtime) ChallengeStatusFor(ctx context.Context, id string) (ChallengeRef, error) {
	if r == nil {
		return ChallengeRef{}, ErrVerificationNotFound
	}
	if err := r.checkScope(ctx); err != nil {
		return ChallengeRef{}, err
	}
	now := r.clock().UTC()
	r.mu.Lock()
	challenge, ok := r.challenges[strings.TrimSpace(id)]
	if !ok {
		r.mu.Unlock()
		return ChallengeRef{}, ErrVerificationNotFound
	}
	if !challengeScopeVisible(ctx, challenge.scopeKey) {
		r.mu.Unlock()
		return ChallengeRef{}, tenant.ErrCrossTenant
	}
	if !now.Before(challenge.ref.ExpiresAt) && (challenge.ref.Status == "active" || challenge.ref.Status == "pending_send") {
		challenge.ref.Status = "expired"
	}
	ref := challenge.ref
	r.mu.Unlock()
	return ref, nil
}

func (r *Runtime) checkScope(ctx context.Context) error {
	if !r.requireTenant {
		return nil
	}
	if _, err := tenant.RequireContext(ctx); err != nil {
		return err
	}
	return nil
}

func runtimeScopeKey(ctx context.Context) string {
	if scope, ok := tenant.FromContext(ctx); ok {
		return strings.TrimSpace(scope.TenantID) + ":" + strings.TrimSpace(scope.Organization)
	}
	return ""
}

// challengeScopeVisible follows the same nearest-to-farthest inheritance
// chain as callers, templates and policies. A tenant-scoped challenge is
// therefore verifiable by an organization within that tenant, while an
// organization-scoped challenge never becomes visible to the tenant-wide
// parent or to another tenant. Platform-admin visibility remains explicit.
func challengeScopeVisible(ctx context.Context, challengeScope string) bool {
	challengeScope = strings.TrimSpace(challengeScope)
	if scope, ok := tenant.FromContext(ctx); ok && scope.PlatformAdmin {
		return true
	}
	for _, visible := range registryScopes(ctx, map[string]struct{}{challengeScope: {}}) {
		if visible == challengeScope {
			return true
		}
	}
	return false
}

func registryKey(scope, key string) string {
	return strings.TrimSpace(scope) + "\x00" + strings.TrimSpace(key)
}

// registryScopes returns the nearest-to-farthest inheritance chain. A tenant
// organization sees its org overrides, then tenant overrides, then system
// defaults. A platform administrator additionally sees every explicitly
// stored scope, while the nearest scope remains first for deterministic
// conflict resolution.
func registryScopes(ctx context.Context, all map[string]struct{}) []string {
	if scope, ok := tenant.FromContext(ctx); ok {
		result := []string{strings.TrimSpace(scope.TenantID) + ":" + strings.TrimSpace(scope.Organization)}
		if strings.TrimSpace(scope.Organization) != "" {
			result = append(result, strings.TrimSpace(scope.TenantID)+":")
		}
		result = append(result, "")
		if scope.PlatformAdmin {
			baseLen := len(result)
			seen := make(map[string]struct{}, len(result))
			for _, value := range result {
				seen[value] = struct{}{}
			}
			for value := range all {
				if _, exists := seen[value]; !exists {
					result = append(result, value)
					seen[value] = struct{}{}
				}
			}
			if len(result) > baseLen {
				sort.Strings(result[baseLen:])
			}
		}
		return result
	}
	return []string{""}
}

func registryScopeSet[T any](values map[string]T) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for key := range values {
		if index := strings.IndexByte(key, '\x00'); index >= 0 {
			result[key[:index]] = struct{}{}
		}
	}
	return result
}

func verificationOutcomeError(outcome string) error {
	switch outcome {
	case "", "ok":
		return nil
	case "expired":
		return ErrVerificationExpired
	case "consumed":
		return ErrVerificationConsumed
	case "locked":
		return ErrVerificationLocked
	case "incorrect":
		return ErrVerificationCodeIncorrect
	case "not_active":
		return ErrVerificationNotActive
	default:
		return ErrVerificationNotActive
	}
}

func (r *Runtime) checkCaller(key string) error {
	return r.checkCallerFor(context.Background(), key)
}

func (r *Runtime) checkCallerFor(ctx context.Context, key string) error {
	if key == "" {
		return ErrCallerNotFound
	}
	r.mu.RLock()
	strict := r.strictRegistration
	var caller Caller
	exists := false
	for _, scope := range registryScopes(ctx, registryScopeSet(r.callers)) {
		if candidate, ok := r.callers[registryKey(scope, key)]; ok {
			caller, exists = candidate, true
			break
		}
	}
	r.mu.RUnlock()
	if !exists {
		if strict {
			return ErrCallerNotFound
		}
		return nil
	}
	if !caller.Enabled {
		return ErrCallerDisabled
	}
	return nil
}

// effectiveCaller treats context metadata as the authority. A request field
// is only a compatibility fallback for in-process callers that have not yet
// installed capability metadata; HTTP payloads therefore cannot switch the
// registered caller of an authenticated request.
func effectiveCaller(ctx context.Context, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	trusted := strings.TrimSpace(ContextMetadataFromContext(ctx).CallerKey)
	if trusted != "" {
		return trusted, nil
	}
	return requested, nil
}

func (r *Runtime) resolveTemplate(key, locale string) (Template, error) {
	return r.resolveTemplateFor(context.Background(), key, locale)
}

func (r *Runtime) resolveTemplateFor(ctx context.Context, key, locale string) (Template, error) {
	key = strings.TrimSpace(key)
	r.mu.RLock()
	var tmpl Template
	ok := false
	for _, scope := range registryScopes(ctx, registryScopeSet(r.templates)) {
		if candidate, exists := r.templates[registryKey(scope, key)]; exists {
			tmpl, ok = candidate, true
			break
		}
		// Notification callers address a purpose, while template storage may use
		// a separate stable key. Resolve an exact purpose deterministically within
		// the nearest visible scope.
		keys := make([]string, 0)
		prefix := strings.TrimSpace(scope) + "\x00"
		for candidateKey, candidate := range r.templates {
			if !strings.HasPrefix(candidateKey, prefix) {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(candidate.Purpose), key) {
				keys = append(keys, candidateKey)
			}
		}
		if len(keys) > 0 {
			sort.Strings(keys)
			tmpl, ok = r.templates[keys[0]]
			break
		}
	}
	r.mu.RUnlock()
	if !ok {
		return Template{}, ErrTemplateNotFound
	}
	if !tmpl.Enabled || !tmpl.Published {
		return Template{}, ErrTemplateUnpublished
	}
	tmpl.Variables = append([]string(nil), tmpl.Variables...)
	tmpl.Locales = cloneLocales(tmpl.Locales)
	return tmpl, nil
}

func (r *Runtime) resolvePolicy(ctx context.Context, callerKey, purpose string) (VerificationPolicy, error) {
	purpose = strings.TrimSpace(purpose)
	r.mu.RLock()
	var policy VerificationPolicy
	ok := false
	for _, scope := range registryScopes(ctx, registryScopeSet(r.policies)) {
		policy, ok = r.policies[registryKey(scope, policyLookupKey(callerKey, purpose))]
		if !ok {
			policy, ok = r.policies[registryKey(scope, policyLookupKey("", purpose))]
		}
		if !ok {
			prefix := strings.TrimSpace(scope) + "\x00"
			for mapKey, candidate := range r.policies {
				if !strings.HasPrefix(mapKey, prefix) {
					continue
				}
				if strings.EqualFold(candidate.Purpose, purpose) || strings.EqualFold(candidate.Key, purpose) {
					if candidate.CallerKey == "" || candidate.CallerKey == callerKey {
						policy, ok = candidate, true
						break
					}
				}
			}
		}
		if ok {
			break
		}
	}
	r.mu.RUnlock()
	if !ok {
		if r.strictRegistration {
			return VerificationPolicy{}, ErrPolicyNotFound
		}
		policy = normalizePolicy(VerificationPolicy{Key: purpose, CallerKey: callerKey})
	}
	if policy.Key == "" || policy.Length == 0 {
		return VerificationPolicy{}, ErrPolicyNotFound
	}
	return policy, nil
}

func (r *Runtime) generateCode(policy VerificationPolicy) (string, error) {
	if r.codeGenerator != nil {
		code, err := r.codeGenerator(policy.Length, policy.Charset)
		if err != nil {
			return "", err
		}
		return validateGeneratedCode(code, policy)
	}
	if policy.Length <= 0 || policy.Charset == "" {
		return "", ErrPolicyNotFound
	}
	bytes := make([]byte, policy.Length)
	if _, err := io.ReadFull(r.random, bytes); err != nil {
		return "", err
	}
	var builder strings.Builder
	for _, value := range bytes {
		builder.WriteByte(policy.Charset[int(value)%len(policy.Charset)])
	}
	return builder.String(), nil
}

func (r *Runtime) digest(value string) []byte {
	h := hmac.New(sha256.New, r.hashKey)
	_, _ = io.WriteString(h, value)
	return h.Sum(nil)
}

func (r *Runtime) nextGeneration() string {
	r.mu.Lock()
	r.generation++
	value := r.generation
	r.mu.Unlock()
	return fmt.Sprintf("g-%d", value)
}

func (r *Runtime) rememberIdempotency(scope, caller, key, hash string, result SendResult, challenge ChallengeRef) {
	r.mu.Lock()
	r.idempotency[scope+":"+caller+":"+key] = runtimeIdempotency{payloadHash: hash, result: result, challenge: challenge}
	r.mu.Unlock()
}

func (r *Runtime) rememberChallengeIdempotency(scope, caller, key, hash string, challenge ChallengeRef) {
	r.mu.Lock()
	r.idempotency[scope+":challenge:"+caller+":"+key] = runtimeIdempotency{payloadHash: hash, challenge: challenge}
	r.mu.Unlock()
}

func normalizePolicy(policy VerificationPolicy) VerificationPolicy {
	policy.Key = strings.TrimSpace(policy.Key)
	policy.CallerKey = strings.TrimSpace(policy.CallerKey)
	policy.Purpose = strings.TrimSpace(policy.Purpose)
	if policy.Purpose == "" {
		policy.Purpose = policy.Key
	}
	if policy.Length == 0 {
		policy.Length = DefaultVerificationLength
	}
	policy.Charset = normalizeCharset(policy.Charset)
	if policy.TTL == 0 {
		policy.TTL = DefaultVerificationTTL
	}
	if policy.MaxFailures == 0 {
		policy.MaxFailures = DefaultVerificationMaxFailures
	}
	if policy.ResendAfter == 0 {
		policy.ResendAfter = DefaultVerificationResendAfter
	}
	if policy.HourlyLimit == 0 {
		policy.HourlyLimit = DefaultVerificationHourlyLimit
	}
	if (policy.CallerKey != "" && !validStableKey(policy.CallerKey)) || !validStableKey(policy.Purpose) || policy.Length < 4 || policy.Length > 10 || policy.TTL < time.Minute || policy.TTL > 30*time.Minute || policy.MaxFailures < 1 || policy.MaxFailures > 20 || policy.ResendAfter < time.Second || policy.HourlyLimit < 1 || !validCharset(policy.Charset) {
		return VerificationPolicy{}
	}
	return policy
}

func normalizeCharset(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "numeric", "number", "numbers", "digits", "digit", "纯数字":
		return DefaultVerificationCharset
	case "alphanumeric", "alpha_numeric", "alpha-numeric", "letters", "字母数字":
		return "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	default:
		return value
	}
}

func validCharset(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	seen := make(map[byte]struct{}, len(value))
	for i := 0; i < len(value); i++ {
		b := value[i]
		if b < 0x21 || b > 0x7e {
			return false
		}
		if _, exists := seen[b]; exists {
			return false
		}
		seen[b] = struct{}{}
	}
	return len(seen) > 0
}

func policyLookupKey(caller, key string) string {
	return strings.TrimSpace(caller) + "\x00" + strings.TrimSpace(key)
}

func normalizeNotificationRecipients(recipients []Recipient) ([]Recipient, error) {
	if len(recipients) == 0 || len(recipients) > 100 {
		return nil, ErrInvalidRecipient
	}
	seen := make(map[string]struct{}, len(recipients))
	result := make([]Recipient, 0, len(recipients))
	for _, recipient := range recipients {
		address, err := normalizeRecipient(recipient.Address)
		if err != nil {
			return nil, err
		}
		kind := strings.ToLower(strings.TrimSpace(recipient.Kind))
		if kind == "" {
			kind = "to"
		}
		if kind != "to" && kind != "cc" && kind != "bcc" {
			return nil, ErrInvalidRecipient
		}
		key := kind + ":" + address
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, Recipient{Address: address, Kind: kind})
	}
	if len(result) == 0 {
		return nil, ErrInvalidRecipient
	}
	return result, nil
}

func normalizeRecipient(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return "", ErrInvalidRecipient
	}
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address == "" {
		return "", ErrInvalidRecipient
	}
	return strings.ToLower(strings.TrimSpace(parsed.Address)), nil
}

func recipientAddresses(recipients []Recipient) []string {
	result := make([]string, 0, len(recipients))
	for _, recipient := range recipients {
		result = append(result, recipient.Address)
	}
	return result
}

func notificationPayloadHash(caller, key string, recipients []Recipient, variables map[string]string, locale string, mode SendMode) string {
	orderedRecipients := append([]Recipient(nil), recipients...)
	sort.Slice(orderedRecipients, func(i, j int) bool {
		if orderedRecipients[i].Kind == orderedRecipients[j].Kind {
			return orderedRecipients[i].Address < orderedRecipients[j].Address
		}
		return orderedRecipients[i].Kind < orderedRecipients[j].Kind
	})
	keys := make([]string, 0, len(variables))
	for key := range variables {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	fmt.Fprintf(&builder, "%s\x00%s\x00%s\x00%s", caller, key, normalizeLocale(locale), mode)
	for _, recipient := range orderedRecipients {
		fmt.Fprintf(&builder, "\x00recipient:%s:%s", recipient.Kind, recipient.Address)
	}
	for _, name := range keys {
		fmt.Fprintf(&builder, "\x00%s=%s", name, variables[name])
	}
	sum := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(sum[:])
}

func notificationRenderedPayloadHash(base, generation, subject, body string) string {
	h := sha256.New()
	_, _ = io.WriteString(h, base)
	_, _ = io.WriteString(h, "\x00generation:")
	_, _ = io.WriteString(h, generation)
	_, _ = io.WriteString(h, "\x00subject:")
	_, _ = io.WriteString(h, subject)
	_, _ = io.WriteString(h, "\x00body:")
	_, _ = io.WriteString(h, body)
	return hex.EncodeToString(h.Sum(nil))
}

func renderTemplate(value Template, locale string, variables map[string]string, fallback string) (string, string, error) {
	variant, ok := chooseLocale(value.Locales, locale, value.DefaultLocale, fallback)
	if !ok {
		return "", "", ErrTemplateUnpublished
	}
	values := cloneStringMap(variables)
	for _, name := range value.Variables {
		if _, present := values[name]; !present {
			return "", "", fmt.Errorf("%w: %s", ErrTemplateVariableMissing, name)
		}
	}
	for key := range values {
		if !contains(value.Variables, key) {
			return "", "", fmt.Errorf("%w: %s", ErrTemplateVariableInvalid, key)
		}
	}
	render := func(source string) (string, error) {
		parsed, err := template.New(value.Key).Option("missingkey=error").Parse(source)
		if err != nil {
			return "", fmt.Errorf("%w: parse", ErrTemplateVariableInvalid)
		}
		var output strings.Builder
		if err := parsed.Execute(&output, values); err != nil {
			return "", fmt.Errorf("%w: execute", ErrTemplateVariableInvalid)
		}
		return output.String(), nil
	}
	subject, err := render(variant.Subject)
	if err != nil {
		return "", "", err
	}
	body, err := render(variant.Body)
	if err != nil {
		return "", "", err
	}
	return subject, body, nil
}

func chooseLocale(locales map[string]TemplateLocale, requested, preferred, fallback string) (TemplateLocale, bool) {
	for _, candidate := range []string{requested, preferred, fallback, "zh-CN", "en-US"} {
		candidate = normalizeLocale(candidate)
		if candidate == "" {
			continue
		}
		if value, ok := locales[candidate]; ok {
			return value, true
		}
		base := strings.Split(candidate, "-")[0]
		for key, value := range locales {
			if strings.Split(normalizeLocale(key), "-")[0] == base {
				return value, true
			}
		}
	}
	keys := make([]string, 0, len(locales))
	for key := range locales {
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return TemplateLocale{}, false
	}
	sort.Strings(keys)
	return locales[keys[0]], true
}

func normalizeLocale(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "_", "-"))
	if value == "" {
		return ""
	}
	parts := strings.Split(value, "-")
	if len(parts) == 1 {
		return strings.ToLower(parts[0])
	}
	return strings.ToLower(parts[0]) + "-" + strings.ToUpper(parts[1])
}

func effectiveLocale(ctx context.Context, requested, fallback string) string {
	if value := normalizeLocale(requested); value != "" {
		return value
	}
	if ctx != nil {
		if value := normalizeLocale(ContextMetadataFromContext(ctx).Locale); value != "" {
			return value
		}
	}
	if value := normalizeLocale(fallback); value != "" {
		return value
	}
	return "zh-CN"
}

func normalizeVariables(values []string) []string {
	set := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := set[value]; exists {
			continue
		}
		set[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func cloneLocales(values map[string]TemplateLocale) map[string]TemplateLocale {
	result := make(map[string]TemplateLocale, len(values))
	for key, value := range values {
		value.Locale = normalizeLocale(value.Locale)
		if value.Locale == "" {
			value.Locale = normalizeLocale(key)
		}
		result[value.Locale] = value
	}
	return result
}

func cloneStringMap(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for key, value := range values {
		result[key] = value
	}
	return result
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validStableKey(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func validateGeneratedCode(code string, policy VerificationPolicy) (string, error) {
	if len(code) != policy.Length || code == "" {
		return "", ErrPolicyNotFound
	}
	for _, value := range code {
		if !strings.ContainsRune(policy.Charset, value) {
			return "", ErrPolicyNotFound
		}
	}
	return code, nil
}

func formatTTL(ttl time.Duration) string {
	if ttl%time.Minute == 0 {
		return fmt.Sprintf("%d minutes", int(ttl/time.Minute))
	}
	return ttl.Round(time.Second).String()
}

func newRuntimeID(reader io.Reader) (string, error) {
	var bytes [16]byte
	if _, err := io.ReadFull(reader, bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

var _ NotificationService = (*Runtime)(nil)
var _ VerificationCodeService = (*Runtime)(nil)
