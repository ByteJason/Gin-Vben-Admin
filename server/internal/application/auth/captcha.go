package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
)

var (
	ErrCaptchaInvalid = errors.New("captcha is invalid")
	ErrCaptchaExpired = errors.New("captcha is expired")
)

// CaptchaChallenge contains only public rendering data. Providers keep the
// expected answer private and may render an image, slider, or remote widget in
// Payload without changing the login use case.
type CaptchaChallenge struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Payload   string `json:"payload,omitempty"`
	ExpiresIn int64  `json:"expiresIn"`
}

type CaptchaProvider interface {
	Issue(context.Context) (CaptchaChallenge, error)
	Verify(ctx context.Context, challengeID, answer string) error
}

type captchaEntry struct {
	answerHash [sha256.Size]byte
	expiresAt  time.Time
	ready      bool
}

// MemoryCaptchaProvider is a concurrency-safe provider seam for local tests
// and adapters. PutAnswer is intentionally separate from Issue so an image,
// slider, or remote provider can supply its own challenge-generation process.
type MemoryCaptchaProvider struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]captchaEntry
}

func NewMemoryCaptchaProvider(ttl time.Duration) *MemoryCaptchaProvider {
	if ttl <= 0 {
		ttl = 2 * time.Minute
	}
	return &MemoryCaptchaProvider{ttl: ttl, entries: map[string]captchaEntry{}}
}

func (p *MemoryCaptchaProvider) Issue(ctx context.Context) (CaptchaChallenge, error) {
	if err := contextError(ctx); err != nil {
		return CaptchaChallenge{}, err
	}
	if p == nil {
		return CaptchaChallenge{}, ErrCaptchaExpired
	}
	id, err := captchaID()
	if err != nil {
		return CaptchaChallenge{}, err
	}
	p.mu.Lock()
	p.entries[id] = captchaEntry{expiresAt: time.Now().Add(p.ttl)}
	p.mu.Unlock()
	return CaptchaChallenge{ID: id, Kind: "provider", ExpiresIn: int64(p.ttl.Seconds())}, nil
}

func (p *MemoryCaptchaProvider) PutAnswer(challengeID, answer string) error {
	if p == nil || strings.TrimSpace(challengeID) == "" || strings.TrimSpace(answer) == "" {
		return ErrCaptchaInvalid
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.entries[challengeID]
	if !ok || !entry.expiresAt.After(time.Now()) {
		delete(p.entries, challengeID)
		return ErrCaptchaExpired
	}
	entry.answerHash = sha256.Sum256([]byte(strings.TrimSpace(answer)))
	entry.ready = true
	p.entries[challengeID] = entry
	return nil
}

func (p *MemoryCaptchaProvider) Verify(ctx context.Context, challengeID, answer string) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if p == nil || strings.TrimSpace(challengeID) == "" || strings.TrimSpace(answer) == "" {
		return ErrCaptchaInvalid
	}
	p.mu.Lock()
	entry, ok := p.entries[challengeID]
	delete(p.entries, challengeID)
	p.mu.Unlock()
	if !ok || !entry.ready || !entry.expiresAt.After(time.Now()) {
		return ErrCaptchaExpired
	}
	provided := sha256.Sum256([]byte(strings.TrimSpace(answer)))
	if subtle.ConstantTimeCompare(entry.answerHash[:], provided[:]) != 1 {
		return ErrCaptchaInvalid
	}
	return nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func captchaID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}
