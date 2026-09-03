// Package mailplatform contains the durable SMTP/email adapters.
package mailplatform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	mailapp "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/mail"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormquery"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type GORMAccountRepository struct{ db *gormdb.Store }

type GORMMessageRepository struct{ db *gormdb.Store }

func NewGORMRepository(db *gormdb.Store) *GORMAccountRepository {
	return &GORMAccountRepository{db: db}
}
func NewGORMAccountRepository(db *gormdb.Store) *GORMAccountRepository {
	return &GORMAccountRepository{db: db}
}
func NewGORMMessageRepository(db *gormdb.Store) *GORMMessageRepository {
	return &GORMMessageRepository{db: db}
}

// Mail repositories use the canonical persistence models directly. Nullable
// foreign-key-like IDs stay pointers so a NULL from a fresh schema is decoded
// without lossy zero-value coercion; relation integrity remains application
// owned rather than database-enforced.
type smtpAccountRecord = model.SMTPAccount
type emailMessageRecord = model.EmailMessage
type emailRecipientRecord = model.EmailRecipient
type emailAttemptRecord = model.EmailDeliveryAttempt

func (r *GORMAccountRepository) List(ctx context.Context, tenantID, orgID string) ([]mailapp.Account, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("mail repository is not initialized")
	}
	query := gorm.G[smtpAccountRecord](r.db.Read(ctx)).Scopes(accountScope(tenantID, orgID)).Where("deleted_at IS NULL")
	records, err := query.Order("created_at ASC").Find(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]mailapp.Account, 0, len(records))
	for _, record := range records {
		items = append(items, toAccount(record))
	}
	return items, nil
}

func (r *GORMAccountRepository) Get(ctx context.Context, id string, scope ...string) (mailapp.Account, error) {
	if r == nil || r.db == nil {
		return mailapp.Account{}, errors.New("mail repository is not initialized")
	}
	tenantID, orgID := optionalScope(scope)
	query := gorm.G[smtpAccountRecord](r.db.Read(ctx)).Scopes(accountScope(tenantID, orgID)).Where("id = ? AND deleted_at IS NULL", strings.TrimSpace(id))
	record, err := query.First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return mailapp.Account{}, mailapp.ErrAccountNotFound
		}
		return mailapp.Account{}, err
	}
	return toAccount(record), nil
}

func (r *GORMAccountRepository) Create(ctx context.Context, account mailapp.Account) (mailapp.Account, error) {
	if r == nil || r.db == nil {
		return mailapp.Account{}, errors.New("mail repository is not initialized")
	}
	// Duplicate detection participates in the following write, so pin it to
	// the primary and avoid a replica-lag false negative immediately after an
	// account was created.
	_, duplicate := gorm.G[smtpAccountRecord](r.db.Write(ctx)).Where("tenant_id = ? AND (org_id = ? OR (org_id IS NULL AND ? = '')) AND deleted_at IS NULL AND (account_name = ? OR (host = ? AND port = ? AND username = ?))", account.TenantID, account.OrgID, account.OrgID, account.Name, account.Host, int32(account.Port), account.Username).First(ctx)
	if duplicate == nil {
		return mailapp.Account{}, mailapp.ErrAccountConflict
	}
	if !errors.Is(duplicate, gorm.ErrRecordNotFound) {
		return mailapp.Account{}, duplicate
	}
	record := fromAccount(account)
	if err := createAccount(ctx, r.db.Write(ctx), record); err != nil {
		return mailapp.Account{}, err
	}
	return toAccount(record), nil
}

func (r *GORMAccountRepository) Update(ctx context.Context, account mailapp.Account) (mailapp.Account, error) {
	if r == nil || r.db == nil {
		return mailapp.Account{}, errors.New("mail repository is not initialized")
	}
	record, err := gorm.G[smtpAccountRecord](r.db.Write(ctx)).Scopes(accountScope(account.TenantID, account.OrgID)).Where("id = ? AND deleted_at IS NULL", account.ID).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return mailapp.Account{}, mailapp.ErrAccountNotFound
		}
		return mailapp.Account{}, err
	}
	_, dupErr := gorm.G[smtpAccountRecord](r.db.Write(ctx)).Where("tenant_id = ? AND (org_id = ? OR (org_id IS NULL AND ? = '')) AND deleted_at IS NULL AND id <> ? AND (account_name = ? OR (host = ? AND port = ? AND username = ?))", account.TenantID, account.OrgID, account.OrgID, account.ID, account.Name, account.Host, int32(account.Port), account.Username).First(ctx)
	if dupErr == nil {
		return mailapp.Account{}, mailapp.ErrAccountConflict
	}
	if !errors.Is(dupErr, gorm.ErrRecordNotFound) {
		return mailapp.Account{}, dupErr
	}
	record = fromAccount(account)
	rows, err := gorm.G[smtpAccountRecord](r.db.Write(ctx)).Where("id = ? AND tenant_id = ? AND (org_id = ? OR (org_id IS NULL AND ? = '')) AND deleted_at IS NULL", account.ID, account.TenantID, account.OrgID, account.OrgID).Set(clause.Assignments(map[string]any{"tenant_id": record.TenantID, "org_id": record.OrgID, "scope_type": record.ScopeType, "account_name": record.AccountName, "enabled": record.Enabled, "host": record.Host, "port": record.Port, "username": record.Username, "password_ciphertext": record.PasswordCiphertext, "weight": record.Weight, "from_email": record.FromEmail, "from_name": record.FromName, "implicit_tls": record.ImplicitTLS, "updated_at": record.UpdatedAt})).Update(ctx)
	if err != nil {
		return mailapp.Account{}, err
	}
	if rows == 0 {
		return mailapp.Account{}, mailapp.ErrAccountNotFound
	}
	return toAccount(record), nil
}

func (r *GORMAccountRepository) SoftDelete(ctx context.Context, id, tenantID, orgID string, at time.Time) error {
	if r == nil || r.db == nil {
		return errors.New("mail repository is not initialized")
	}
	rows, err := gorm.G[smtpAccountRecord](r.db.Write(ctx)).Scopes(accountScope(tenantID, orgID)).Where("id = ? AND deleted_at IS NULL", strings.TrimSpace(id)).Set(clause.Assignments(map[string]any{"deleted_at": at, "updated_at": at})).Update(ctx)
	if err != nil {
		return err
	}
	if rows == 0 {
		return mailapp.ErrAccountNotFound
	}
	return nil
}

func (r *GORMMessageRepository) CreateMessage(ctx context.Context, message mailapp.EmailMessage) (mailapp.EmailMessage, error) {
	if r == nil || r.db == nil {
		return mailapp.EmailMessage{}, errors.New("mail repository is not initialized")
	}
	record := fromMessage(message)
	err := r.db.WithinTransaction(ctx, func(tx *gorm.DB) error {
		if err := createMessage(ctx, tx, record); err != nil {
			return err
		}
		for _, recipient := range message.Recipients {
			item := emailRecipientRecord{ID: newRecordID(), MessageID: message.ID, Kind: recipient.Kind, Address: recipient.Address, CreatedAt: message.CreatedAt, UpdatedAt: message.UpdatedAt}
			if err := createRecipient(ctx, tx, item); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return mailapp.EmailMessage{}, err
	}
	return toMessage(record, message.Recipients), nil
}

func (r *GORMMessageRepository) Create(ctx context.Context, message mailapp.EmailMessage) (mailapp.EmailMessage, error) {
	return r.CreateMessage(ctx, message)
}

func (r *GORMMessageRepository) Update(ctx context.Context, message mailapp.EmailMessage) (mailapp.EmailMessage, error) {
	if r == nil || r.db == nil {
		return mailapp.EmailMessage{}, errors.New("mail repository is not initialized")
	}
	record := fromMessage(message)
	rows, err := gorm.G[emailMessageRecord](r.db.Write(ctx)).Where("id = ? AND tenant_id = ? AND (org_id = ? OR (org_id IS NULL AND ? = '')) AND deleted_at IS NULL", message.ID, message.TenantID, message.OrgID, message.OrgID).Set(clause.Assignments(map[string]any{"scope_type": record.ScopeType, "smtp_account_id": record.SMTPAccountID, "sender_id": record.SenderID, "caller_key": record.CallerKey, "template_key": record.TemplateKey, "template_generation": record.TemplateGeneration, "policy_generation": record.PolicyGeneration, "locale": record.Locale, "is_test": record.IsTest, "challenge_id": record.ChallengeID, "relay_status": record.RelayStatus, "subject": record.Subject, "body_ciphertext": record.BodyCiphertext, "body_digest": record.BodyDigest, "status": record.Status, "attempt_count": record.AttemptCount, "provider_message_id": record.ProviderMessageID, "last_error_code": record.LastErrorCode, "sent_at": record.SentAt, "idempotency_key": record.IdempotencyKey, "idempotency_scope_hash": record.IdempotencyScopeHash, "updated_at": record.UpdatedAt})).Update(ctx)
	if err != nil {
		return mailapp.EmailMessage{}, err
	}
	if rows == 0 {
		return mailapp.EmailMessage{}, mailapp.ErrMessageNotFound
	}
	return r.Get(ctx, message.ID, message.TenantID, message.OrgID)
}

func (r *GORMMessageRepository) List(ctx context.Context, tenantID, orgID string, filter mailapp.MessageFilter) (mailapp.MessagePage, error) {
	if r == nil || r.db == nil {
		return mailapp.MessagePage{}, errors.New("mail repository is not initialized")
	}
	query := gorm.G[emailMessageRecord](r.db.Read(ctx)).Scopes(messageScope(tenantID, orgID)).Where("deleted_at IS NULL")
	if strings.TrimSpace(filter.Status) != "" {
		query = query.Where("status = ?", strings.TrimSpace(filter.Status))
	}
	if accountID := strings.TrimSpace(filter.AccountID); accountID != "" {
		query = query.Where("smtp_account_id = ?", accountID)
	}
	if filter.From != nil {
		query = query.Where("created_at >= ?", *filter.From)
	}
	if filter.To != nil {
		query = query.Where("created_at <= ?", *filter.To)
	}
	if callerKey := strings.TrimSpace(filter.CallerKey); callerKey != "" {
		query = query.Where("caller_key = ?", callerKey)
	}
	switch strings.ToLower(strings.TrimSpace(filter.Source)) {
	case "business":
		query = query.Where("is_test = ? AND (caller_key IS NOT NULL OR template_key IS NOT NULL)", false)
	case "template_test", "template-test", "test":
		query = query.Where("is_test = ?", true)
	case "system":
		query = query.Where("is_test = ? AND caller_key IS NULL AND template_key IS NULL", false)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		pattern := "%" + strings.ToLower(keyword) + "%"
		query = query.Where("LOWER(id) LIKE ? OR LOWER(subject) LIKE ? OR LOWER(caller_key) LIKE ? OR LOWER(template_key) LIKE ? OR id IN (SELECT message_id FROM email_recipients WHERE LOWER(address) LIKE ? AND deleted_at IS NULL)", pattern, pattern, pattern, pattern, pattern)
	}
	total, err := query.Count(ctx, "*")
	if err != nil {
		return mailapp.MessagePage{}, err
	}
	limit := filter.Limit
	if limit < 0 || filter.Offset < 0 {
		return mailapp.MessagePage{}, mailapp.ErrInvalidSend
	}
	if limit == 0 {
		limit = 50
	}
	records, err := query.Order("created_at DESC").Limit(limit).Offset(filter.Offset).Find(ctx)
	if err != nil {
		return mailapp.MessagePage{}, err
	}
	items := make([]mailapp.MessageView, 0, len(records))
	for _, record := range records {
		recipients, err := r.recipients(ctx, record.ID)
		if err != nil {
			return mailapp.MessagePage{}, err
		}
		view, _ := messageView(toMessage(record, recipients))
		items = append(items, view)
	}
	return mailapp.MessagePage{Items: items, Total: int(total), Limit: limit, Offset: filter.Offset}, nil
}

func (r *GORMMessageRepository) Get(ctx context.Context, id string, scope ...string) (mailapp.EmailMessage, error) {
	if r == nil || r.db == nil {
		return mailapp.EmailMessage{}, errors.New("mail repository is not initialized")
	}
	tenantID, orgID := optionalScope(scope)
	record, err := gorm.G[emailMessageRecord](r.db.Read(ctx)).Scopes(messageScope(tenantID, orgID)).Where("id = ? AND deleted_at IS NULL", strings.TrimSpace(id)).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return mailapp.EmailMessage{}, mailapp.ErrMessageNotFound
		}
		return mailapp.EmailMessage{}, err
	}
	recipients, err := r.recipients(ctx, record.ID)
	if err != nil {
		return mailapp.EmailMessage{}, err
	}
	return toMessage(record, recipients), nil
}

func (r *GORMMessageRepository) GetByIdempotency(ctx context.Context, tenantID, key string) (mailapp.EmailMessage, error) {
	if r == nil || r.db == nil {
		return mailapp.EmailMessage{}, mailapp.ErrRepositoryFailure
	}
	record, err := gorm.G[emailMessageRecord](r.db.Read(ctx)).Scopes(messageScope(tenantID, "")).Where("idempotency_key = ? AND deleted_at IS NULL", key).First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return mailapp.EmailMessage{}, mailapp.ErrMessageNotFound
		}
		return mailapp.EmailMessage{}, err
	}
	recipients, err := r.recipients(ctx, record.ID)
	if err != nil {
		return mailapp.EmailMessage{}, err
	}
	return toMessage(record, recipients), nil
}

func (r *GORMMessageRepository) GetByIdempotencyScope(ctx context.Context, tenantID, orgID, caller, template, key string) (mailapp.EmailMessage, error) {
	if r == nil || r.db == nil {
		return mailapp.EmailMessage{}, mailapp.ErrRepositoryFailure
	}
	query := gorm.G[emailMessageRecord](r.db.Read(ctx)).Scopes(messageScope(tenantID, orgID)).Where(
		"idempotency_key = ? AND COALESCE(caller_key, '') = ? AND COALESCE(template_key, '') = ? AND deleted_at IS NULL",
		strings.TrimSpace(key), strings.TrimSpace(caller), strings.TrimSpace(template),
	)
	record, err := query.First(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return mailapp.EmailMessage{}, mailapp.ErrMessageNotFound
		}
		return mailapp.EmailMessage{}, err
	}
	recipients, err := r.recipients(ctx, record.ID)
	if err != nil {
		return mailapp.EmailMessage{}, err
	}
	return toMessage(record, recipients), nil
}

func (r *GORMMessageRepository) recipients(ctx context.Context, messageID string) ([]mailapp.Recipient, error) {
	records, err := gorm.G[emailRecipientRecord](r.db.Read(ctx)).Where("message_id = ? AND deleted_at IS NULL", messageID).Order("created_at ASC").Find(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]mailapp.Recipient, 0, len(records))
	for _, record := range records {
		items = append(items, mailapp.Recipient{Address: record.Address, Kind: record.Kind})
	}
	return items, nil
}

func (r *GORMMessageRepository) Append(ctx context.Context, attempt mailapp.Attempt) error {
	if r == nil || r.db == nil {
		return errors.New("mail repository is not initialized")
	}
	record := emailAttemptRecord{ID: attempt.ID, MessageID: attempt.MessageID, AccountID: attempt.AccountID, AttemptNo: int32(attempt.AttemptNo), Stage: attempt.Stage, Code: attempt.Code, CreatedAt: attempt.CreatedAt, UpdatedAt: attempt.CreatedAt}
	return createAttempt(ctx, r.db.Write(ctx), record)
}

// The canonical mail models retain database defaults for fresh-install DDL.
// Runtime inputs, however, carry explicit zero values (notably Enabled=false
// and Weight=0), so use the typed assignment-create helper to avoid GORM
// replacing those values with the model default during the Create callback.
func createAccount(ctx context.Context, db *gorm.DB, record smtpAccountRecord) error {
	return gormquery.CreateValues[smtpAccountRecord](ctx, db, map[string]any{
		"id": record.ID, "tenant_id": record.TenantID, "org_id": record.OrgID,
		"scope_type":   record.ScopeType,
		"account_name": record.AccountName, "enabled": record.Enabled, "host": record.Host,
		"port": record.Port, "username": record.Username, "password_ciphertext": record.PasswordCiphertext,
		"weight": record.Weight, "from_email": record.FromEmail, "from_name": record.FromName,
		"implicit_tls": record.ImplicitTLS, "created_at": record.CreatedAt, "updated_at": record.UpdatedAt,
		"deleted_at": record.DeletedAt,
	})
}

func createMessage(ctx context.Context, db *gorm.DB, record emailMessageRecord) error {
	return gormquery.CreateValues[emailMessageRecord](ctx, db, map[string]any{
		"id": record.ID, "tenant_id": record.TenantID, "org_id": record.OrgID,
		"scope_type": record.ScopeType, "caller_key": record.CallerKey, "template_key": record.TemplateKey,
		"template_generation": record.TemplateGeneration, "policy_generation": record.PolicyGeneration,
		"locale": record.Locale, "is_test": record.IsTest, "challenge_id": record.ChallengeID,
		"relay_status":    record.RelayStatus,
		"smtp_account_id": record.SMTPAccountID, "sender_id": record.SenderID, "subject": record.Subject,
		"body_ciphertext": record.BodyCiphertext, "body_digest": record.BodyDigest, "status": record.Status,
		"attempt_count": record.AttemptCount, "provider_message_id": record.ProviderMessageID,
		"last_error_code": record.LastErrorCode, "sent_at": record.SentAt, "idempotency_key": record.IdempotencyKey, "idempotency_scope_hash": record.IdempotencyScopeHash,
		"created_at": record.CreatedAt, "updated_at": record.UpdatedAt, "deleted_at": record.DeletedAt,
	})
}

func createRecipient(ctx context.Context, db *gorm.DB, record emailRecipientRecord) error {
	return gormquery.CreateValues[emailRecipientRecord](ctx, db, map[string]any{
		"id": record.ID, "message_id": record.MessageID, "kind": record.Kind, "address": record.Address,
		"created_at": record.CreatedAt, "updated_at": record.UpdatedAt, "deleted_at": record.DeletedAt,
	})
}

func createAttempt(ctx context.Context, db *gorm.DB, record emailAttemptRecord) error {
	return gormquery.CreateValues[emailAttemptRecord](ctx, db, map[string]any{
		"id": record.ID, "message_id": record.MessageID, "account_id": record.AccountID,
		"attempt_no": record.AttemptNo, "stage": record.Stage, "code": record.Code,
		"created_at": record.CreatedAt, "updated_at": record.UpdatedAt, "deleted_at": record.DeletedAt,
	})
}

func accountScope(tenantID, orgID string) func(*gorm.Statement) {
	return func(stmt *gorm.Statement) {
		if strings.TrimSpace(tenantID) != "" {
			stmt.AddClause(clause.Where{Exprs: []clause.Expression{clause.Eq{Column: clause.Column{Name: "tenant_id"}, Value: tenantID}}})
		}
		if strings.TrimSpace(orgID) != "" {
			stmt.AddClause(clause.Where{Exprs: []clause.Expression{clause.Eq{Column: clause.Column{Name: "org_id"}, Value: orgID}}})
		}
	}
}

func messageScope(tenantID, orgID string) func(*gorm.Statement) {
	return accountScope(tenantID, orgID)
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

func toAccount(record smtpAccountRecord) mailapp.Account {
	password := binaryBytes(record.PasswordCiphertext)
	return mailapp.Account{ID: record.ID, TenantID: record.TenantID, OrgID: stringValue(record.OrgID), ScopeType: record.ScopeType, Name: record.AccountName, Enabled: record.Enabled, Host: record.Host, Port: int(record.Port), Username: record.Username, PasswordConfigured: len(password) > 0, PasswordCiphertext: password, Weight: int(record.Weight), FromEmail: record.FromEmail, FromName: record.FromName, ImplicitTLS: record.ImplicitTLS, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, DeletedAt: record.DeletedAt}
}

func fromAccount(account mailapp.Account) smtpAccountRecord {
	return smtpAccountRecord{ID: account.ID, TenantID: account.TenantID, OrgID: stringPtrIfNonEmpty(account.OrgID), ScopeType: account.ScopeType, AccountName: account.Name, Enabled: account.Enabled, Host: account.Host, Port: int32(account.Port), Username: account.Username, PasswordCiphertext: binaryPtr(account.PasswordCiphertext), Weight: int32(account.Weight), FromEmail: account.FromEmail, FromName: account.FromName, ImplicitTLS: account.ImplicitTLS, CreatedAt: account.CreatedAt, UpdatedAt: account.UpdatedAt, DeletedAt: account.DeletedAt}
}

func toMessage(record emailMessageRecord, recipients []mailapp.Recipient) mailapp.EmailMessage {
	return mailapp.EmailMessage{ID: record.ID, TenantID: record.TenantID, OrgID: stringValue(record.OrgID), ScopeType: record.ScopeType, SMTPAccountID: stringValue(record.SMTPAccountID), SenderID: stringValue(record.SenderID), CallerKey: stringValue(record.CallerKey), TemplateKey: stringValue(record.TemplateKey), TemplateGeneration: uint64String(record.TemplateGeneration), PolicyGeneration: stringValue(record.PolicyGeneration), Locale: stringValue(record.Locale), IsTest: record.IsTest, ChallengeID: stringValue(record.ChallengeID), RelayStatus: record.RelayStatus, Subject: record.Subject, Recipients: append([]mailapp.Recipient(nil), recipients...), BodyCiphertext: append([]byte(nil), record.BodyCiphertext...), BodyDigest: record.BodyDigest, Status: mailapp.Status(record.Status), AttemptCount: int(record.AttemptCount), ProviderMessageID: stringValue(record.ProviderMessageID), LastErrorCode: stringValue(record.LastErrorCode), SentAt: record.SentAt, IdempotencyKey: stringValue(record.IdempotencyKey), IdempotencyScopeHash: stringValue(record.IdempotencyScopeHash), CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, DeletedAt: record.DeletedAt}
}

func fromMessage(message mailapp.EmailMessage) emailMessageRecord {
	return emailMessageRecord{ID: message.ID, TenantID: message.TenantID, OrgID: stringPtrIfNonEmpty(message.OrgID), ScopeType: message.ScopeType, SMTPAccountID: stringPtrIfNonEmpty(message.SMTPAccountID), SenderID: stringPtrIfNonEmpty(message.SenderID), CallerKey: stringPtrIfNonEmpty(message.CallerKey), TemplateKey: stringPtrIfNonEmpty(message.TemplateKey), TemplateGeneration: parseUint64Ptr(message.TemplateGeneration), PolicyGeneration: stringPtrIfNonEmpty(message.PolicyGeneration), Locale: stringPtrIfNonEmpty(message.Locale), IsTest: message.IsTest, ChallengeID: stringPtrIfNonEmpty(message.ChallengeID), RelayStatus: message.RelayStatus, Subject: message.Subject, BodyCiphertext: model.BinaryValue(append([]byte(nil), message.BodyCiphertext...)), BodyDigest: message.BodyDigest, Status: string(message.Status), AttemptCount: int32(message.AttemptCount), ProviderMessageID: stringPtrIfNonEmpty(message.ProviderMessageID), LastErrorCode: stringPtrIfNonEmpty(message.LastErrorCode), SentAt: message.SentAt, IdempotencyKey: stringPtrIfNonEmpty(message.IdempotencyKey), IdempotencyScopeHash: stringPtrIfNonEmpty(message.IdempotencyScopeHash), CreatedAt: message.CreatedAt, UpdatedAt: message.UpdatedAt, DeletedAt: message.DeletedAt}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringPtrIfNonEmpty(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func binaryBytes(value *model.BinaryValue) []byte {
	if value == nil {
		return nil
	}
	return append([]byte(nil), (*value)...)
}

func binaryPtr(value []byte) *model.BinaryValue {
	if len(value) == 0 {
		return nil
	}
	copyValue := model.BinaryValue(append([]byte(nil), value...))
	return &copyValue
}

func uint64String(value *uint64) string {
	if value == nil {
		return ""
	}
	// The application/runtime representation is deliberately opaque and uses
	// the `g-` prefix. Preserve it when loading a durable record so an
	// idempotent retry compares the same value that was originally sent.
	return "g-" + strconv.FormatUint(*value, 10)
}

func parseUint64Ptr(value string) *uint64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, "g-"), 10, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

func messageView(message mailapp.EmailMessage) (mailapp.MessageView, error) {
	return mailapp.MessageView{ID: message.ID, TenantID: message.TenantID, OrgID: message.OrgID, ScopeType: message.ScopeType, SMTPAccountID: message.SMTPAccountID, SenderID: message.SenderID, CallerKey: message.CallerKey, TemplateKey: message.TemplateKey, TemplateGeneration: message.TemplateGeneration, PolicyGeneration: message.PolicyGeneration, Locale: message.Locale, IsTest: message.IsTest, ChallengeID: message.ChallengeID, RelayStatus: message.RelayStatus, Subject: message.Subject, Recipients: append([]mailapp.Recipient(nil), message.Recipients...), BodyDigest: message.BodyDigest, Status: message.Status, AttemptCount: message.AttemptCount, ProviderMessageID: message.ProviderMessageID, LastErrorCode: message.LastErrorCode, SentAt: message.SentAt, IdempotencyKey: message.IdempotencyKey, CreatedAt: message.CreatedAt, UpdatedAt: message.UpdatedAt}, nil
}

func newRecordID() string {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err == nil {
		return hex.EncodeToString(bytes[:])
	}
	// Entropy failure is exceptionally rare; a nanosecond fallback still keeps
	// the local adapter moving while the application-level ID remains opaque.
	return time.Now().UTC().Format("20060102150405.000000000")
}

var _ mailapp.AccountRepository = (*GORMAccountRepository)(nil)
var _ mailapp.MessageRepository = (*GORMMessageRepository)(nil)
var _ mailapp.IdempotencyRepository = (*GORMMessageRepository)(nil)
var _ mailapp.ScopedIdempotencyRepository = (*GORMMessageRepository)(nil)
var _ mailapp.AttemptRepository = (*GORMMessageRepository)(nil)
