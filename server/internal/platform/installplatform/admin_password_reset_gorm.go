package installplatform

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
	"github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/persistence/gormdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrInitialAdminPasswordReset = errors.New("initial administrator password reset failed")

type initialAdminTransactionStore interface {
	WithinTransaction(context.Context, func(*gorm.DB) error) error
}

// GORMInitialAdminPasswordStore is the database adapter for the local recovery
// command. It resolves the account exclusively from the completed installation
// receipt, rather than accepting a caller-provided username or user id.
type GORMInitialAdminPasswordStore struct {
	database initialAdminTransactionStore
}

func NewGORMInitialAdminPasswordStore(database *gormdb.Store) *GORMInitialAdminPasswordStore {
	return &GORMInitialAdminPasswordStore{database: database}
}

func (s *GORMInitialAdminPasswordStore) InitialAdminIdentifier(ctx context.Context) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s == nil || s.database == nil {
		return "", ErrInitialAdminPasswordReset
	}

	var identifier string
	err := s.database.WithinTransaction(ctx, func(tx *gorm.DB) error {
		identity, err := loadInstalledInitialAdmin(ctx, tx)
		if err != nil {
			return err
		}
		identifier = identity.identifier
		return nil
	})
	if err != nil || identifier == "" {
		return "", ErrInitialAdminPasswordReset
	}
	return identifier, nil
}

func (s *GORMInitialAdminPasswordStore) ResetInitialAdminPassword(ctx context.Context, identifier, passwordHash string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	normalized, identifierType, err := authdomain.NormalizeIdentifier(identifier)
	if s == nil || s.database == nil || err != nil || identifierType != authdomain.IdentifierUsername ||
		normalized != identifier || !validInitialAdminPasswordHash(passwordHash) {
		return ErrInitialAdminPasswordReset
	}

	err = s.database.WithinTransaction(ctx, func(tx *gorm.DB) error {
		identity, err := loadInstalledInitialAdmin(ctx, tx)
		if err != nil {
			return err
		}
		if identity.identifier != identifier {
			return ErrInitialAdminPasswordReset
		}
		rows, err := gorm.G[installationUserRow](tx).
			Where("tenant_id = ? AND id = ? AND username = ? AND status = ?", initialTenantID, identity.userID, identity.username, "active").
			Set(clause.Assignments(map[string]any{
				"password_hash":        passwordHash,
				"failed_attempts":      0,
				"locked_until":         nil,
				"must_change_password": false,
				"password_changed_at":  time.Now().UTC(),
			})).Update(ctx)
		if err != nil {
			return err
		}
		if rows != 1 {
			return ErrInitialAdminPasswordReset
		}
		return nil
	})
	if err != nil {
		return ErrInitialAdminPasswordReset
	}
	return nil
}

type installedInitialAdmin struct {
	userID     uint64
	username   string
	identifier string
}

func loadInstalledInitialAdmin(ctx context.Context, tx *gorm.DB) (installedInitialAdmin, error) {
	metadata, found, err := lockInstallationMetadata(tx)
	if err != nil {
		return installedInitialAdmin{}, err
	}
	if !found || metadata.State != "installed" || metadata.UserID == 0 ||
		metadata.RoleID != installationRoleID || strings.TrimSpace(metadata.Username) == "" {
		return installedInitialAdmin{}, ErrInitialAdminPasswordReset
	}
	row, err := gorm.G[installationUserRow](tx, clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = ? AND id = ? AND username = ?", initialTenantID, metadata.UserID, metadata.Username).
		Take(ctx)
	if err != nil {
		return installedInitialAdmin{}, err
	}
	normalized, identifierType, err := authdomain.NormalizeIdentifier(metadata.Username)
	if err != nil || identifierType != authdomain.IdentifierUsername || row.UsernameNormalized == nil ||
		*row.UsernameNormalized != normalized || row.Status != "active" {
		return installedInitialAdmin{}, ErrInitialAdminPasswordReset
	}
	return installedInitialAdmin{userID: metadata.UserID, username: metadata.Username, identifier: normalized}, nil
}

func validInitialAdminPasswordHash(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= 255 &&
		!strings.ContainsAny(value, "\x00\r\n")
}
