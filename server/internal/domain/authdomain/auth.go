// Package authdomain contains authentication entities and application ports.
package authdomain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"
)

var (
	ErrInvalidCredentials    = errors.New("invalid credentials")
	ErrInvalidToken          = errors.New("invalid token")
	ErrRefreshReplay         = errors.New("refresh token replay detected")
	ErrSessionNotFound       = errors.New("session not found")
	ErrSessionRevoked        = errors.New("session revoked")
	ErrDependencyUnavailable = errors.New("authentication dependency unavailable")
	ErrAccountLocked         = errors.New("authentication account locked")
)

const (
	AccessToken  = "access"
	RefreshToken = "refresh"
)

type User struct {
	ID           string
	Identifier   string
	PasswordHash string
	Active       bool
}

type Session struct {
	ID             string
	UserID         string
	RefreshJTI     string
	RefreshJTIHash string
	ExpiresAt      time.Time
	Revoked        bool
}

// HashRefreshJTI returns the storage-safe digest used by durable session
// journals. Runtime session stores may retain RefreshJTI directly; durable
// adapters should populate RefreshJTIHash instead.
func HashRefreshJTI(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// MatchesRefreshJTI accepts either the in-memory token identifier or its
// durable digest without exposing the raw refresh token in a database record.
func (s Session) MatchesRefreshJTI(value string) bool {
	return s.RefreshJTI == value || (s.RefreshJTIHash != "" && s.RefreshJTIHash == HashRefreshJTI(value))
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

type Claims struct {
	Subject   string
	SessionID string
	TokenID   string
	Type      string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

type UserRepository interface {
	FindByIdentifier(ctx context.Context, identifier string) (User, error)
}

type PasswordHasher interface {
	Hash(password string) (string, error)
	Compare(encoded, password string) error
}

type TokenService interface {
	Issue(userID, sessionID string) (TokenPair, error)
	Parse(token string) (Claims, error)
}

// SessionStore atomically rotates the currently valid refresh token JTI.
type SessionStore interface {
	Create(ctx context.Context, session Session) error
	Get(ctx context.Context, id string) (Session, error)
	Rotate(ctx context.Context, id, expectedJTI, nextJTI string, expiresAt time.Time) error
	Revoke(ctx context.Context, id string) error
}

// TokenStore is an optional token index for revocation/introspection. JWT
// verification remains stateless; implementations can retain refresh/access
// metadata when an installation needs server-side token lookup.
type TokenRecord struct {
	UserID    string    `json:"user_id"`
	SessionID string    `json:"session_id"`
	TokenID   string    `json:"token_id"`
	Type      string    `json:"type"`
	ExpiresAt time.Time `json:"expires_at"`
}

type TokenStore interface {
	Put(ctx context.Context, record TokenRecord) error
	Get(ctx context.Context, tokenID string) (TokenRecord, error)
	Delete(ctx context.Context, tokenID string) error
}
