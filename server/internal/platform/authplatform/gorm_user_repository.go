package authplatform

import (
	"context"
	"errors"
	"strconv"

	"example.com/gin-vben-admin/server/internal/domain/authdomain"
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
	ID           uint64 `gorm:"column:id"`
	Username     string `gorm:"column:username"`
	PasswordHash string `gorm:"column:password_hash"`
	Status       string `gorm:"column:status"`
}

func (gormUserRow) TableName() string { return "users" }

// FindByIdentifier loads a user by username through the primary/write route.
// Authentication must not accept stale replica state after an administrator
// disables or locks an account.
func (r *GORMUserRepository) FindByIdentifier(ctx context.Context, identifier string) (authdomain.User, error) {
	if r == nil || r.store == nil {
		return authdomain.User{}, ErrUserLookup
	}

	var row gormUserRow
	err := r.store.Write(ctx).Where("username = ?", identifier).First(&row).Error
	if err == nil {
		return authdomain.User{
			ID:           strconv.FormatUint(row.ID, 10),
			Identifier:   row.Username,
			PasswordHash: row.PasswordHash,
			Active:       row.Status == "active",
		}, nil
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
	if r == nil || r.store == nil || user.Identifier == "" || user.PasswordHash == "" {
		return ErrUserProvisioning
	}
	status := "disabled"
	if user.Active {
		status = "active"
	}
	err := r.store.Write(ctx).Create(&gormUserRow{
		Username:     user.Identifier,
		PasswordHash: user.PasswordHash,
		Status:       status,
	}).Error
	return mapUserWriteError(err)
}

// UpdatePassword replaces a credential and clears login lock state. It uses a
// primary write so a subsequent login cannot observe a replica's stale hash.
func (r *GORMUserRepository) UpdatePassword(ctx context.Context, identifier, passwordHash string) error {
	if r == nil || r.store == nil || identifier == "" || passwordHash == "" {
		return ErrUserProvisioning
	}
	result := r.store.Write(ctx).Model(&gormUserRow{}).
		Where("username = ?", identifier).
		Updates(map[string]any{
			"password_hash":        passwordHash,
			"failed_attempts":      0,
			"locked_until":         nil,
			"must_change_password": false,
		})
	if err := mapUserWriteError(result.Error); err != nil {
		return err
	}
	if result.RowsAffected == 0 {
		return authdomain.ErrInvalidCredentials
	}
	return nil
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
