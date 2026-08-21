package authplatform

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"example.com/gin-vben-admin/server/internal/domain/authdomain"
	"example.com/gin-vben-admin/server/internal/domain/tenant"
	"example.com/gin-vben-admin/server/internal/platform/persistence/gormdb"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// ErrUserLookup is returned when the persistence layer cannot complete a user
// lookup. The underlying database error is intentionally not exposed.
var ErrUserLookup = errors.New("user lookup failed")

// ErrUserProvisioning is returned when a credential write cannot be
// completed. Driver details are deliberately kept behind this sentinel.
var ErrUserProvisioning = errors.New("user provisioning failed")

// GORMUserRepository adapts the users table to the authentication port.
type GORMUserRepository struct {
	store *gormdb.Store
}

// GormUserRepository is kept as an acronym-style alias for callers that use
// Go's mixed-case initialism convention.
type GormUserRepository = GORMUserRepository

// NewGORMUserRepository constructs a user repository backed by the configured
// GORM store.
func NewGORMUserRepository(store *gormdb.Store) *GORMUserRepository {
	return &GORMUserRepository{store: store}
}

// NewGormUserRepository is a spelling-compatible constructor alias.
func NewGormUserRepository(store *gormdb.Store) *GORMUserRepository {
	return NewGORMUserRepository(store)
}

var _ authdomain.UserRepository = (*GORMUserRepository)(nil)

type gormUserRow struct {
	ID                 uint64     `gorm:"column:id"`
	TenantID           string     `gorm:"column:tenant_id"`
	OrgID              *string    `gorm:"column:org_id"`
	Username           string     `gorm:"column:username"`
	UsernameNormalized *string    `gorm:"column:username_normalized"`
	Email              *string    `gorm:"column:email"`
	EmailNormalized    *string    `gorm:"column:email_normalized"`
	Nickname           *string    `gorm:"column:nickname"`
	Avatar             *string    `gorm:"column:avatar"`
	Phone              *string    `gorm:"column:phone"`
	PasswordHash       string     `gorm:"column:password_hash"`
	Status             string     `gorm:"column:status"`
	LastLoginIP        *string    `gorm:"column:last_login_ip"`
	LastLoginAt        *time.Time `gorm:"column:last_login_at"`
	PasswordChangedAt  *time.Time `gorm:"column:password_changed_at"`
}

func (gormUserRow) TableName() string { return "users" }

// FindByIdentifier loads a user by normalized username or email through the
// primary/write route. Authentication must not accept stale replica state
// after an administrator disables or locks an account.
func (r *GORMUserRepository) FindByIdentifier(ctx context.Context, identifier string) (authdomain.User, error) {
	if r == nil || r.store == nil {
		return authdomain.User{}, ErrUserLookup
	}
	tenantID, err := requestTenantID(ctx)
	if err != nil {
		return authdomain.User{}, err
	}
	normalized, identifierType, err := authdomain.NormalizeIdentifier(identifier)
	if err != nil {
		return authdomain.User{}, err
	}

	var row gormUserRow
	query := r.store.Write(ctx).Where("tenant_id = ?", tenantID)
	if identifierType == authdomain.IdentifierEmail {
		query = query.Where("email_normalized = ?", normalized)
	} else {
		query = query.Where("username_normalized = ?", normalized)
	}
	err = query.First(&row).Error
	if err == nil {
		return row.toDomain(), nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return authdomain.User{}, authdomain.ErrInvalidCredentials
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return authdomain.User{}, err
	}
	return authdomain.User{}, ErrUserLookup
}

// CreateUser inserts a new active/disabled user through the primary endpoint.
// The database uniqueness constraint remains the authority for races.
func (r *GORMUserRepository) CreateUser(ctx context.Context, user authdomain.User) error {
	if r == nil || r.store == nil || user.PasswordHash == "" {
		return ErrUserProvisioning
	}
	tenantID, err := requestTenantID(ctx)
	if err != nil {
		return err
	}
	row, err := userRowFromDomain(user, tenantID)
	if err != nil {
		return err
	}
	status := "disabled"
	if user.Active {
		status = "active"
	}
	row.Status = status
	err = r.store.Write(ctx).Create(&row).Error
	return mapUserWriteError(err)
}

// UpdatePassword replaces a credential and clears login lock state. It uses a
// primary write so a subsequent login cannot observe a replica's stale hash.
func (r *GORMUserRepository) UpdatePassword(ctx context.Context, identifier, passwordHash string) error {
	if r == nil || r.store == nil || identifier == "" || passwordHash == "" {
		return ErrUserProvisioning
	}
	tenantID, err := requestTenantID(ctx)
	if err != nil {
		return err
	}
	normalized, identifierType, err := authdomain.NormalizeIdentifier(identifier)
	if err != nil {
		return err
	}
	column := "username_normalized"
	if identifierType == authdomain.IdentifierEmail {
		column = "email_normalized"
	}
	result := r.store.Write(ctx).Model(&gormUserRow{}).
		Where("tenant_id = ? AND "+column+" = ?", tenantID, normalized).
		Updates(map[string]any{
			"password_hash":        passwordHash,
			"failed_attempts":      0,
			"locked_until":         nil,
			"must_change_password": false,
			"password_changed_at":  time.Now().UTC(),
		})
	if err := mapUserWriteError(result.Error); err != nil {
		return err
	}
	if result.RowsAffected == 0 {
		return authdomain.ErrInvalidCredentials
	}
	return nil
}

func (row gormUserRow) toDomain() authdomain.User {
	username := row.Username
	email := stringValue(row.Email)
	identifier := username
	if strings.TrimSpace(identifier) == "" {
		identifier = email
	}
	usernameNormalized := stringValue(row.UsernameNormalized)
	emailNormalized := stringValue(row.EmailNormalized)
	return authdomain.User{
		ID:                 strconv.FormatUint(row.ID, 10),
		Identifier:         identifier,
		Username:           username,
		UsernameNormalized: usernameNormalized,
		Email:              email,
		EmailNormalized:    emailNormalized,
		Nickname:           stringValue(row.Nickname),
		Avatar:             stringValue(row.Avatar),
		Phone:              stringValue(row.Phone),
		PasswordHash:       row.PasswordHash,
		Active:             row.Status == "active",
		LastLoginIP:        stringValue(row.LastLoginIP),
		LastLoginAt:        timeValue(row.LastLoginAt),
		PasswordChangedAt:  timeValue(row.PasswordChangedAt),
		TenantID:           row.TenantID,
		OrgID:              stringValue(row.OrgID),
	}
}

func userRowFromDomain(user authdomain.User, tenantID string) (gormUserRow, error) {
	username := strings.TrimSpace(user.Username)
	email := strings.TrimSpace(user.Email)
	if username == "" && email == "" {
		identifier := strings.TrimSpace(user.Identifier)
		canonical, kind, err := authdomain.NormalizeIdentifier(identifier)
		if err != nil {
			return gormUserRow{}, err
		}
		if kind == authdomain.IdentifierEmail {
			email = canonical
		} else {
			username = canonical
		}
	}
	if username == "" {
		// The users schema keeps username as a required profile key. An
		// email-only provision request uses its canonical email as that key
		// while still retaining the email alias below.
		if email == "" {
			return gormUserRow{}, authdomain.ErrInvalidIdentifier
		}
		canonicalEmail, kind, normalizeErr := authdomain.NormalizeIdentifier(email)
		if normalizeErr != nil || kind != authdomain.IdentifierEmail || len([]byte(canonicalEmail)) > 191 {
			return gormUserRow{}, authdomain.ErrInvalidIdentifier
		}
		username = canonicalEmail
	}
	row := gormUserRow{TenantID: tenantID, Username: username, PasswordHash: user.PasswordHash}
	{
		normalized, kind, err := authdomain.NormalizeIdentifier(username)
		if err != nil || kind != authdomain.IdentifierUsername {
			return gormUserRow{}, authdomain.ErrInvalidIdentifier
		}
		row.UsernameNormalized = stringPtr(normalized)
	}
	if email != "" {
		normalized, kind, err := authdomain.NormalizeIdentifier(email)
		if err != nil || kind != authdomain.IdentifierEmail {
			return gormUserRow{}, authdomain.ErrInvalidIdentifier
		}
		row.Email = stringPtr(email)
		row.EmailNormalized = stringPtr(normalized)
	}
	row.OrgID = stringPtrIfNonEmpty(user.OrgID)
	row.Nickname = stringPtrIfNonEmpty(user.Nickname)
	row.Avatar = stringPtrIfNonEmpty(user.Avatar)
	phone, err := authdomain.NormalizePhone(user.Phone)
	if err != nil {
		return gormUserRow{}, err
	}
	row.Phone = stringPtrIfNonEmpty(phone)
	row.LastLoginIP = stringPtrIfNonEmpty(user.LastLoginIP)
	if !user.LastLoginAt.IsZero() {
		value := user.LastLoginAt.UTC()
		row.LastLoginAt = &value
	}
	if !user.PasswordChangedAt.IsZero() {
		value := user.PasswordChangedAt.UTC()
		row.PasswordChangedAt = &value
	}
	return row, nil
}

func stringPtr(value string) *string { return &value }

func stringPtrIfNonEmpty(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func timeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}

func requestTenantID(ctx context.Context) (string, error) {
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return "", err
	}
	return scope.TenantID, nil
}

func mapUserWriteError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return authdomain.ErrUserAlreadyExists
	}
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return authdomain.ErrUserAlreadyExists
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return authdomain.ErrUserAlreadyExists
	}
	return ErrUserProvisioning
}
