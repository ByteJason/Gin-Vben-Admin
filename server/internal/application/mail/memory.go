package mail

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemoryAccountRepository is the deterministic local/contract repository. It
// is intentionally tenant-aware and mirrors the unique indexes in migrations.
type MemoryAccountRepository struct {
	mu    sync.RWMutex
	items map[string]Account
}

func NewMemoryAccountRepository() *MemoryAccountRepository {
	return &MemoryAccountRepository{items: make(map[string]Account)}
}

func (r *MemoryAccountRepository) List(ctx context.Context, tenantID, orgID string) ([]Account, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]Account, 0, len(r.items))
	for _, item := range r.items {
		if item.DeletedAt != nil || !matchesScope(item.TenantID, item.OrgID, tenantID, orgID) {
			continue
		}
		items = append(items, cloneAccount(item))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	return items, nil
}

func (r *MemoryAccountRepository) Get(ctx context.Context, id string, scope ...string) (Account, error) {
	if err := contextErr(ctx); err != nil {
		return Account{}, err
	}
	tenantID, orgID := optionalScope(scope)
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.items[strings.TrimSpace(id)]
	if !ok || item.DeletedAt != nil || !matchesScope(item.TenantID, item.OrgID, tenantID, orgID) {
		return Account{}, ErrAccountNotFound
	}
	return cloneAccount(item), nil
}

func (r *MemoryAccountRepository) Create(ctx context.Context, account Account) (Account, error) {
	if err := contextErr(ctx); err != nil {
		return Account{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range r.items {
		if item.DeletedAt != nil || item.TenantID != account.TenantID {
			continue
		}
		if strings.EqualFold(item.Name, account.Name) || (strings.EqualFold(item.Host, account.Host) && item.Port == account.Port && strings.EqualFold(item.Username, account.Username)) {
			return Account{}, ErrAccountConflict
		}
	}
	r.items[account.ID] = cloneAccount(account)
	return cloneAccount(account), nil
}

func (r *MemoryAccountRepository) Update(ctx context.Context, account Account) (Account, error) {
	if err := contextErr(ctx); err != nil {
		return Account{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[account.ID]; !ok {
		return Account{}, ErrAccountNotFound
	}
	for id, item := range r.items {
		if id == account.ID || item.DeletedAt != nil || item.TenantID != account.TenantID {
			continue
		}
		if strings.EqualFold(item.Name, account.Name) || (strings.EqualFold(item.Host, account.Host) && item.Port == account.Port && strings.EqualFold(item.Username, account.Username)) {
			return Account{}, ErrAccountConflict
		}
	}
	r.items[account.ID] = cloneAccount(account)
	return cloneAccount(account), nil
}

func (r *MemoryAccountRepository) SoftDelete(ctx context.Context, id, tenantID, orgID string, at time.Time) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.items[strings.TrimSpace(id)]
	if !ok || item.DeletedAt != nil || !matchesScope(item.TenantID, item.OrgID, tenantID, orgID) {
		return ErrAccountNotFound
	}
	item.DeletedAt = &at
	item.UpdatedAt = at
	r.items[item.ID] = item
	return nil
}

type MemoryMessageRepository struct {
	mu    sync.RWMutex
	items map[string]EmailMessage
}

func NewMemoryMessageRepository() *MemoryMessageRepository {
	return &MemoryMessageRepository{items: make(map[string]EmailMessage)}
}

func (r *MemoryMessageRepository) Create(ctx context.Context, message EmailMessage) (EmailMessage, error) {
	if err := contextErr(ctx); err != nil {
		return EmailMessage{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[message.ID]; exists {
		return EmailMessage{}, errors.New("email message id conflict")
	}
	if message.IdempotencyKey != "" {
		for _, existing := range r.items {
			if existing.DeletedAt == nil && existing.TenantID == message.TenantID && existing.IdempotencyKey == message.IdempotencyKey {
				return EmailMessage{}, errors.New("email message idempotency conflict")
			}
		}
	}
	r.items[message.ID] = cloneMessage(message)
	return cloneMessage(message), nil
}

func (r *MemoryMessageRepository) Update(ctx context.Context, message EmailMessage) (EmailMessage, error) {
	if err := contextErr(ctx); err != nil {
		return EmailMessage{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[message.ID]; !ok {
		return EmailMessage{}, ErrMessageNotFound
	}
	r.items[message.ID] = cloneMessage(message)
	return cloneMessage(message), nil
}

func (r *MemoryMessageRepository) List(ctx context.Context, tenantID, orgID string, filter MessageFilter) (MessagePage, error) {
	if err := contextErr(ctx); err != nil {
		return MessagePage{}, err
	}
	if filter.Limit < 0 || filter.Offset < 0 {
		return MessagePage{}, ErrInvalidSend
	}
	r.mu.RLock()
	items := make([]EmailMessage, 0, len(r.items))
	for _, item := range r.items {
		if item.DeletedAt != nil || !matchesScope(item.TenantID, item.OrgID, tenantID, orgID) || (filter.Status != "" && string(item.Status) != filter.Status) {
			continue
		}
		items = append(items, cloneMessage(item))
	}
	r.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	total := len(items)
	start := filter.Offset
	if start > total {
		start = total
	}
	end := total
	if filter.Limit > 0 && start+filter.Limit < end {
		end = start + filter.Limit
	}
	views := make([]MessageView, 0, end-start)
	for _, item := range items[start:end] {
		view, _ := messageView(item)
		views = append(views, view)
	}
	return MessagePage{Items: views, Total: total, Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (r *MemoryMessageRepository) Get(ctx context.Context, id string, scope ...string) (EmailMessage, error) {
	if err := contextErr(ctx); err != nil {
		return EmailMessage{}, err
	}
	tenantID, orgID := optionalScope(scope)
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.items[strings.TrimSpace(id)]
	if !ok || item.DeletedAt != nil || !matchesScope(item.TenantID, item.OrgID, tenantID, orgID) {
		return EmailMessage{}, ErrMessageNotFound
	}
	return cloneMessage(item), nil
}

func (r *MemoryMessageRepository) GetByIdempotency(ctx context.Context, tenantID, key string) (EmailMessage, error) {
	if err := contextErr(ctx); err != nil {
		return EmailMessage{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, item := range r.items {
		if item.DeletedAt == nil && item.TenantID == tenantID && item.IdempotencyKey == key {
			return cloneMessage(item), nil
		}
	}
	return EmailMessage{}, ErrMessageNotFound
}

type MemoryAttemptRepository struct {
	mu    sync.Mutex
	items []Attempt
}

func NewMemoryAttemptRepository() *MemoryAttemptRepository { return &MemoryAttemptRepository{} }

func (r *MemoryAttemptRepository) Append(ctx context.Context, attempt Attempt) error {
	if err := contextErr(ctx); err != nil {
		return err
	}
	r.mu.Lock()
	r.items = append(r.items, attempt)
	r.mu.Unlock()
	return nil
}

func (r *MemoryAttemptRepository) Items() []Attempt {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Attempt(nil), r.items...)
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return context.Canceled
	}
	return ctx.Err()
}

func matchesScope(resourceTenant, resourceOrg, tenantID, orgID string) bool {
	if tenantID != "" && resourceTenant != tenantID {
		return false
	}
	return orgID == "" || resourceOrg == orgID
}

func optionalScope(scope []string) (string, string) {
	var tenantID, orgID string
	if len(scope) > 0 {
		tenantID = scope[0]
	}
	if len(scope) > 1 {
		orgID = scope[1]
	}
	return tenantID, orgID
}

func cloneAccount(item Account) Account {
	item.PasswordCiphertext = append([]byte(nil), item.PasswordCiphertext...)
	return item
}

func cloneMessage(item EmailMessage) EmailMessage {
	item.BodyCiphertext = append([]byte(nil), item.BodyCiphertext...)
	item.Recipients = cloneRecipients(item.Recipients)
	return item
}

func messageView(item EmailMessage) (MessageView, error) {
	return MessageView{ID: item.ID, TenantID: item.TenantID, OrgID: item.OrgID, SMTPAccountID: item.SMTPAccountID, SenderID: item.SenderID, Subject: item.Subject, Recipients: cloneRecipients(item.Recipients), BodyDigest: item.BodyDigest, Status: item.Status, AttemptCount: item.AttemptCount, ProviderMessageID: item.ProviderMessageID, LastErrorCode: item.LastErrorCode, SentAt: item.SentAt, IdempotencyKey: item.IdempotencyKey, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}, nil
}

var _ AccountRepository = (*MemoryAccountRepository)(nil)
var _ MessageRepository = (*MemoryMessageRepository)(nil)
var _ IdempotencyRepository = (*MemoryMessageRepository)(nil)
var _ AttemptRepository = (*MemoryAttemptRepository)(nil)
