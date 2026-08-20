package authplatform

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"example.com/gin-vben-admin/server/internal/domain/authdomain"
	"example.com/gin-vben-admin/server/internal/platform/persistence/gormdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GORMSessionStore is the durable auth-session adapter. Database writes use
// the primary endpoint so refresh rotation and revocation are read-your-write
// operations even when the application later enables read/write routing.
type GORMSessionStore struct {
	db *gormdb.Store
}

// NewGORMSessionStore constructs a durable session store. The nil form is
// retained as a compile-time seam for dependency injection tests; operations
// on it return the same dependency error used by the auth service.
func NewGORMSessionStore(db *gormdb.Store) *GORMSessionStore {
	return &GORMSessionStore{db: db}
}

type authSessionRecord struct {
	ID               string     `gorm:"column:id;primaryKey"`
	UserID           uint64     `gorm:"column:user_id"`
	RefreshTokenHash string     `gorm:"column:refresh_token_hash"`
	FamilyID         string     `gorm:"column:family_id"`
	Status           string     `gorm:"column:status"`
	ExpiresAt        time.Time  `gorm:"column:expires_at"`
	LastSeenAt       time.Time  `gorm:"column:last_seen_at"`
	RevokedAt        *time.Time `gorm:"column:revoked_at"`
	CreatedAt        time.Time  `gorm:"column:created_at"`
}

func (authSessionRecord) TableName() string { return "auth_sessions" }

func (s *GORMSessionStore) Create(ctx context.Context, session authdomain.Session) error {
	if err := sessionContext(ctx); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return authdomain.ErrDependencyUnavailable
	}
	if strings.TrimSpace(session.ID) == "" || strings.TrimSpace(session.RefreshJTI) == "" || !session.ExpiresAt.After(time.Now()) {
		return authdomain.ErrSessionRevoked
	}
	userID, err := parseNumericUserID(session.UserID)
	if err != nil {
		return authdomain.ErrDependencyUnavailable
	}
	record := authSessionRecord{
		ID:               session.ID,
		UserID:           userID,
		RefreshTokenHash: authdomain.HashRefreshJTI(session.RefreshJTI),
		FamilyID:         session.ID,
		Status:           sessionStatus(session.Revoked),
		ExpiresAt:        session.ExpiresAt,
		LastSeenAt:       time.Now().UTC(),
	}
	if session.Revoked {
		now := time.Now().UTC()
		record.RevokedAt = &now
	}
	if err := s.db.Write(ctx).Create(&record).Error; err != nil {
		return authdomain.ErrDependencyUnavailable
	}
	return nil
}

func (s *GORMSessionStore) Get(ctx context.Context, id string) (authdomain.Session, error) {
	if err := sessionContext(ctx); err != nil {
		return authdomain.Session{}, err
	}
	if s == nil || s.db == nil {
		return authdomain.Session{}, authdomain.ErrDependencyUnavailable
	}
	if strings.TrimSpace(id) == "" {
		return authdomain.Session{}, authdomain.ErrSessionNotFound
	}
	var record authSessionRecord
	err := s.db.Write(ctx).Where("id = ?", id).Take(&record).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return authdomain.Session{}, authdomain.ErrSessionNotFound
	}
	if err != nil {
		return authdomain.Session{}, authdomain.ErrDependencyUnavailable
	}
	return record.toDomain()
}

func (s *GORMSessionStore) Rotate(ctx context.Context, id, expectedJTI, nextJTI string, expiresAt time.Time) error {
	if err := sessionContext(ctx); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return authdomain.ErrDependencyUnavailable
	}
	if strings.TrimSpace(id) == "" || strings.TrimSpace(expectedJTI) == "" || strings.TrimSpace(nextJTI) == "" || !expiresAt.After(time.Now()) {
		return authdomain.ErrInvalidToken
	}
	err := s.db.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var record authSessionRecord
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&record).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return authdomain.ErrSessionNotFound
		}
		if err != nil {
			return authdomain.ErrDependencyUnavailable
		}
		if record.Status != "active" || record.RevokedAt != nil || !record.ExpiresAt.After(time.Now()) {
			return authdomain.ErrSessionRevoked
		}
		if record.RefreshTokenHash != authdomain.HashRefreshJTI(expectedJTI) {
			return authdomain.ErrRefreshReplay
		}
		updates := map[string]any{
			"refresh_token_hash": authdomain.HashRefreshJTI(nextJTI),
			"expires_at":         expiresAt,
			"last_seen_at":       time.Now().UTC(),
		}
		if err := tx.Model(&authSessionRecord{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return authdomain.ErrDependencyUnavailable
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func (s *GORMSessionStore) Revoke(ctx context.Context, id string) error {
	if err := sessionContext(ctx); err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return authdomain.ErrDependencyUnavailable
	}
	return s.db.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var record authSessionRecord
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).Take(&record).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return authdomain.ErrSessionNotFound
		}
		if err != nil {
			return authdomain.ErrDependencyUnavailable
		}
		if record.Status != "active" || record.RevokedAt != nil {
			return authdomain.ErrSessionRevoked
		}
		now := time.Now().UTC()
		if err := tx.Model(&authSessionRecord{}).Where("id = ?", id).Updates(map[string]any{
			"status":       "revoked",
			"revoked_at":   now,
			"last_seen_at": now,
		}).Error; err != nil {
			return authdomain.ErrDependencyUnavailable
		}
		return nil
	})
}

func (r authSessionRecord) toDomain() (authdomain.Session, error) {
	if r.UserID == 0 || strings.TrimSpace(r.ID) == "" {
		return authdomain.Session{}, authdomain.ErrDependencyUnavailable
	}
	return authdomain.Session{
		ID:             r.ID,
		UserID:         strconv.FormatUint(r.UserID, 10),
		RefreshJTIHash: r.RefreshTokenHash,
		ExpiresAt:      r.ExpiresAt,
		Revoked:        r.Status != "active" || r.RevokedAt != nil,
	}, nil
}

func sessionContext(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func parseNumericUserID(value string) (uint64, error) {
	id, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil || id == 0 {
		return 0, errors.New("user id must be numeric")
	}
	return id, nil
}

func sessionStatus(revoked bool) string {
	if revoked {
		return "revoked"
	}
	return "active"
}
