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

type Recipient struct {
	Address string `json:"address"`
	Kind    string `json:"kind"`
}

type EmailMessage struct {
	ID                string      `json:"id"`
	TenantID          string      `json:"tenantId"`
	OrgID             string      `json:"orgId,omitempty"`
	SMTPAccountID     string      `json:"smtpAccountId,omitempty"`
	SenderID          string      `json:"senderId,omitempty"`
	Subject           string      `json:"subject"`
	Recipients        []Recipient `json:"recipients"`
	BodyCiphertext    []byte      `json:"-"`
	BodyDigest        string      `json:"bodyDigest"`
	Status            Status      `json:"status"`
	AttemptCount      int         `json:"attemptCount"`
	ProviderMessageID string      `json:"providerMessageId,omitempty"`
	LastErrorCode     string      `json:"lastErrorCode,omitempty"`
	SentAt            *time.Time  `json:"sentAt,omitempty"`
	IdempotencyKey    string      `json:"idempotencyKey,omitempty"`
	CreatedAt         time.Time   `json:"createdAt"`
	UpdatedAt         time.Time   `json:"updatedAt"`
	DeletedAt         *time.Time  `json:"deletedAt,omitempty"`
}

// MessageView is the only representation exposed to HTTP/UI clients. Body is
// populated only for an explicitly authorized detail request.
type MessageView struct {
	ID                string      `json:"id"`
	TenantID          string      `json:"tenantId"`
	OrgID             string      `json:"orgId,omitempty"`
	SMTPAccountID     string      `json:"smtpAccountId,omitempty"`
	SenderID          string      `json:"senderId,omitempty"`
	Subject           string      `json:"subject"`
	Recipients        []Recipient `json:"recipients"`
	Body              string      `json:"body,omitempty"`
	BodyDigest        string      `json:"bodyDigest"`
	Status            Status      `json:"status"`
	AttemptCount      int         `json:"attemptCount"`
	ProviderMessageID string      `json:"providerMessageId,omitempty"`
	LastErrorCode     string      `json:"lastErrorCode,omitempty"`
	SentAt            *time.Time  `json:"sentAt,omitempty"`
	IdempotencyKey    string      `json:"idempotencyKey,omitempty"`
	CreatedAt         time.Time   `json:"createdAt"`
	UpdatedAt         time.Time   `json:"updatedAt"`
}

type SendInput struct {
	Recipients     []string `json:"recipients"`
	To             string   `json:"to,omitempty"`
	Subject        string   `json:"subject"`
	Body           string   `json:"body"`
	IdempotencyKey string   `json:"idempotencyKey,omitempty"`
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

type Service struct {
	accounts  AccountRepository
	messages  MessageRepository
	attempts  AttemptRepository
	provider  Provider
	cipher    Encryptor
	selection appnotification.SMTPSelection
	rng       func(int) int
	clock     func() time.Time
	max       int
	delays    []time.Duration
	cooldown  time.Duration
	sequence  uint64
	mu        sync.Mutex
	cooling   map[string]time.Time
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
	return &Service{accounts: accounts, messages: messages, provider: provider, cipher: cfg.Cipher, selection: selection, rng: cfg.RNG, clock: clock, max: max, delays: delays, cooldown: cfg.Cooldown, cooling: make(map[string]time.Time)}
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
	account := Account{ID: id, TenantID: scope.TenantID, OrgID: scope.Organization, Name: input.Name, Enabled: input.Enabled, Host: input.Host, Port: input.Port, Username: input.Username, Weight: normalizedWeight(input.Weight), FromEmail: input.FromEmail, FromName: input.FromName, ImplicitTLS: input.ImplicitTLS, PasswordConfigured: strings.TrimSpace(input.Password) != "", PasswordCiphertext: ciphertext, CreatedAt: now, UpdatedAt: now}
	created, err := s.accounts.Create(ctx, account)
	if err != nil {
		if errors.Is(err, ErrAccountConflict) {
			return Account{}, err
		}
		return Account{}, fmt.Errorf("%w: create account", ErrRepositoryFailure)
	}
	return sanitizeAccount(created), nil
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

func (s *Service) Send(ctx context.Context, input SendInput) (MessageView, error) {
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
	accounts, err := s.accounts.List(ctx, repositoryTenant(scope), repositoryOrg(scope))
	if err != nil {
		return MessageView{}, fmt.Errorf("%w: list accounts", ErrRepositoryFailure)
	}
	ordered := s.orderAccounts(accounts)
	id, err := newID()
	if err != nil {
		return MessageView{}, err
	}
	ciphertext, err := s.cipher.Encrypt(ctx, "email/message/"+id, []byte(input.Body))
	if err != nil {
		return MessageView{}, fmt.Errorf("encrypt email body: %w", err)
	}
	digest := sha256.Sum256([]byte(input.Body))
	now := s.clock().UTC()
	record := EmailMessage{ID: id, TenantID: scope.TenantID, OrgID: scope.Organization, SenderID: actorFromContext(ctx), Subject: strings.TrimSpace(input.Subject), Recipients: recipients, BodyCiphertext: ciphertext, BodyDigest: hex.EncodeToString(digest[:]), Status: StatusPending, IdempotencyKey: strings.TrimSpace(input.IdempotencyKey), CreatedAt: now, UpdatedAt: now}
	if record.IdempotencyKey != "" {
		if repository, ok := s.messages.(IdempotencyRepository); ok {
			if existing, getErr := repository.GetByIdempotency(ctx, record.TenantID, record.IdempotencyKey); getErr == nil {
				return s.view(ctx, existing, false)
			}
		}
	}
	record, err = s.messages.Create(ctx, record)
	if err != nil {
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
		_, _ = s.messages.Update(ctx, record)
		providerAccount, decryptErr := s.providerAccount(ctx, account)
		if decryptErr != nil {
			last = decryptErr
		} else {
			recipientAddresses := make([]string, 0, len(recipients))
			for _, recipient := range recipients {
				recipientAddresses = append(recipientAddresses, recipient.Address)
			}
			notificationMessage := appnotification.Message{ID: record.ID, To: recipients[0].Address, Recipients: recipientAddresses, Subject: record.Subject, Body: input.Body, CreatedAt: record.CreatedAt}
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
			record.Status, record.SentAt, record.UpdatedAt, record.LastErrorCode = StatusSent, &sentAt, sentAt, ""
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
	record.Status = StatusFailed
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
	return appnotification.SMTPAccount{Enabled: account.Enabled, Name: account.Name, TenantID: account.TenantID, Host: account.Host, Port: account.Port, Username: account.Username, Password: string(password), Weight: account.Weight, FromEmail: account.FromEmail, FromName: account.FromName, ImplicitTLS: account.ImplicitTLS}, nil
}

func (s *Service) view(ctx context.Context, record EmailMessage, includeBody bool) (MessageView, error) {
	view := MessageView{ID: record.ID, TenantID: record.TenantID, OrgID: record.OrgID, SMTPAccountID: record.SMTPAccountID, SenderID: record.SenderID, Subject: record.Subject, Recipients: cloneRecipients(record.Recipients), BodyDigest: record.BodyDigest, Status: record.Status, AttemptCount: record.AttemptCount, ProviderMessageID: record.ProviderMessageID, LastErrorCode: record.LastErrorCode, SentAt: record.SentAt, IdempotencyKey: record.IdempotencyKey, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
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
	enabled := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if account.DeletedAt != nil || !account.Enabled {
			continue
		}
		if account.Weight <= 0 {
			account.Weight = 1
		}
		enabled = append(enabled, account)
	}
	if len(enabled) == 0 {
		return nil
	}
	start := 0
	if s.selection == appnotification.SMTPSelectionRoundRobin {
		start = int(atomic.AddUint64(&s.sequence, 1)-1) % len(enabled)
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
	return ordered
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
