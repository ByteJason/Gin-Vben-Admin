// Package mailplatform contains the durable SMTP/email adapters.
package mailplatform

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	mailapp "example.com/gin-vben-admin/server/internal/application/mail"
	"example.com/gin-vben-admin/server/internal/platform/persistence/gormdb"
	"gorm.io/gorm"
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

type smtpAccountRecord struct {
	ID                 string     `gorm:"column:id;primaryKey"`
	TenantID           string     `gorm:"column:tenant_id"`
	OrgID              string     `gorm:"column:org_id"`
	AccountName        string     `gorm:"column:account_name"`
	Enabled            bool       `gorm:"column:enabled"`
	Host               string     `gorm:"column:host"`
	Port               int        `gorm:"column:port"`
	Username           string     `gorm:"column:username"`
	PasswordCiphertext []byte     `gorm:"column:password_ciphertext"`
	Weight             int        `gorm:"column:weight"`
	FromEmail          string     `gorm:"column:from_email"`
	FromName           string     `gorm:"column:from_name"`
	ImplicitTLS        bool       `gorm:"column:implicit_tls"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at"`
	DeletedAt          *time.Time `gorm:"column:deleted_at"`
}

func (smtpAccountRecord) TableName() string { return "smtp_accounts" }

type emailMessageRecord struct {
	ID                string     `gorm:"column:id;primaryKey"`
	TenantID          string     `gorm:"column:tenant_id"`
	OrgID             string     `gorm:"column:org_id"`
	SMTPAccountID     string     `gorm:"column:smtp_account_id"`
	SenderID          string     `gorm:"column:sender_id"`
	Subject           string     `gorm:"column:subject"`
	BodyCiphertext    []byte     `gorm:"column:body_ciphertext"`
	BodyDigest        string     `gorm:"column:body_digest"`
	Status            string     `gorm:"column:status"`
	AttemptCount      int        `gorm:"column:attempt_count"`
	ProviderMessageID string     `gorm:"column:provider_message_id"`
	LastErrorCode     string     `gorm:"column:last_error_code"`
	SentAt            *time.Time `gorm:"column:sent_at"`
	IdempotencyKey    string     `gorm:"column:idempotency_key"`
	CreatedAt         time.Time  `gorm:"column:created_at"`
	UpdatedAt         time.Time  `gorm:"column:updated_at"`
	DeletedAt         *time.Time `gorm:"column:deleted_at"`
}

func (emailMessageRecord) TableName() string { return "email_messages" }

type emailRecipientRecord struct {
	ID        string     `gorm:"column:id;primaryKey"`
	MessageID string     `gorm:"column:message_id"`
	Kind      string     `gorm:"column:kind"`
	Address   string     `gorm:"column:address"`
	CreatedAt time.Time  `gorm:"column:created_at"`
	UpdatedAt time.Time  `gorm:"column:updated_at"`
	DeletedAt *time.Time `gorm:"column:deleted_at"`
}

func (emailRecipientRecord) TableName() string { return "email_recipients" }

type emailAttemptRecord struct {
	ID        string     `gorm:"column:id;primaryKey"`
	MessageID string     `gorm:"column:message_id"`
	AccountID string     `gorm:"column:account_id"`
	AttemptNo int        `gorm:"column:attempt_no"`
	Stage     string     `gorm:"column:stage"`
	Code      string     `gorm:"column:code"`
	CreatedAt time.Time  `gorm:"column:created_at"`
	UpdatedAt time.Time  `gorm:"column:updated_at"`
	DeletedAt *time.Time `gorm:"column:deleted_at"`
}

func (emailAttemptRecord) TableName() string { return "email_delivery_attempts" }

func (r *GORMAccountRepository) List(ctx context.Context, tenantID, orgID string) ([]mailapp.Account, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("mail repository is not initialized")
	}
	query := r.db.Read(ctx).Where("deleted_at IS NULL")
	query = accountScope(query, tenantID, orgID)
	var records []smtpAccountRecord
	if err := query.Order("created_at ASC").Find(&records).Error; err != nil {
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
	var record smtpAccountRecord
	query := accountScope(r.db.Read(ctx), tenantID, orgID).Where("id = ? AND deleted_at IS NULL", strings.TrimSpace(id))
	if err := query.First(&record).Error; err != nil {
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
	var existing smtpAccountRecord
	duplicate := r.db.Read(ctx).Where("tenant_id = ? AND deleted_at IS NULL AND (account_name = ? OR (host = ? AND port = ? AND username = ?))", account.TenantID, account.Name, account.Host, account.Port, account.Username).First(&existing).Error
	if duplicate == nil {
		return mailapp.Account{}, mailapp.ErrAccountConflict
	}
	if !errors.Is(duplicate, gorm.ErrRecordNotFound) {
		return mailapp.Account{}, duplicate
	}
	record := fromAccount(account)
	if err := r.db.Write(ctx).Create(&record).Error; err != nil {
		return mailapp.Account{}, err
	}
	return toAccount(record), nil
}

func (r *GORMAccountRepository) Update(ctx context.Context, account mailapp.Account) (mailapp.Account, error) {
	if r == nil || r.db == nil {
		return mailapp.Account{}, errors.New("mail repository is not initialized")
	}
	var record smtpAccountRecord
	if err := accountScope(r.db.Write(ctx), account.TenantID, account.OrgID).Where("id = ? AND deleted_at IS NULL", account.ID).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return mailapp.Account{}, mailapp.ErrAccountNotFound
		}
		return mailapp.Account{}, err
	}
	var duplicate smtpAccountRecord
	dupErr := r.db.Read(ctx).Where("tenant_id = ? AND deleted_at IS NULL AND id <> ? AND (account_name = ? OR (host = ? AND port = ? AND username = ?))", account.TenantID, account.ID, account.Name, account.Host, account.Port, account.Username).First(&duplicate).Error
	if dupErr == nil {
		return mailapp.Account{}, mailapp.ErrAccountConflict
	}
	if !errors.Is(dupErr, gorm.ErrRecordNotFound) {
		return mailapp.Account{}, dupErr
	}
	record = fromAccount(account)
	if err := r.db.Write(ctx).Model(&smtpAccountRecord{}).Where("id = ?", account.ID).Updates(map[string]any{"tenant_id": record.TenantID, "org_id": record.OrgID, "account_name": record.AccountName, "enabled": record.Enabled, "host": record.Host, "port": record.Port, "username": record.Username, "password_ciphertext": record.PasswordCiphertext, "weight": record.Weight, "from_email": record.FromEmail, "from_name": record.FromName, "implicit_tls": record.ImplicitTLS, "updated_at": record.UpdatedAt}).Error; err != nil {
		return mailapp.Account{}, err
	}
	return toAccount(record), nil
}

func (r *GORMAccountRepository) SoftDelete(ctx context.Context, id, tenantID, orgID string, at time.Time) error {
	if r == nil || r.db == nil {
		return errors.New("mail repository is not initialized")
	}
	result := accountScope(r.db.Write(ctx), tenantID, orgID).Where("id = ? AND deleted_at IS NULL", strings.TrimSpace(id)).Model(&smtpAccountRecord{}).Updates(map[string]any{"deleted_at": at, "updated_at": at})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
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
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		for _, recipient := range message.Recipients {
			item := emailRecipientRecord{ID: newRecordID(), MessageID: message.ID, Kind: recipient.Kind, Address: recipient.Address, CreatedAt: message.CreatedAt, UpdatedAt: message.UpdatedAt}
			if err := tx.Create(&item).Error; err != nil {
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
	if err := r.db.Write(ctx).Model(&emailMessageRecord{}).Where("id = ? AND tenant_id = ? AND deleted_at IS NULL", message.ID, message.TenantID).Updates(map[string]any{"smtp_account_id": record.SMTPAccountID, "sender_id": record.SenderID, "subject": record.Subject, "body_ciphertext": record.BodyCiphertext, "body_digest": record.BodyDigest, "status": record.Status, "attempt_count": record.AttemptCount, "provider_message_id": record.ProviderMessageID, "last_error_code": record.LastErrorCode, "sent_at": record.SentAt, "idempotency_key": record.IdempotencyKey, "updated_at": record.UpdatedAt}).Error; err != nil {
		return mailapp.EmailMessage{}, err
	}
	return r.Get(ctx, message.ID, message.TenantID, message.OrgID)
}

func (r *GORMMessageRepository) List(ctx context.Context, tenantID, orgID string, filter mailapp.MessageFilter) (mailapp.MessagePage, error) {
	if r == nil || r.db == nil {
		return mailapp.MessagePage{}, errors.New("mail repository is not initialized")
	}
	query := messageScope(r.db.Read(ctx), tenantID, orgID).Where("deleted_at IS NULL")
	if strings.TrimSpace(filter.Status) != "" {
		query = query.Where("status = ?", strings.TrimSpace(filter.Status))
	}
	var total int64
	if err := query.Model(&emailMessageRecord{}).Count(&total).Error; err != nil {
		return mailapp.MessagePage{}, err
	}
	limit := filter.Limit
	if limit < 0 || filter.Offset < 0 {
		return mailapp.MessagePage{}, mailapp.ErrInvalidSend
	}
	if limit == 0 {
		limit = 50
	}
	var records []emailMessageRecord
	if err := query.Order("created_at DESC").Limit(limit).Offset(filter.Offset).Find(&records).Error; err != nil {
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
	return mailapp.MessagePage{Items: items, Total: int(total), Limit: filter.Limit, Offset: filter.Offset}, nil
}

func (r *GORMMessageRepository) Get(ctx context.Context, id string, scope ...string) (mailapp.EmailMessage, error) {
	if r == nil || r.db == nil {
		return mailapp.EmailMessage{}, errors.New("mail repository is not initialized")
	}
	tenantID, orgID := optionalScope(scope)
	var record emailMessageRecord
	if err := messageScope(r.db.Read(ctx), tenantID, orgID).Where("id = ? AND deleted_at IS NULL", strings.TrimSpace(id)).First(&record).Error; err != nil {
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
	var record emailMessageRecord
	if err := messageScope(r.db.Read(ctx), tenantID, "").Where("idempotency_key = ? AND deleted_at IS NULL", key).First(&record).Error; err != nil {
		return mailapp.EmailMessage{}, mailapp.ErrMessageNotFound
	}
	recipients, err := r.recipients(ctx, record.ID)
	if err != nil {
		return mailapp.EmailMessage{}, err
	}
	return toMessage(record, recipients), nil
}

func (r *GORMMessageRepository) recipients(ctx context.Context, messageID string) ([]mailapp.Recipient, error) {
	var records []emailRecipientRecord
	if err := r.db.Read(ctx).Where("message_id = ? AND deleted_at IS NULL", messageID).Order("created_at ASC").Find(&records).Error; err != nil {
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
	record := emailAttemptRecord{ID: attempt.ID, MessageID: attempt.MessageID, AccountID: attempt.AccountID, AttemptNo: attempt.AttemptNo, Stage: attempt.Stage, Code: attempt.Code, CreatedAt: attempt.CreatedAt, UpdatedAt: attempt.CreatedAt}
	return r.db.Write(ctx).Create(&record).Error
}

func accountScope(query *gorm.DB, tenantID, orgID string) *gorm.DB {
	if strings.TrimSpace(tenantID) != "" {
		query = query.Where("tenant_id = ?", tenantID)
	}
	if strings.TrimSpace(orgID) != "" {
		query = query.Where("org_id = ?", orgID)
	}
	return query
}

func messageScope(query *gorm.DB, tenantID, orgID string) *gorm.DB {
	return accountScope(query, tenantID, orgID)
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
	return mailapp.Account{ID: record.ID, TenantID: record.TenantID, OrgID: record.OrgID, Name: record.AccountName, Enabled: record.Enabled, Host: record.Host, Port: record.Port, Username: record.Username, PasswordConfigured: len(record.PasswordCiphertext) > 0, PasswordCiphertext: append([]byte(nil), record.PasswordCiphertext...), Weight: record.Weight, FromEmail: record.FromEmail, FromName: record.FromName, ImplicitTLS: record.ImplicitTLS, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, DeletedAt: record.DeletedAt}
}

func fromAccount(account mailapp.Account) smtpAccountRecord {
	return smtpAccountRecord{ID: account.ID, TenantID: account.TenantID, OrgID: account.OrgID, AccountName: account.Name, Enabled: account.Enabled, Host: account.Host, Port: account.Port, Username: account.Username, PasswordCiphertext: append([]byte(nil), account.PasswordCiphertext...), Weight: account.Weight, FromEmail: account.FromEmail, FromName: account.FromName, ImplicitTLS: account.ImplicitTLS, CreatedAt: account.CreatedAt, UpdatedAt: account.UpdatedAt, DeletedAt: account.DeletedAt}
}

func toMessage(record emailMessageRecord, recipients []mailapp.Recipient) mailapp.EmailMessage {
	return mailapp.EmailMessage{ID: record.ID, TenantID: record.TenantID, OrgID: record.OrgID, SMTPAccountID: record.SMTPAccountID, SenderID: record.SenderID, Subject: record.Subject, Recipients: append([]mailapp.Recipient(nil), recipients...), BodyCiphertext: append([]byte(nil), record.BodyCiphertext...), BodyDigest: record.BodyDigest, Status: mailapp.Status(record.Status), AttemptCount: record.AttemptCount, ProviderMessageID: record.ProviderMessageID, LastErrorCode: record.LastErrorCode, SentAt: record.SentAt, IdempotencyKey: record.IdempotencyKey, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, DeletedAt: record.DeletedAt}
}

func fromMessage(message mailapp.EmailMessage) emailMessageRecord {
	return emailMessageRecord{ID: message.ID, TenantID: message.TenantID, OrgID: message.OrgID, SMTPAccountID: message.SMTPAccountID, SenderID: message.SenderID, Subject: message.Subject, BodyCiphertext: append([]byte(nil), message.BodyCiphertext...), BodyDigest: message.BodyDigest, Status: string(message.Status), AttemptCount: message.AttemptCount, ProviderMessageID: message.ProviderMessageID, LastErrorCode: message.LastErrorCode, SentAt: message.SentAt, IdempotencyKey: message.IdempotencyKey, CreatedAt: message.CreatedAt, UpdatedAt: message.UpdatedAt, DeletedAt: message.DeletedAt}
}

func messageView(message mailapp.EmailMessage) (mailapp.MessageView, error) {
	return mailapp.MessageView{ID: message.ID, TenantID: message.TenantID, OrgID: message.OrgID, SMTPAccountID: message.SMTPAccountID, SenderID: message.SenderID, Subject: message.Subject, Recipients: append([]mailapp.Recipient(nil), message.Recipients...), BodyDigest: message.BodyDigest, Status: message.Status, AttemptCount: message.AttemptCount, ProviderMessageID: message.ProviderMessageID, LastErrorCode: message.LastErrorCode, SentAt: message.SentAt, IdempotencyKey: message.IdempotencyKey, CreatedAt: message.CreatedAt, UpdatedAt: message.UpdatedAt}, nil
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
var _ mailapp.AttemptRepository = (*GORMMessageRepository)(nil)
