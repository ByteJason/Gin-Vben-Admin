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

// PasswordResetRecipientDelivery is the email-aware variant used when a login
// identifier is a username but the profile contains a separate email address.
// The token remains provider-owned and must never be returned by transport.
type PasswordResetRecipientDelivery func(context.Context, string, string, string) error

// PasswordResetRecipientProvider extends the legacy provider seam without
// breaking injected development providers. RequestTo receives the canonical
// identifier and the already-loaded profile recipient.
type PasswordResetRecipientProvider interface {
	PasswordResetProvider
	RequestTo(context.Context, string, string) error
}

type memoryResetEntry struct {
	identifier string
	expiresAt  time.Time
}

// MemoryPasswordResetProvider is a deterministic provider seam for local
// development and unit tests. Production deployments should supply a durable
// provider with an email delivery implementation; phone/SMS is outside this
// version boundary.
type MemoryPasswordResetProvider struct {
	mu        sync.Mutex
	ttl       time.Duration
	deliver   PasswordResetDelivery
	deliverTo PasswordResetRecipientDelivery
	tokens    map[[32]byte]memoryResetEntry
	now       func() time.Time
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

// NewDevelopmentPasswordResetProvider names the explicit no-remote-resource
// fallback used by local fixtures and administrator-led recovery flows.
func NewDevelopmentPasswordResetProvider(ttl time.Duration, deliver PasswordResetDelivery) *MemoryPasswordResetProvider {
	return NewMemoryPasswordResetProvider(ttl, deliver)
}

// NewMemoryPasswordResetProviderWithRecipient keeps the deterministic token
// store while allowing an email-aware delivery callback. It is used by the
// SMTP adapter and remains safe for local fixture injection.
func NewMemoryPasswordResetProviderWithRecipient(ttl time.Duration, deliver PasswordResetRecipientDelivery) *MemoryPasswordResetProvider {
	provider := NewMemoryPasswordResetProvider(ttl, nil)
	provider.deliverTo = deliver
	return provider
}

// Request creates a one-time token, stores only its digest, and invokes the
// configured delivery callback. Failed delivery removes the token.
func (p *MemoryPasswordResetProvider) Request(ctx context.Context, identifier string) error {
	return p.request(ctx, identifier, "")
}

// RequestTo delivers a token to an explicit profile recipient. Legacy
// providers can continue implementing Request only; the recovery service
// selects this method only when the provider advertises the extension.
func (p *MemoryPasswordResetProvider) RequestTo(ctx context.Context, identifier, recipient string) error {
	return p.request(ctx, identifier, recipient)
}

func (p *MemoryPasswordResetProvider) request(ctx context.Context, identifier, recipient string) error {
	if p == nil || identifier == "" || (p.deliver == nil && p.deliverTo == nil) {
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
	var deliveryErr error
	if p.deliverTo != nil {
		if recipient == "" {
			deliveryErr = authdomain.ErrDependencyUnavailable
		} else {
			deliveryErr = p.deliverTo(ctx, identifier, recipient, token)
		}
	} else {
		deliveryErr = p.deliver(ctx, identifier, token)
	}
	if deliveryErr != nil {
		p.mu.Lock()
		delete(p.tokens, digest)
		p.mu.Unlock()
		return deliveryErr
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
var _ PasswordResetRecipientProvider = (*MemoryPasswordResetProvider)(nil)
