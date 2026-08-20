// Package auth is the conventional import path for the authentication domain.
// The aliases keep the domain ports usable without coupling transports to the
// concrete platform package name.
package auth

import base "example.com/gin-vben-admin/server/internal/domain/authdomain"

type User = base.User
type Session = base.Session
type TokenPair = base.TokenPair
type Claims = base.Claims
type TokenRecord = base.TokenRecord
type UserRepository = base.UserRepository
type PasswordHasher = base.PasswordHasher
type TokenService = base.TokenService
type SessionStore = base.SessionStore
type TokenStore = base.TokenStore

var (
	ErrInvalidCredentials = base.ErrInvalidCredentials
	ErrInvalidToken       = base.ErrInvalidToken
	ErrRefreshReplay      = base.ErrRefreshReplay
	ErrSessionNotFound    = base.ErrSessionNotFound
	ErrSessionRevoked     = base.ErrSessionRevoked
)

const (
	AccessToken  = base.AccessToken
	RefreshToken = base.RefreshToken
)
