package authplatform

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"html"
	"math/big"
	"strings"
	"time"

	appauth "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/auth"
	rediscache "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/cache/redis"
)

var (
	// ErrRedisCaptchaUnavailable indicates that the shared Redis-backed
	// provider cannot be used. The HTTP layer maps this to a dependency error
	// instead of silently accepting a login without a challenge.
	ErrRedisCaptchaUnavailable = errors.New("redis captcha provider is unavailable")
	ErrRedisCaptchaConfig      = errors.New("redis captcha configuration is invalid")
)

// captchaCache is the narrow cache port required by both Redis captcha
// adapters. Keeping it small makes the one-time semantics testable without a
// live Redis process while the production constructor still accepts the shared
// namespaced Redis client.
type captchaCache interface {
	Key(parts ...string) (string, error)
	SetJSON(context.Context, string, any, time.Duration) error
	GetJSON(context.Context, string, any) error
	TakeJSON(context.Context, string, any) error
	Delete(context.Context, string) error
	Increment(context.Context, string, time.Duration) (int64, error)
}

type redisCaptchaEntry struct {
	AnswerHash string `json:"answer_hash"`
}

// RedisCaptchaRiskStore stores failed-login counters in Redis. Logical
// identifiers are hashed before key construction so account names and IPs do
// not appear in Redis key material.
type RedisCaptchaRiskStore struct {
	cache  captchaCache
	prefix string
}

var _ appauth.CaptchaRiskStore = (*RedisCaptchaRiskStore)(nil)

func NewRedisCaptchaRiskStore(cache *rediscache.Client, prefix string) *RedisCaptchaRiskStore {
	return newRedisCaptchaRiskStore(cache, prefix)
}

func newRedisCaptchaRiskStore(cache captchaCache, prefix string) *RedisCaptchaRiskStore {
	return &RedisCaptchaRiskStore{cache: cache, prefix: prefix}
}

func (s *RedisCaptchaRiskStore) Requires(ctx context.Context, key string, threshold int, window time.Duration) (bool, error) {
	if err := s.validate(ctx, key, threshold, window); err != nil {
		return false, err
	}
	physical, err := s.riskKey(key)
	if err != nil {
		return false, err
	}
	var count int64
	if err := s.cache.GetJSON(ctx, physical, &count); err != nil {
		if errors.Is(err, rediscache.ErrCacheMiss) {
			return false, nil
		}
		return false, err
	}
	return count >= int64(threshold), nil
}

func (s *RedisCaptchaRiskStore) RecordFailure(ctx context.Context, key string, window time.Duration) error {
	if err := s.validate(ctx, key, 1, window); err != nil {
		return err
	}
	physical, err := s.riskKey(key)
	if err != nil {
		return err
	}
	_, err = s.cache.Increment(ctx, physical, window)
	return err
}

func (s *RedisCaptchaRiskStore) Reset(ctx context.Context, key string) error {
	if err := s.validate(ctx, key, 1, time.Second); err != nil {
		return err
	}
	physical, err := s.riskKey(key)
	if err != nil {
		return err
	}
	return s.cache.Delete(ctx, physical)
}

func (s *RedisCaptchaRiskStore) riskKey(key string) (string, error) {
	digest := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return s.cache.Key(s.prefix, "risk", hex.EncodeToString(digest[:]))
}

func (s *RedisCaptchaRiskStore) validate(ctx context.Context, key string, threshold int, window time.Duration) error {
	if err := captchaContextError(ctx); err != nil {
		return err
	}
	if s == nil || s.cache == nil {
		return ErrRedisCaptchaUnavailable
	}
	if err := validateCaptchaPrefix(s.prefix); err != nil {
		return err
	}
	if strings.TrimSpace(key) == "" {
		return appauth.ErrInvalidCaptchaRiskKey
	}
	if threshold <= 0 || window <= 0 {
		return appauth.ErrInvalidCaptchaRiskPolicy
	}
	return nil
}

// RedisCaptchaProvider implements one-time image challenges. Redis contains
// only the SHA-256 answer hash and the configured TTL; the answer is rendered
// into the returned data URI and never serialized into the stored entry.
type RedisCaptchaProvider struct {
	cache           captchaCache
	prefix          string
	ttl             time.Duration
	answerGenerator func() (string, error)
	idGenerator     func() (string, error)
}

var _ appauth.CaptchaProvider = (*RedisCaptchaProvider)(nil)

func NewRedisCaptchaProvider(cache *rediscache.Client, prefix string, ttl time.Duration) *RedisCaptchaProvider {
	return &RedisCaptchaProvider{
		cache:           cache,
		prefix:          prefix,
		ttl:             ttl,
		answerGenerator: randomCaptchaAnswer,
		idGenerator:     randomCaptchaID,
	}
}

func (p *RedisCaptchaProvider) Issue(ctx context.Context) (appauth.CaptchaChallenge, error) {
	ctx = normalizeCaptchaContext(ctx)
	if err := ctx.Err(); err != nil {
		return appauth.CaptchaChallenge{}, err
	}
	if err := p.validate(); err != nil {
		return appauth.CaptchaChallenge{}, err
	}
	answer, err := p.answerGenerator()
	if err != nil {
		return appauth.CaptchaChallenge{}, err
	}
	answer = strings.TrimSpace(answer)
	if !validCaptchaAnswer(answer) {
		return appauth.CaptchaChallenge{}, ErrRedisCaptchaConfig
	}
	id, err := p.idGenerator()
	if err != nil {
		return appauth.CaptchaChallenge{}, err
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return appauth.CaptchaChallenge{}, ErrRedisCaptchaConfig
	}
	payload := captchaImagePayload(answer)
	digest := sha256.Sum256([]byte(answer))
	entry := redisCaptchaEntry{AnswerHash: hex.EncodeToString(digest[:])}
	key, err := p.challengeKey(id)
	if err != nil {
		return appauth.CaptchaChallenge{}, err
	}
	if err := p.cache.SetJSON(ctx, key, entry, p.ttl); err != nil {
		return appauth.CaptchaChallenge{}, err
	}
	expiresIn := int64(p.ttl / time.Second)
	if expiresIn < 1 {
		expiresIn = 1
	}
	return appauth.CaptchaChallenge{
		ID: id, Kind: "image", Payload: payload, ExpiresIn: expiresIn,
	}, nil
}

func (p *RedisCaptchaProvider) Verify(ctx context.Context, challengeID, answer string) error {
	ctx = normalizeCaptchaContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.validateStorage(); err != nil {
		return err
	}
	challengeID = strings.TrimSpace(challengeID)
	answer = strings.TrimSpace(answer)
	if challengeID == "" {
		return appauth.ErrCaptchaInvalid
	}
	key, err := p.challengeKey(challengeID)
	if err != nil {
		return err
	}
	var entry redisCaptchaEntry
	if err := p.cache.TakeJSON(ctx, key, &entry); err != nil {
		if errors.Is(err, rediscache.ErrCacheMiss) {
			return appauth.ErrCaptchaExpired
		}
		return err
	}
	// TakeJSON atomically consumes the challenge, including a wrong answer, so
	// concurrent requests and captured images cannot be replayed.
	if !validCaptchaAnswer(answer) {
		return appauth.ErrCaptchaInvalid
	}
	digest := sha256.Sum256([]byte(answer))
	provided := hex.EncodeToString(digest[:])
	if subtle.ConstantTimeCompare([]byte(entry.AnswerHash), []byte(provided)) != 1 {
		return appauth.ErrCaptchaInvalid
	}
	return nil
}

func (p *RedisCaptchaProvider) challengeKey(challengeID string) (string, error) {
	digest := sha256.Sum256([]byte(strings.TrimSpace(challengeID)))
	return p.cache.Key(p.prefix, "challenge", hex.EncodeToString(digest[:]))
}

func (p *RedisCaptchaProvider) validate() error {
	if err := p.validateStorage(); err != nil {
		return err
	}
	if p.answerGenerator == nil || p.idGenerator == nil {
		return ErrRedisCaptchaConfig
	}
	return nil
}

func (p *RedisCaptchaProvider) validateStorage() error {
	if p == nil || p.cache == nil {
		return ErrRedisCaptchaUnavailable
	}
	if err := validateCaptchaPrefix(p.prefix); err != nil {
		return err
	}
	if p.ttl <= 0 {
		return ErrRedisCaptchaConfig
	}
	return nil
}

func validateCaptchaPrefix(prefix string) error {
	if prefix == "" || prefix != strings.TrimSpace(prefix) || strings.ContainsAny(prefix, "\r\n\t :") {
		return ErrRedisCaptchaConfig
	}
	return nil
}

func validCaptchaAnswer(answer string) bool {
	if len(answer) < 4 || len(answer) > 8 {
		return false
	}
	for _, character := range answer {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func captchaImagePayload(answer string) string {
	// Answers are digits only, but escaping keeps this renderer safe if the
	// validation policy is widened by a future provider.
	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="160" height="56" viewBox="0 0 160 56" role="img" aria-label="captcha"><rect width="160" height="56" rx="8" fill="#f3f4f6"/><path d="M8 44L152 12M8 16L152 42" stroke="#cbd5e1" stroke-width="2"/><text x="80" y="38" text-anchor="middle" font-family="monospace" font-size="28" font-weight="700" letter-spacing="6" fill="#111827">` + html.EscapeString(answer) + `</text></svg>`
	return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
}

func randomCaptchaAnswer() (string, error) {
	const digits = 6
	var builder strings.Builder
	for index := 0; index < digits; index++ {
		value, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		builder.WriteByte(byte('0' + value.Int64()))
	}
	return builder.String(), nil
}

func randomCaptchaID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func normalizeCaptchaContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func captchaContextError(ctx context.Context) error {
	return normalizeCaptchaContext(ctx).Err()
}
