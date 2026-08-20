package authplatform

import (
	"context"
	"errors"
	"strconv"

	"example.com/gin-vben-admin/server/internal/domain/authdomain"
	"example.com/gin-vben-admin/server/internal/platform/persistence/gormdb"
	"gorm.io/gorm"
)

// ErrUserLookup is returned when the persistence layer cannot complete a user
// lookup. The underlying database error is intentionally not exposed.
var ErrUserLookup = errors.New("user lookup failed")

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
