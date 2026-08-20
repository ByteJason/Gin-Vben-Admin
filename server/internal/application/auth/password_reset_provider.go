package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"sync"
	"time"

	"example.com/gin-vben-admin/server/internal/domain/authdomain"
)

// PasswordResetDelivery receives a newly generated token and is responsible
// for delivering it through an out-of-band channel. It must not log tokens.
type PasswordResetDelivery func(context.Context, string, string) error

type memoryResetEntry struct {
	identifier string
	expiresAt  time.Time
}

// MemoryPasswordResetProvider is a deterministic provider seam for local
// development and unit tests. Production deployments should supply a durable
// provider with an email/SMS delivery implementation.
type MemoryPasswordResetProvider struct {
	mu      sync.Mutex
	ttl     time.Duration
	deliver PasswordResetDelivery
	tokens  map[[32]byte]memoryResetEntry
	now     func() time.Time
}

func NewMemoryPasswordResetProvider(ttl time.Duration, deliver PasswordResetDelivery) *MemoryPasswordResetProvider {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	return &MemoryPasswordResetProvider{
		ttl:     ttl,
		deliver: deliver,
		tokens:  make(map[[32]byte]memoryResetEntry),
		now:     time.Now,
	}
}

// Request creates a one-time token, stores only its digest, and invokes the
// configured delivery callback. Failed delivery removes the token.
func (p *MemoryPasswordResetProvider) Request(ctx context.Context, identifier string) error {
	if p == nil || p.deliver == nil || identifier == "" {
		return authdomain.ErrDependencyUnavailable
	}
	token, digest, err := newResetToken()
	if err != nil {
		return authdomain.ErrDependencyUnavailable
	}
	now := time.Now()
	if p.now != nil {
		now = p.now()
	}
	p.mu.Lock()
	for key, entry := range p.tokens {
		if entry.identifier == identifier {
			delete(p.tokens, key)
		}
	}
	p.tokens[digest] = memoryResetEntry{identifier: identifier, expiresAt: now.Add(p.ttl)}
	p.mu.Unlock()
	if err := p.deliver(ctx, identifier, token); err != nil {
		p.mu.Lock()
		delete(p.tokens, digest)
		p.mu.Unlock()
		return err
	}
	return nil
}

// Consume atomically deletes a token before returning its identifier.
func (p *MemoryPasswordResetProvider) Consume(_ context.Context, token string) (string, error) {
	if p == nil || token == "" {
		return "", authdomain.ErrPasswordResetInvalid
	}
	digest := sha256.Sum256([]byte(token))
	p.mu.Lock()
	entry, ok := p.tokens[digest]
	delete(p.tokens, digest)
	p.mu.Unlock()
	if !ok {
		return "", authdomain.ErrPasswordResetInvalid
	}
	now := time.Now()
	if p.now != nil {
		now = p.now()
	}
	if !entry.expiresAt.After(now) {
		return "", authdomain.ErrPasswordResetInvalid
	}
	return entry.identifier, nil
}

func newResetToken() (string, [32]byte, error) {
	var digest [32]byte
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", digest, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	digest = sha256.Sum256([]byte(token))
	return token, digest, nil
}

var _ PasswordResetProvider = (*MemoryPasswordResetProvider)(nil)
