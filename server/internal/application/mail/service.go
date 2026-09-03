// Package mail owns tenant-scoped SMTP account management and durable email
// delivery metadata. Secrets and message bodies stay inside the application
// boundary; transport handlers only receive redacted views.
package mail

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	appnotification "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/notification"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

var (
	ErrInvalidAccount    = errors.New("invalid smtp account")
	ErrAccountNotFound   = errors.New("smtp account not found")
	ErrAccountConflict   = errors.New("smtp account already exists")
	ErrMessageNotFound   = errors.New("email message not found")
	ErrInvalidSend       = errors.New("invalid email message")
	ErrDeliveryFailed    = errors.New("email delivery failed")
	ErrPermissionDenied  = errors.New("email detail permission denied")
	ErrBodyUnavailable   = errors.New("email body unavailable")
	ErrRepositoryFailure = errors.New("mail repository unavailable")
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusSending  Status = "sending"
	StatusRetrying Status = "retrying"
	StatusSent     Status = "sent"
	StatusFailed   Status = "failed"
)

// Encryptor matches settings.Encryptor. The key argument is authenticated
// data, so ciphertext cannot be moved between account/message records.
type Encryptor interface {
	Encrypt(context.Context, string, []byte) ([]byte, error)
	Decrypt(context.Context, string, []byte) ([]byte, error)
}

// Provider is deliberately account-oriented. The service owns tenant
// selection/retry and the platform adapter owns SMTP protocol details.
type Provider interface {
	Send(context.Context, appnotification.SMTPAccount, appnotification.Message) error
	TestConnection(context.Context, appnotification.SMTPAccount) error
}

type ResultProvider interface {
	Provider
	SendWithResult(context.Context, appnotification.SMTPAccount, appnotification.Message) (ProviderResult, error)
}

// StringResultProvider is the dependency-light result seam used by platform
// adapters that should not import this application package merely to return a
// provider message id. ResultProvider remains for in-process fixtures.
type StringResultProvider interface {
	Provider
	SendWithResult(context.Context, appnotification.SMTPAccount, appnotification.Message) (string, error)
}

type ProviderResult struct {
	ProviderMessageID string
}

type Account struct {
	ID                 string     `json:"id"`
	TenantID           string     `json:"tenantId"`
	OrgID              string     `json:"orgId,omitempty"`
	ScopeType          string     `json:"scopeType,omitempty"`
	Name               string     `json:"name"`
	Enabled            bool       `json:"enabled"`
	Host               string     `json:"host"`
	Port               int        `json:"port"`
	Username           string     `json:"username"`
	Weight             int        `json:"weight"`
	FromEmail          string     `json:"fromEmail"`
	FromName           string     `json:"fromName,omitempty"`
	ImplicitTLS        bool       `json:"implicitTls"`
	PasswordConfigured bool       `json:"passwordConfigured"`
	PasswordCiphertext []byte     `json:"-"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
	DeletedAt          *time.Time `json:"deletedAt,omitempty"`
}

type AccountInput struct {
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	Weight      int    `json:"weight"`
	FromEmail   string `json:"fromEmail"`
	FromName    string `json:"fromName"`
	ImplicitTLS bool   `json:"implicitTls"`
}

type EmailMessage struct {
	ID                   string      `json:"id"`
	TenantID             string      `json:"tenantId"`
	OrgID                string      `json:"orgId,omitempty"`
	ScopeType            string      `json:"scopeType,omitempty"`
	SMTPAccountID        string      `json:"smtpAccountId,omitempty"`
	SenderID             string      `json:"senderId,omitempty"`
	CallerKey            string      `json:"callerKey,omitempty"`
	TemplateKey          string      `json:"templateKey,omitempty"`
	TemplateGeneration   string      `json:"templateGeneration,omitempty"`
	PolicyGeneration     string      `json:"policyGeneration,omitempty"`
	Locale               string      `json:"locale,omitempty"`
	IsTest               bool        `json:"isTest,omitempty"`
	ChallengeID          string      `json:"challengeId,omitempty"`
	RelayStatus          string      `json:"relayStatus,omitempty"`
	Subject              string      `json:"subject"`
	Recipients           []Recipient `json:"recipients"`
	BodyCiphertext       []byte      `json:"-"`
	BodyDigest           string      `json:"bodyDigest"`
	Status               Status      `json:"status"`
	AttemptCount         int         `json:"attemptCount"`
	ProviderMessageID    string      `json:"providerMessageId,omitempty"`
	LastErrorCode        string      `json:"lastErrorCode,omitempty"`
	SentAt               *time.Time  `json:"sentAt,omitempty"`
	IdempotencyKey       string      `json:"idempotencyKey,omitempty"`
	IdempotencyScopeHash string      `json:"-"`
	CreatedAt            time.Time   `json:"createdAt"`
	UpdatedAt            time.Time   `json:"updatedAt"`
	DeletedAt            *time.Time  `json:"deletedAt,omitempty"`
}

// MessageView is the only representation exposed to HTTP/UI clients. Body is
// populated only for an explicitly authorized detail request.
type MessageView struct {
	ID                 string      `json:"id"`
	TenantID           string      `json:"tenantId"`
	OrgID              string      `json:"orgId,omitempty"`
	ScopeType          string      `json:"scopeType,omitempty"`
	SMTPAccountID      string      `json:"smtpAccountId,omitempty"`
	SenderID           string      `json:"senderId,omitempty"`
	CallerKey          string      `json:"callerKey,omitempty"`
	TemplateKey        string      `json:"templateKey,omitempty"`
	TemplateGeneration string      `json:"templateGeneration,omitempty"`
	PolicyGeneration   string      `json:"policyGeneration,omitempty"`
	Locale             string      `json:"locale,omitempty"`
	IsTest             bool        `json:"isTest,omitempty"`
	ChallengeID        string      `json:"challengeId,omitempty"`
	RelayStatus        string      `json:"relayStatus,omitempty"`
	Subject            string      `json:"subject"`
	Recipients         []Recipient `json:"recipients"`
	Body               string      `json:"body,omitempty"`
	BodyDigest         string      `json:"bodyDigest"`
	Status             Status      `json:"status"`
	AttemptCount       int         `json:"attemptCount"`
	ProviderMessageID  string      `json:"providerMessageId,omitempty"`
	LastErrorCode      string      `json:"lastErrorCode,omitempty"`
	SentAt             *time.Time  `json:"sentAt,omitempty"`
	IdempotencyKey     string      `json:"idempotencyKey,omitempty"`
	CreatedAt          time.Time   `json:"createdAt"`
	UpdatedAt          time.Time   `json:"updatedAt"`
}

type SendInput struct {
	Recipients     []string `json:"recipients"`
	To             string   `json:"to,omitempty"`
	Subject        string   `json:"subject"`
	Body           string   `json:"body"`
	IdempotencyKey string   `json:"idempotencyKey,omitempty"`
	// The following fields are populated by trusted application adapters, not
	// by public SMTP request bodies. They let the common notification runtime
	// select a caller-specific account pool while preserving the legacy API.
	CallerKey          string                   `json:"-"`
	TemplateKey        string                   `json:"-"`
	TemplateGeneration string                   `json:"-"`
	PolicyGeneration   string                   `json:"-"`
	Locale             string                   `json:"-"`
	Mode               appnotification.SendMode `json:"-"`
	IsTest             bool                     `json:"-"`
	ChallengeID        string                   `json:"-"`
}

type MessageFilter struct {
	Status string
	Limit  int
	Offset int
}

type MessagePage struct {
	Items  []MessageView `json:"items"`
	Total  int           `json:"total"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
}

type ConnectionResult struct {
	AccountID string    `json:"accountId"`
	RequestID string    `json:"requestId"`
	Status    string    `json:"status"`
	Stage     string    `json:"stage,omitempty"`
	Code      string    `json:"code,omitempty"`
	Message   string    `json:"message,omitempty"`
	CheckedAt time.Time `json:"checkedAt"`
}

type Attempt struct {
	ID        string    `json:"id"`
	MessageID string    `json:"messageId"`
	AccountID string    `json:"accountId"`
	AttemptNo int       `json:"attemptNo"`
	Stage     string    `json:"stage,omitempty"`
	Code      string    `json:"code,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type AccountRepository interface {
	List(context.Context, string, string) ([]Account, error)
	Get(context.Context, string, ...string) (Account, error)
	Create(context.Context, Account) (Account, error)
	Update(context.Context, Account) (Account, error)
	SoftDelete(context.Context, string, string, string, time.Time) error
}

type MessageRepository interface {
	Create(context.Context, EmailMessage) (EmailMessage, error)
	Update(context.Context, EmailMessage) (EmailMessage, error)
	List(context.Context, string, string, MessageFilter) (MessagePage, error)
	Get(context.Context, string, ...string) (EmailMessage, error)
}

type IdempotencyRepository interface {
	GetByIdempotency(context.Context, string, string) (EmailMessage, error)
}

// ScopedIdempotencyRepository is an optional stronger lookup seam. Legacy
// repositories can keep implementing IdempotencyRepository; new durable
// adapters should include organization, caller and template in the lookup so
// a client key cannot collide across independent delivery streams.
type ScopedIdempotencyRepository interface {
	GetByIdempotencyScope(context.Context, string, string, string, string, string) (EmailMessage, error)
}

type AttemptRepository interface {
	Append(context.Context, Attempt) error
}

type Config struct {
	Cipher      Encryptor
	Selection   appnotification.SMTPSelection
	RNG         func(int) int
	Clock       func() time.Time
	MaxAttempts int
	RetryDelays []time.Duration
	Cooldown    time.Duration
}

// CallerRoute is the final-state SMTP account policy for one public caller.
// Account IDs are an allow-list; disabled or cooling accounts are filtered at
// dispatch time. Strategy and weights are copied on registration so callers
// cannot mutate the live route through a shared slice/map.
type CallerRoute struct {
	AccountIDs       []string
	DefaultAccountID string
	Strategy         appnotification.SMTPSelection
	Weights          map[string]int
}

type RoutingPolicy = appnotification.SMTPSelection

const (
	RoutingWeightedRandom = appnotification.SMTPSelectionWeightedRandom
	RoutingRoundRobin     = appnotification.SMTPSelectionRoundRobin
)

type Service struct {
	accounts      AccountRepository
	messages      MessageRepository
	attempts      AttemptRepository
	provider      Provider
	cipher        Encryptor
	selection     appnotification.SMTPSelection
	rng           func(int) int
	clock         func() time.Time
	max           int
	delays        []time.Duration
	cooldown      time.Duration
	sequence      uint64
	mu            sync.RWMutex
	cooling       map[string]time.Time
	routes        map[string]CallerRoute
	routeSeq      map[string]uint64
	idempotencyMu sync.Mutex
}

func NewService(accounts AccountRepository, messages MessageRepository, provider Provider, cfg Config) *Service {
	selection := cfg.Selection
	if selection == "" {
		selection = appnotification.SMTPSelectionWeightedRandom
	}
	clock := cfg.Clock
	if clock == nil {
		clock = time.Now
	}
	max := cfg.MaxAttempts
	if max <= 0 {
		max = 3
	}
	delays := append([]time.Duration(nil), cfg.RetryDelays...)
	if delays == nil {
		delays = []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	}
	return &Service{accounts: accounts, messages: messages, provider: provider, cipher: cfg.Cipher, selection: selection, rng: cfg.RNG, clock: clock, max: max, delays: delays, cooldown: cfg.Cooldown, cooling: make(map[string]time.Time), routes: make(map[string]CallerRoute), routeSeq: make(map[string]uint64)}
}

// SetCallerRoute atomically replaces a caller's account allow-list and
// strategy. An empty strategy inherits the service default. This is the
// runtime hot-reload seam used by the administration UI.
func (s *Service) SetCallerRoute(caller string, route CallerRoute) {
	s.SetCallerRouteFor(context.Background(), caller, route)
}

// SetCallerRouteFor scopes a route to the effective tenant/organization. The
// global wrapper remains for in-process compatibility and system defaults.
func (s *Service) SetCallerRouteFor(ctx context.Context, caller string, route CallerRoute) {
	if s == nil {
		return
	}
	caller = strings.TrimSpace(caller)
	if caller == "" {
		return
	}
	if route.Strategy == "" {
		route.Strategy = s.selection
	}
	if route.Strategy != appnotification.SMTPSelectionWeightedRandom && route.Strategy != appnotification.SMTPSelectionRoundRobin {
		return
	}
	route.AccountIDs = uniqueStrings(route.AccountIDs)
	route.DefaultAccountID = strings.TrimSpace(route.DefaultAccountID)
	weights := make(map[string]int, len(route.Weights))
	for id, weight := range route.Weights {
		id = strings.TrimSpace(id)
		if id == "" || weight <= 0 {
			continue
		}
		weights[id] = weight
	}
	route.Weights = weights
	key := routeKey(runtimeScopeKey(ctx), caller)
	s.mu.Lock()
	s.routes[key] = route
	s.routeSeq[key] = 0
	s.mu.Unlock()
}

func (s *Service) DeleteCallerRoute(caller string) {
	s.DeleteCallerRouteFor(context.Background(), caller)
}

func (s *Service) DeleteCallerRouteFor(ctx context.Context, caller string) {
	if s == nil {
		return
	}
	key := routeKey(runtimeScopeKey(ctx), caller)
	s.mu.Lock()
	delete(s.routes, key)
	delete(s.routeSeq, key)
	s.mu.Unlock()
}

func (s *Service) CallerRoute(caller string) (CallerRoute, bool) {
	return s.CallerRouteFor(context.Background(), caller)
}

func (s *Service) CallerRouteFor(ctx context.Context, caller string) (CallerRoute, bool) {
	if s == nil {
		return CallerRoute{}, false
	}
	caller = strings.TrimSpace(caller)
	s.mu.RLock()
	var route CallerRoute
	ok := false
	for _, scope := range routeScopes(ctx, s.routes) {
		if candidate, exists := s.routes[routeKey(scope, caller)]; exists {
			route, ok = candidate, true
			break
		}
	}
	s.mu.RUnlock()
	if !ok {
		return CallerRoute{}, false
	}
	return cloneCallerRoute(route), true
}

func (s *Service) SetAttemptRepository(repository AttemptRepository) {
	if s != nil {
		s.attempts = repository
	}
}

func (s *Service) CreateAccount(ctx context.Context, input AccountInput) (Account, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil || s == nil || s.accounts == nil || s.cipher == nil {
		return Account{}, wrapInvalid(err)
	}
	input, err = normalizeAccountInput(input)
	if err != nil {
		return Account{}, err
	}
	id, err := newID()
	if err != nil {
		return Account{}, err
	}
	ciphertext, err := s.cipher.Encrypt(ctx, "smtp/account/"+id, []byte(input.Password))
	if err != nil {
		return Account{}, fmt.Errorf("encrypt smtp password: %w", err)
	}
	now := s.clock().UTC()
	account := Account{ID: id, TenantID: scope.TenantID, OrgID: scope.Organization, ScopeType: scopeType(scope), Name: input.Name, Enabled: input.Enabled, Host: input.Host, Port: input.Port, Username: input.Username, Weight: normalizedWeight(input.Weight), FromEmail: input.FromEmail, FromName: input.FromName, ImplicitTLS: input.ImplicitTLS, PasswordConfigured: strings.TrimSpace(input.Password) != "", PasswordCiphertext: ciphertext, CreatedAt: now, UpdatedAt: now}
	created, err := s.accounts.Create(ctx, account)
	if err != nil {
		if errors.Is(err, ErrAccountConflict) {
			return Account{}, err
		}
		return Account{}, fmt.Errorf("%w: create account", ErrRepositoryFailure)
	}
	return sanitizeAccount(created), nil
}

// GetAccount returns a redacted account in the caller's effective scope.
// Transport adapters use it to preserve omitted patch fields while keeping
// encrypted credentials inside the application boundary.
func (s *Service) GetAccount(ctx context.Context, id string) (Account, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil || s == nil || s.accounts == nil {
		return Account{}, wrapInvalid(err)
	}
	account, err := s.accounts.Get(ctx, strings.TrimSpace(id), repositoryTenant(scope), repositoryOrg(scope))
	if err != nil {
		return Account{}, err
	}
	return sanitizeAccount(account), nil
}

func (s *Service) UpdateAccount(ctx context.Context, id string, input AccountInput) (Account, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil || s == nil || s.accounts == nil || s.cipher == nil {
		return Account{}, wrapInvalid(err)
	}
	existing, err := s.accounts.Get(ctx, strings.TrimSpace(id), repositoryTenant(scope), repositoryOrg(scope))
	if err != nil {
		return Account{}, err
	}
	input, err = normalizeAccountInputForUpdate(input, existing)
	if err != nil {
		return Account{}, err
	}
	existing.Name, existing.Enabled, existing.Host, existing.Port = input.Name, input.Enabled, input.Host, input.Port
	existing.Username, existing.Weight, existing.FromEmail, existing.FromName, existing.ImplicitTLS = input.Username, normalizedWeight(input.Weight), input.FromEmail, input.FromName, input.ImplicitTLS
	if strings.TrimSpace(input.Password) != "" {
		existing.PasswordCiphertext, err = s.cipher.Encrypt(ctx, "smtp/account/"+existing.ID, []byte(input.Password))
		if err != nil {
			return Account{}, fmt.Errorf("encrypt smtp password: %w", err)
		}
		existing.PasswordConfigured = true
	}
	existing.UpdatedAt = s.clock().UTC()
	updated, err := s.accounts.Update(ctx, existing)
	if err != nil {
		return Account{}, err
	}
	return sanitizeAccount(updated), nil
}

func (s *Service) ListAccounts(ctx context.Context) ([]Account, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil || s == nil || s.accounts == nil {
		return nil, wrapInvalid(err)
	}
	items, err := s.accounts.List(ctx, repositoryTenant(scope), repositoryOrg(scope))
	if err != nil {
		return nil, fmt.Errorf("%w: list accounts", ErrRepositoryFailure)
	}
	for i := range items {
		items[i] = sanitizeAccount(items[i])
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func (s *Service) DeleteAccount(ctx context.Context, id string) error {
	scope, err := tenant.RequireContext(ctx)
	if err != nil || s == nil || s.accounts == nil {
		return wrapInvalid(err)
	}
	if err := s.accounts.SoftDelete(ctx, strings.TrimSpace(id), repositoryTenant(scope), repositoryOrg(scope), s.clock().UTC()); err != nil {
		return err
	}
	return nil
}

func (s *Service) TestAccount(ctx context.Context, id, requestID string) (ConnectionResult, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil || s == nil || s.accounts == nil || s.provider == nil || s.cipher == nil {
		return ConnectionResult{}, wrapInvalid(err)
	}
	account, err := s.accounts.Get(ctx, strings.TrimSpace(id), repositoryTenant(scope), repositoryOrg(scope))
	if err != nil {
		return ConnectionResult{}, err
	}
	providerAccount, err := s.providerAccount(ctx, account)
	if err != nil {
		return ConnectionResult{}, err
	}
	result := ConnectionResult{AccountID: account.ID, RequestID: strings.TrimSpace(requestID), CheckedAt: s.clock().UTC(), Status: "ok"}
	if err := s.provider.TestConnection(ctx, providerAccount); err != nil {
		result.Status = "failed"
		result.Stage, result.Code = providerErrorFields(err)
		if result.Code == "" {
			result.Code = "provider_unavailable"
		}
		result.Message = "SMTP connection failed"
	}
	return result, nil
}

// Send serializes keyed requests in-process. The durable repository still
// owns the cross-process uniqueness constraint; the process lock closes the
// read-then-create race for the memory adapter and prevents duplicate provider
// dispatches when two goroutines retry the same key at once.
func (s *Service) Send(ctx context.Context, input SendInput) (MessageView, error) {
	if s == nil {
		return MessageView{}, ErrRepositoryFailure
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return s.send(ctx, input)
	}
	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()
	return s.send(ctx, input)
}

func (s *Service) send(ctx context.Context, input SendInput) (MessageView, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil || s == nil || s.accounts == nil || s.messages == nil || s.provider == nil || s.cipher == nil {
		return MessageView{}, wrapInvalid(err)
	}
	recipients, err := normalizeRecipients(input)
	if err != nil {
		return MessageView{}, err
	}
	if strings.TrimSpace(input.Subject) == "" || len(input.Subject) > 998 || len(input.Body) > 10<<20 {
		return MessageView{}, ErrInvalidSend
	}
	callerKey := strings.TrimSpace(input.CallerKey)
	if trusted := strings.TrimSpace(appnotification.ContextMetadataFromContext(ctx).CallerKey); trusted != "" {
		callerKey = trusted
	}
	templateKey := strings.TrimSpace(input.TemplateKey)
	locale := strings.TrimSpace(input.Locale)
	if trustedLocale := strings.TrimSpace(appnotification.ContextMetadataFromContext(ctx).Locale); trustedLocale != "" {
		locale = trustedLocale
	}
	isTest := input.IsTest || input.Mode == appnotification.SendModeAdminTest
	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	bodyDigest := sha256.Sum256([]byte(input.Body))
	if idempotencyKey != "" {
		if existing, found, lookupErr := s.lookupIdempotent(ctx, scope, callerKey, templateKey, idempotencyKey); lookupErr != nil {
			return MessageView{}, lookupErr
		} else if found {
			if !sameIdempotencyPayload(existing, input, callerKey, templateKey, locale, isTest, strings.TrimSpace(input.Subject), hex.EncodeToString(bodyDigest[:]), recipients) {
				return MessageView{}, appnotification.ErrIdempotencyConflict
			}
			return s.view(ctx, existing, false)
		}
	}
	accounts, err := s.accounts.List(ctx, repositoryTenant(scope), repositoryOrg(scope))
	if err != nil {
		return MessageView{}, fmt.Errorf("%w: list accounts", ErrRepositoryFailure)
	}
	route, hasRoute := s.CallerRouteFor(ctx, callerKey)
	ordered := s.orderAccountsForScoped(accounts, runtimeScopeKey(ctx), callerKey, route, hasRoute)
	id, err := newID()
	if err != nil {
		return MessageView{}, err
	}
	ciphertext, err := s.cipher.Encrypt(ctx, "email/message/"+id, []byte(input.Body))
	if err != nil {
		return MessageView{}, fmt.Errorf("encrypt email body: %w", err)
	}
	now := s.clock().UTC()
	scopeHash := ""
	if idempotencyKey != "" {
		scopeHash = idempotencyScopeHash(scope.TenantID, scope.Organization, callerKey, templateKey, idempotencyKey)
	}
	record := EmailMessage{ID: id, TenantID: scope.TenantID, OrgID: scope.Organization, ScopeType: scopeType(scope), SenderID: actorFromContext(ctx), CallerKey: callerKey, TemplateKey: templateKey, TemplateGeneration: strings.TrimSpace(input.TemplateGeneration), PolicyGeneration: strings.TrimSpace(input.PolicyGeneration), Locale: locale, IsTest: isTest, ChallengeID: strings.TrimSpace(input.ChallengeID), RelayStatus: "pending", Subject: strings.TrimSpace(input.Subject), Recipients: recipients, BodyCiphertext: ciphertext, BodyDigest: hex.EncodeToString(bodyDigest[:]), Status: StatusPending, IdempotencyKey: idempotencyKey, IdempotencyScopeHash: scopeHash, CreatedAt: now, UpdatedAt: now}
	record, err = s.messages.Create(ctx, record)
	if err != nil {
		// A unique database index may win a cross-process race after the
		// preflight lookup. Re-read the keyed record and apply the same payload
		// comparison before surfacing the storage error.
		if idempotencyKey != "" {
			if existing, found, lookupErr := s.lookupIdempotent(ctx, scope, callerKey, templateKey, idempotencyKey); lookupErr == nil && found {
				if sameIdempotencyPayload(existing, input, callerKey, templateKey, locale, isTest, strings.TrimSpace(input.Subject), hex.EncodeToString(bodyDigest[:]), recipients) {
					return s.view(ctx, existing, false)
				}
				return MessageView{}, appnotification.ErrIdempotencyConflict
			}
		}
		return MessageView{}, err
	}
	if len(ordered) == 0 {
		record.Status = StatusFailed
		record.LastErrorCode = "no_enabled_account"
		record.UpdatedAt = s.clock().UTC()
		updated, updateErr := s.messages.Update(ctx, record)
		if updateErr != nil {
			return MessageView{}, updateErr
		}
		view, _ := s.view(ctx, updated, false)
		return view, errors.Join(ErrDeliveryFailed, appnotification.ErrProvider)
	}
	var last error
	attempts := 0
	for _, account := range ordered {
		if attempts >= s.max {
			break
		}
		if s.isCooling(account.ID) {
			continue
		}
		if attempts > 0 {
			idx := attempts - 1
			if idx >= len(s.delays) {
				idx = len(s.delays) - 1
			}
			if idx >= 0 {
				if err := wait(ctx, s.delays[idx]); err != nil {
					last = err
					break
				}
			}
		}
		attempts++
		record.SMTPAccountID = account.ID
		record.AttemptCount = attempts
		record.Status = StatusSending
		record.UpdatedAt = s.clock().UTC()
		if _, updateErr := s.messages.Update(ctx, record); updateErr != nil {
			return MessageView{}, updateErr
		}
		providerAccount, decryptErr := s.providerAccount(ctx, account)
		if decryptErr != nil {
			last = decryptErr
		} else {
			recipientAddresses := make([]string, 0, len(recipients))
			for _, recipient := range recipients {
				recipientAddresses = append(recipientAddresses, recipient.Address)
			}
			notificationMessage := appnotification.Message{ID: record.ID, To: recipients[0].Address, Recipients: recipientAddresses, Subject: record.Subject, Body: input.Body, CallerKey: record.CallerKey, TemplateKey: record.TemplateKey, TemplateGeneration: record.TemplateGeneration, PolicyGeneration: record.PolicyGeneration, Locale: record.Locale, Mode: input.Mode, IsTest: record.IsTest, ChallengeID: record.ChallengeID, IdempotencyKey: record.IdempotencyKey, CreatedAt: record.CreatedAt}
			if resultProvider, ok := s.provider.(StringResultProvider); ok {
				record.ProviderMessageID, last = resultProvider.SendWithResult(ctx, providerAccount, notificationMessage)
			} else if resultProvider, ok := s.provider.(ResultProvider); ok {
				var providerResult ProviderResult
				providerResult, last = resultProvider.SendWithResult(ctx, providerAccount, notificationMessage)
				record.ProviderMessageID = providerResult.ProviderMessageID
			} else {
				last = s.provider.Send(ctx, providerAccount, notificationMessage)
			}
		}
		stage, code := providerErrorFields(last)
		attempt := Attempt{ID: newAttemptID(), MessageID: record.ID, AccountID: account.ID, AttemptNo: attempts, Stage: stage, Code: code, CreatedAt: s.clock().UTC()}
		if s.attempts != nil {
			if appendErr := s.attempts.Append(ctx, attempt); appendErr != nil && last == nil {
				last = appendErr
			}
		}
		if last == nil {
			sentAt := s.clock().UTC()
			record.Status, record.RelayStatus, record.SentAt, record.UpdatedAt, record.LastErrorCode = StatusSent, "sent", &sentAt, sentAt, ""
			updated, updateErr := s.messages.Update(ctx, record)
			if updateErr != nil {
				return MessageView{}, updateErr
			}
			return s.view(ctx, updated, false)
		}
		record.LastErrorCode = code
		record.Status = StatusRetrying
		s.markCooling(account.ID)
	}
	record.Status, record.RelayStatus = StatusFailed, "failed"
	record.UpdatedAt = s.clock().UTC()
	if record.LastErrorCode == "" {
		_, record.LastErrorCode = providerErrorFields(last)
		if record.LastErrorCode == "" {
			record.LastErrorCode = "provider_unavailable"
		}
	}
	if _, updateErr := s.messages.Update(ctx, record); updateErr != nil {
		return MessageView{}, updateErr
	}
	view, _ := s.view(ctx, record, false)
	if last == nil {
		last = appnotification.ErrProvider
	}
	return view, errors.Join(ErrDeliveryFailed, last)
}

// lookupIdempotent resolves a keyed message in the caller's exact delivery
// scope. A legacy repository may only expose tenant+key; the application then
// performs the remaining caller/template comparison before reusing a record.
func (s *Service) lookupIdempotent(ctx context.Context, scope tenant.Context, caller, template, key string) (EmailMessage, bool, error) {
	if s == nil || s.messages == nil || strings.TrimSpace(key) == "" {
		return EmailMessage{}, false, nil
	}
	if repository, ok := s.messages.(ScopedIdempotencyRepository); ok {
		record, err := repository.GetByIdempotencyScope(ctx, scope.TenantID, scope.Organization, caller, template, key)
		if err == nil {
			return record, true, nil
		}
		if !errors.Is(err, ErrMessageNotFound) {
			return EmailMessage{}, false, fmt.Errorf("%w: lookup idempotency", ErrRepositoryFailure)
		}
		return EmailMessage{}, false, nil
	}
	if repository, ok := s.messages.(IdempotencyRepository); ok {
		record, err := repository.GetByIdempotency(ctx, scope.TenantID, key)
		if err == nil {
			return record, true, nil
		}
		if !errors.Is(err, ErrMessageNotFound) {
			return EmailMessage{}, false, fmt.Errorf("%w: lookup idempotency", ErrRepositoryFailure)
		}
	}
	return EmailMessage{}, false, nil
}

func sameIdempotencyPayload(existing EmailMessage, input SendInput, caller, template, locale string, isTest bool, subject, bodyDigest string, recipients []Recipient) bool {
	if existing.CallerKey != caller || existing.TemplateKey != template || existing.Locale != locale || existing.IsTest != isTest || existing.Subject != subject || existing.BodyDigest != bodyDigest || existing.ChallengeID != strings.TrimSpace(input.ChallengeID) || existing.TemplateGeneration != strings.TrimSpace(input.TemplateGeneration) || existing.PolicyGeneration != strings.TrimSpace(input.PolicyGeneration) {
		return false
	}
	left := canonicalRecipients(existing.Recipients)
	right := canonicalRecipients(recipients)
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func canonicalRecipients(values []Recipient) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, strings.ToLower(strings.TrimSpace(value.Kind))+":"+strings.ToLower(strings.TrimSpace(value.Address)))
	}
	sort.Strings(result)
	return result
}

func idempotencyScopeHash(tenantID, orgID, caller, template, key string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{strings.TrimSpace(tenantID), strings.TrimSpace(orgID), strings.TrimSpace(caller), strings.TrimSpace(template), strings.TrimSpace(key)}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (s *Service) ListMessages(ctx context.Context, filter MessageFilter) (MessagePage, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil || s == nil || s.messages == nil {
		return MessagePage{}, wrapInvalid(err)
	}
	return s.messages.List(ctx, repositoryTenant(scope), repositoryOrg(scope), filter)
}

func (s *Service) GetMessage(ctx context.Context, id string, includeBody bool) (MessageView, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil || s == nil || s.messages == nil {
		return MessageView{}, wrapInvalid(err)
	}
	record, err := s.messages.Get(ctx, strings.TrimSpace(id), repositoryTenant(scope), repositoryOrg(scope))
	if err != nil {
		return MessageView{}, err
	}
	if includeBody && !scope.PlatformAdmin {
		return MessageView{}, ErrPermissionDenied
	}
	return s.view(ctx, record, includeBody)
}

func (s *Service) providerAccount(ctx context.Context, account Account) (appnotification.SMTPAccount, error) {
	if s.cipher == nil {
		return appnotification.SMTPAccount{}, ErrBodyUnavailable
	}
	password := []byte(nil)
	if len(account.PasswordCiphertext) > 0 {
		decrypted, err := s.cipher.Decrypt(ctx, "smtp/account/"+account.ID, account.PasswordCiphertext)
		if err != nil {
			return appnotification.SMTPAccount{}, ErrBodyUnavailable
		}
		password = decrypted
	}
	return appnotification.SMTPAccount{Enabled: account.Enabled, Name: account.Name, TenantID: account.TenantID, OrgID: account.OrgID, ScopeType: account.ScopeType, Host: account.Host, Port: account.Port, Username: account.Username, Password: string(password), Weight: account.Weight, FromEmail: account.FromEmail, FromName: account.FromName, ImplicitTLS: account.ImplicitTLS}, nil
}

func (s *Service) view(ctx context.Context, record EmailMessage, includeBody bool) (MessageView, error) {
	view := MessageView{
		ID: record.ID, TenantID: record.TenantID, OrgID: record.OrgID, ScopeType: record.ScopeType,
		SMTPAccountID: record.SMTPAccountID, SenderID: record.SenderID, CallerKey: record.CallerKey,
		TemplateKey: record.TemplateKey, TemplateGeneration: record.TemplateGeneration,
		PolicyGeneration: record.PolicyGeneration, Locale: record.Locale, IsTest: record.IsTest,
		ChallengeID: record.ChallengeID, RelayStatus: record.RelayStatus, Subject: record.Subject,
		Recipients: cloneRecipients(record.Recipients), BodyDigest: record.BodyDigest, Status: record.Status,
		AttemptCount: record.AttemptCount, ProviderMessageID: record.ProviderMessageID,
		LastErrorCode: record.LastErrorCode, SentAt: record.SentAt, IdempotencyKey: record.IdempotencyKey,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
	}
	if includeBody {
		if s.cipher == nil || len(record.BodyCiphertext) == 0 {
			return MessageView{}, ErrBodyUnavailable
		}
		body, err := s.cipher.Decrypt(ctx, "email/message/"+record.ID, record.BodyCiphertext)
		if err != nil {
			return MessageView{}, ErrBodyUnavailable
		}
		view.Body = string(body)
	}
	return view, nil
}

func (s *Service) orderAccounts(accounts []Account) []Account {
	return s.orderAccountsFor(accounts, "", CallerRoute{}, false)
}

// orderAccountsFor applies an optional caller route before the service-level
// weighted/round-robin policy. A configured route never silently falls back to
// unrelated accounts: an empty eligible set is reported as a delivery failure
// by Send.
func (s *Service) orderAccountsFor(accounts []Account, caller string, route CallerRoute, hasRoute bool) []Account {
	return s.orderAccountsForScoped(accounts, "", caller, route, hasRoute)
}

func (s *Service) orderAccountsForScoped(accounts []Account, scopeKey, caller string, route CallerRoute, hasRoute bool) []Account {
	enabled := make([]Account, 0, len(accounts))
	allowed := make(map[string]struct{}, len(route.AccountIDs))
	if hasRoute {
		for _, id := range route.AccountIDs {
			allowed[strings.TrimSpace(id)] = struct{}{}
		}
	}
	for _, account := range accounts {
		if account.DeletedAt != nil || !account.Enabled {
			continue
		}
		if hasRoute {
			if _, ok := allowed[account.ID]; !ok {
				continue
			}
		}
		if account.Weight <= 0 {
			account.Weight = 1
		}
		if weight, ok := route.Weights[account.ID]; ok {
			account.Weight = weight
		}
		enabled = append(enabled, account)
	}
	if len(enabled) == 0 {
		return nil
	}
	start := 0
	selection := s.selection
	if hasRoute && route.Strategy != "" {
		selection = route.Strategy
	}
	if selection == appnotification.SMTPSelectionRoundRobin {
		if hasRoute {
			routeSequenceKey := routeKey(scopeKey, caller)
			s.mu.Lock()
			seq := s.routeSeq[routeSequenceKey]
			s.routeSeq[routeSequenceKey] = seq + 1
			s.mu.Unlock()
			start = int(seq % uint64(len(enabled)))
		} else {
			start = int(atomic.AddUint64(&s.sequence, 1)-1) % len(enabled)
		}
	} else {
		total := 0
		for _, account := range enabled {
			total += account.Weight
		}
		rng := s.rng
		if rng == nil {
			// A deterministic fallback keeps tests and a restarted single-node
			// instance reproducible while callers may inject secure randomness.
			rng = func(n int) int { return int(time.Now().UnixNano() % int64(n)) }
		}
		n := rng(total)
		if n < 0 {
			n = 0
		}
		n %= total
		for i, account := range enabled {
			if n < account.Weight {
				start = i
				break
			}
			n -= account.Weight
		}
	}
	ordered := make([]Account, 0, len(enabled))
	for i := range enabled {
		ordered = append(ordered, enabled[(start+i)%len(enabled)])
	}
	if hasRoute && route.DefaultAccountID != "" {
		for i, account := range ordered {
			if account.ID != route.DefaultAccountID {
				continue
			}
			if i > 0 {
				ordered = append([]Account{account}, append(ordered[:i], ordered[i+1:]...)...)
			}
			break
		}
	}
	return ordered
}

func cloneCallerRoute(route CallerRoute) CallerRoute {
	route.AccountIDs = append([]string(nil), route.AccountIDs...)
	route.Weights = make(map[string]int, len(route.Weights))
	for key, value := range route.Weights {
		route.Weights[key] = value
	}
	return route
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func scopeType(scope tenant.Context) string {
	if strings.TrimSpace(scope.Organization) != "" {
		return "org"
	}
	return "tenant"
}

func runtimeScopeKey(ctx context.Context) string {
	if scope, ok := tenant.FromContext(ctx); ok {
		return strings.TrimSpace(scope.TenantID) + ":" + strings.TrimSpace(scope.Organization)
	}
	return ""
}

func routeKey(scope, caller string) string {
	return strings.TrimSpace(scope) + "\x00" + strings.TrimSpace(caller)
}

func routeScopes(ctx context.Context, routes map[string]CallerRoute) []string {
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
			for key := range routes {
				if index := strings.IndexByte(key, '\x00'); index >= 0 {
					scopeKey := key[:index]
					if _, exists := seen[scopeKey]; !exists {
						result = append(result, scopeKey)
						seen[scopeKey] = struct{}{}
					}
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

func (s *Service) isCooling(id string) bool {
	if s.cooldown <= 0 {
		return false
	}
	now := s.clock().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	until, ok := s.cooling[id]
	return ok && now.Before(until)
}

func (s *Service) markCooling(id string) {
	if s.cooldown <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cooling[id] = s.clock().UTC().Add(s.cooldown)
}

func normalizeAccountInput(input AccountInput) (AccountInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Host = strings.TrimSpace(input.Host)
	input.Username = strings.TrimSpace(input.Username)
	input.FromEmail = strings.TrimSpace(input.FromEmail)
	input.FromName = strings.TrimSpace(input.FromName)
	if input.Port == 465 {
		input.ImplicitTLS = true
	}
	if input.Weight < 0 || input.Weight > 1_000_000 || input.Name == "" || input.Host == "" || input.Port < 1 || input.Port > 65535 || strings.ContainsAny(input.Name+input.Host+input.Username+input.Password+input.FromEmail+input.FromName, "\r\n") {
		return AccountInput{}, ErrInvalidAccount
	}
	if _, err := mail.ParseAddress(input.FromEmail); err != nil {
		return AccountInput{}, ErrInvalidAccount
	}
	if input.Username != "" && strings.TrimSpace(input.Password) == "" {
		return AccountInput{}, ErrInvalidAccount
	}
	return input, nil
}

func normalizeAccountInputForUpdate(input AccountInput, existing Account) (AccountInput, error) {
	password := input.Password
	if strings.TrimSpace(password) == "" && strings.TrimSpace(input.Username) != "" && !strings.EqualFold(strings.TrimSpace(input.Username), strings.TrimSpace(existing.Username)) {
		return AccountInput{}, ErrInvalidAccount
	}
	if input.Name == "" {
		input.Name = existing.Name
	}
	if input.Host == "" {
		input.Host = existing.Host
	}
	if input.Port == 0 {
		input.Port = existing.Port
	}
	if input.Username == "" {
		input.Username = existing.Username
	}
	if input.FromEmail == "" {
		input.FromEmail = existing.FromEmail
	}
	if input.FromName == "" {
		input.FromName = existing.FromName
	}
	if input.Weight == 0 {
		input.Weight = existing.Weight
	}
	if !input.Enabled {
		// false is a meaningful update; preserve only when the caller omitted
		// all fields (the HTTP layer always sends enabled explicitly).
	}
	if input.Port == 465 {
		input.ImplicitTLS = true
	}
	// Validation of an update may retain the encrypted password when the
	// caller leaves password empty; creation still requires a password for
	// credentialed accounts.
	if strings.TrimSpace(password) == "" && input.Username != "" {
		input.Password = "update-placeholder"
	}
	normalized, err := normalizeAccountInput(input)
	if err != nil {
		return AccountInput{}, err
	}
	normalized.Password = password
	return normalized, nil
}

func normalizedWeight(weight int) int {
	if weight <= 0 {
		return 1
	}
	return weight
}

func normalizeRecipients(input SendInput) ([]Recipient, error) {
	raw := append([]string(nil), input.Recipients...)
	if strings.TrimSpace(input.To) != "" {
		raw = append(raw, strings.FieldsFunc(input.To, func(r rune) bool { return r == ',' || r == ';' || r == '\n' })...)
	}
	if len(raw) == 0 || len(raw) > 100 {
		return nil, ErrInvalidSend
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]Recipient, 0, len(raw))
	for _, value := range raw {
		value = strings.TrimSpace(value)
		parsed, err := mail.ParseAddress(value)
		if err != nil || parsed.Address == "" || strings.ContainsAny(value, "\r\n") {
			return nil, ErrInvalidSend
		}
		address := strings.ToLower(strings.TrimSpace(parsed.Address))
		if _, ok := seen[address]; ok {
			continue
		}
		seen[address] = struct{}{}
		out = append(out, Recipient{Address: address, Kind: "to"})
	}
	if len(out) == 0 {
		return nil, ErrInvalidSend
	}
	return out, nil
}

func providerErrorFields(err error) (string, string) {
	if err == nil {
		return "", ""
	}
	var carrier smtpErrorCarrier
	if errors.As(err, &carrier) {
		return carrier.SMTPStage(), carrier.SMTPCode()
	}
	return "provider", "provider_unavailable"
}

// smtpErrorCarrier is implemented by platform SMTPError through these methods.
type smtpErrorCarrier interface {
	SMTPStage() string
	SMTPCode() string
}

func wait(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func wrapInvalid(err error) error {
	if err != nil {
		return err
	}
	return ErrInvalidAccount
}

func repositoryTenant(scope tenant.Context) string {
	if scope.PlatformAdmin {
		return ""
	}
	return scope.TenantID
}

func repositoryOrg(scope tenant.Context) string {
	if scope.PlatformAdmin {
		return ""
	}
	return scope.Organization
}

func actorFromContext(ctx context.Context) string {
	// The HTTP adapter supplies sender id through context when available; the
	// application remains transport-neutral and uses an explicit fallback.
	if value := ctx.Value(senderContextKey{}); value != nil {
		if id, ok := value.(string); ok {
			return strings.TrimSpace(id)
		}
	}
	return ""
}

type senderContextKey struct{}

func WithSender(ctx context.Context, senderID string) context.Context {
	return context.WithValue(ctx, senderContextKey{}, strings.TrimSpace(senderID))
}

func sanitizeAccount(account Account) Account {
	configured := account.PasswordConfigured || len(account.PasswordCiphertext) > 0
	account.PasswordCiphertext = nil
	account.PasswordConfigured = configured
	return account
}

func cloneRecipients(in []Recipient) []Recipient {
	return append([]Recipient(nil), in...)
}

func newID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func newAttemptID() string {
	id, err := newID()
	if err != nil {
		return fmt.Sprintf("attempt-%d", time.Now().UnixNano())
	}
	return id
}
