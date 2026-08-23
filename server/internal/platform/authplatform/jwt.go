package authplatform

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/authdomain"
)

var ErrJWTSecret = errors.New("jwt secret must not be empty")

type JWTService struct {
	secret                []byte
	accessTTL, refreshTTL time.Duration
	Issuer                string
	Audience              string
}

func NewJWTService(secret []byte, accessTTL, refreshTTL time.Duration) *JWTService {
	return &JWTService{secret: append([]byte(nil), secret...), accessTTL: accessTTL, refreshTTL: refreshTTL}
}

// NewJWTServiceWithOptions enables issuer/audience validation while retaining
// the compact constructor used by dependency injection.
func NewJWTServiceWithOptions(secret []byte, accessTTL, refreshTTL time.Duration, issuer, audience string) *JWTService {
	s := NewJWTService(secret, accessTTL, refreshTTL)
	s.Issuer, s.Audience = issuer, audience
	return s
}

func (s *JWTService) Issue(userID, sessionID string) (authdomain.TokenPair, error) {
	if len(s.secret) == 0 {
		return authdomain.TokenPair{}, ErrJWTSecret
	}
	now := time.Now().UTC()
	access, err := s.sign(userID, sessionID, authdomain.AccessToken, now, s.accessTTL)
	if err != nil {
		return authdomain.TokenPair{}, err
	}
	refresh, err := s.sign(userID, sessionID, authdomain.RefreshToken, now, s.refreshTTL)
	if err != nil {
		return authdomain.TokenPair{}, err
	}
	return authdomain.TokenPair{AccessToken: access, RefreshToken: refresh, ExpiresIn: int64(s.accessTTL.Seconds())}, nil
}

func (s *JWTService) sign(subject, sessionID, typ string, now time.Time, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", errors.New("jwt ttl must be positive")
	}
	jtiBytes := make([]byte, 16)
	if _, err := rand.Read(jtiBytes); err != nil {
		return "", err
	}
	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	claims := map[string]any{"sub": subject, "sid": sessionID, "jti": hex.EncodeToString(jtiBytes), "typ": typ, "iat": now.Unix(), "exp": now.Add(ttl).Unix()}
	if s.Issuer != "" {
		claims["iss"] = s.Issuer
	}
	if s.Audience != "" {
		claims["aud"] = s.Audience
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	unsigned := enc.EncodeToString(header) + "." + enc.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + enc.EncodeToString(mac.Sum(nil)), nil
}

func (s *JWTService) Parse(token string) (authdomain.Claims, error) {
	if len(s.secret) == 0 {
		return authdomain.Claims{}, authdomain.ErrInvalidToken
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return authdomain.Claims{}, authdomain.ErrInvalidToken
	}
	enc := base64.RawURLEncoding
	var header struct{ Alg, Typ string }
	headerBytes, err := enc.DecodeString(parts[0])
	if err != nil || json.Unmarshal(headerBytes, &header) != nil || header.Alg != "HS256" || header.Typ != "JWT" {
		return authdomain.Claims{}, authdomain.ErrInvalidToken
	}
	provided, err := enc.DecodeString(parts[2])
	if err != nil {
		return authdomain.Claims{}, authdomain.ErrInvalidToken
	}
	mac := hmac.New(sha256.New, s.secret)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return authdomain.Claims{}, authdomain.ErrInvalidToken
	}
	var p struct {
		Sub, SID, JTI, Typ, Iss, Aud string
		IAT, Exp                     int64
	}
	b, err := enc.DecodeString(parts[1])
	if err != nil || json.Unmarshal(b, &p) != nil || p.Sub == "" || p.SID == "" || p.JTI == "" || p.Typ == "" {
		return authdomain.Claims{}, authdomain.ErrInvalidToken
	}
	if s.Issuer != "" && p.Iss != s.Issuer {
		return authdomain.Claims{}, authdomain.ErrInvalidToken
	}
	if s.Audience != "" && p.Aud != s.Audience {
		return authdomain.Claims{}, authdomain.ErrInvalidToken
	}
	if p.Exp <= time.Now().Unix() || p.IAT > time.Now().Add(time.Minute).Unix() {
		return authdomain.Claims{}, authdomain.ErrInvalidToken
	}
	if p.Typ != authdomain.AccessToken && p.Typ != authdomain.RefreshToken {
		return authdomain.Claims{}, authdomain.ErrInvalidToken
	}
	return authdomain.Claims{Subject: p.Sub, SessionID: p.SID, TokenID: p.JTI, Type: p.Typ, IssuedAt: time.Unix(p.IAT, 0), ExpiresAt: time.Unix(p.Exp, 0)}, nil
}
