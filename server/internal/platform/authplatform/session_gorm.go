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
	TenantID         string     `gorm:"column:tenant_id"`
	UserID           uint64     `gorm:"column:user_id"`
	RefreshTokenHash string     `gorm:"column:refresh_token_hash"`
	FamilyID         string     `gorm:"column:family_id"`
	Status           string     `gorm:"column:status"`
	DeviceID         string     `gorm:"column:device_id"`
	DeviceName       string     `gorm:"column:device_name"`
	IPAddress        string     `gorm:"column:ip_address"`
	UserAgent        string     `gorm:"column:user_agent"`
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
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(session.TenantID) == "" {
		return tenant.ErrTenantRequired
	}
	if session.TenantID != scope.TenantID {
		return tenant.ErrCrossTenant
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
		TenantID:         scope.TenantID,
		UserID:           userID,
		RefreshTokenHash: authdomain.HashRefreshJTI(session.RefreshJTI),
		FamilyID:         session.ID,
		Status:           sessionStatus(session.Revoked),
		DeviceID:         session.DeviceID,
		DeviceName:       session.DeviceName,
		IPAddress:        session.IPAddress,
		UserAgent:        session.UserAgent,
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

// ListByUser returns durable device sessions newest first. The user predicate
// is part of the SQL query, not only an application-side filter.
func (s *GORMSessionStore) ListByUser(ctx context.Context, userID string) ([]authdomain.Session, error) {
	if err := sessionContext(ctx); err != nil {
		return nil, err
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return nil, err
	}
	if s == nil || s.db == nil {
		return nil, authdomain.ErrDependencyUnavailable
	}
	parsedID, err := parseNumericUserID(userID)
	if err != nil {
		return nil, authdomain.ErrDependencyUnavailable
	}
	var records []authSessionRecord
	if err := s.db.Write(ctx).Where("tenant_id = ? AND user_id = ?", scope.TenantID, parsedID).Order("created_at DESC").Find(&records).Error; err != nil {
		return nil, authdomain.ErrDependencyUnavailable
	}
	result := make([]authdomain.Session, 0, len(records))
	for _, record := range records {
		session, err := record.toDomain()
		if err != nil {
			return nil, err
		}
		result = append(result, session)
	}
	return result, nil
}

// RevokeOwned revokes one session only when it belongs to userID.
func (s *GORMSessionStore) RevokeOwned(ctx context.Context, userID, sessionID string) error {
	if err := sessionContext(ctx); err != nil {
		return err
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return authdomain.ErrDependencyUnavailable
	}
	parsedID, err := parseNumericUserID(userID)
	if err != nil || strings.TrimSpace(sessionID) == "" {
		return authdomain.ErrSessionNotFound
	}
	return s.db.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var record authSessionRecord
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ? AND user_id = ?", scope.TenantID, sessionID, parsedID).Take(&record).Error
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
		if err := tx.Model(&authSessionRecord{}).Where("tenant_id = ? AND id = ? AND user_id = ?", scope.TenantID, sessionID, parsedID).Updates(map[string]any{
			"status":       "revoked",
			"revoked_at":   now,
			"last_seen_at": now,
		}).Error; err != nil {
			return authdomain.ErrDependencyUnavailable
		}
		return nil
	})
}

func (s *GORMSessionStore) Get(ctx context.Context, id string) (authdomain.Session, error) {
	if err := sessionContext(ctx); err != nil {
		return authdomain.Session{}, err
	}
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return authdomain.Session{}, err
	}
	if s == nil || s.db == nil {
		return authdomain.Session{}, authdomain.ErrDependencyUnavailable
	}
	if strings.TrimSpace(id) == "" {
		return authdomain.Session{}, authdomain.ErrSessionNotFound
	}
	var record authSessionRecord
	err = s.db.Write(ctx).Where("tenant_id = ? AND id = ?", scope.TenantID, id).Take(&record).Error
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
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return authdomain.ErrDependencyUnavailable
	}
	if strings.TrimSpace(id) == "" || strings.TrimSpace(expectedJTI) == "" || strings.TrimSpace(nextJTI) == "" || !expiresAt.After(time.Now()) {
		return authdomain.ErrInvalidToken
	}
	err = s.db.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var record authSessionRecord
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", scope.TenantID, id).Take(&record).Error
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
		if err := tx.Model(&authSessionRecord{}).Where("tenant_id = ? AND id = ?", scope.TenantID, id).Updates(updates).Error; err != nil {
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
	scope, err := tenant.RequireContext(ctx)
	if err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return authdomain.ErrDependencyUnavailable
	}
	return s.db.WithinTransaction(ctx, func(tx *gorm.DB) error {
		var record authSessionRecord
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = ? AND id = ?", scope.TenantID, id).Take(&record).Error
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
		if err := tx.Model(&authSessionRecord{}).Where("tenant_id = ? AND id = ?", scope.TenantID, id).Updates(map[string]any{
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
	if strings.TrimSpace(r.TenantID) == "" {
		return authdomain.Session{}, tenant.ErrTenantRequired
	}
	return authdomain.Session{
		ID:             r.ID,
		TenantID:       r.TenantID,
		UserID:         strconv.FormatUint(r.UserID, 10),
		RefreshJTIHash: r.RefreshTokenHash,
		DeviceID:       r.DeviceID,
		DeviceName:     r.DeviceName,
		IPAddress:      r.IPAddress,
		UserAgent:      r.UserAgent,
		ExpiresAt:      r.ExpiresAt,
		CreatedAt:      r.CreatedAt,
		LastSeenAt:     r.LastSeenAt,
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
