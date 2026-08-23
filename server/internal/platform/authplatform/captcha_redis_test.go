package authplatform

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	appauth "github.com/ByteJason/Gin-Vben-Admin/server/internal/application/auth"
	rediscache "github.com/ByteJason/Gin-Vben-Admin/server/internal/platform/cache/redis"
)

func TestRedisCaptchaProviderIntegration(t *testing.T) {
	if os.Getenv("REDIS_INTEGRATION") != "1" {
		t.Skip("set REDIS_INTEGRATION=1 to run against the local Redis fixture")
	}
	cache, err := rediscache.New(rediscache.Config{
		Mode: rediscache.ModeSingle, Addr: "127.0.0.1:6379",
		Namespace: "app:test-captcha-" + time.Now().UTC().Format("20060102150405.000000000"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cache.Close() })
	provider := NewRedisCaptchaProvider(cache, "auth-captcha", time.Minute)
	provider.answerGenerator = func() (string, error) { return "4821", nil }
	provider.idGenerator = func() (string, error) { return "integration-challenge", nil }
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	challenge, err := provider.Issue(ctx)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	challengeKey, err := provider.challengeKey(challenge.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cache.Delete(context.Background(), challengeKey) })
	if err := provider.Verify(ctx, challenge.ID, "4821"); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if err := provider.Verify(ctx, challenge.ID, "4821"); !errors.Is(err, appauth.ErrCaptchaExpired) {
		t.Fatalf("Verify(replay) error = %v, want ErrCaptchaExpired", err)
	}

	risk := NewRedisCaptchaRiskStore(cache, "auth-captcha")
	logical := "integration-risk"
	riskKey, err := risk.riskKey(logical)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cache.Delete(context.Background(), riskKey) })
	if err := risk.RecordFailure(ctx, logical, time.Minute); err != nil {
		t.Fatalf("RecordFailure() error = %v", err)
	}
	if required, err := risk.Requires(ctx, logical, 1, time.Minute); err != nil || !required {
		t.Fatalf("Requires() = %v, %v", required, err)
	}
}

type captchaCacheFixture struct {
	values     map[string][]byte
	ttls       map[string]time.Duration
	increments map[string]int64
	deleted    []string
}

func newCaptchaCacheFixture() *captchaCacheFixture {
	return &captchaCacheFixture{
		values: make(map[string][]byte), ttls: make(map[string]time.Duration),
		increments: make(map[string]int64),
	}
}

func (f *captchaCacheFixture) Key(parts ...string) (string, error) {
	return "app:v1:" + strings.Join(parts, ":"), nil
}

func (f *captchaCacheFixture) SetJSON(_ context.Context, key string, value any, ttl time.Duration) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	f.values[key] = append([]byte(nil), payload...)
	f.ttls[key] = ttl
	return nil
}

func (f *captchaCacheFixture) GetJSON(_ context.Context, key string, destination any) error {
	payload, ok := f.values[key]
	if !ok {
		return rediscache.ErrCacheMiss
	}
	return json.Unmarshal(payload, destination)
}

func (f *captchaCacheFixture) TakeJSON(_ context.Context, key string, destination any) error {
	payload, ok := f.values[key]
	if !ok {
		return rediscache.ErrCacheMiss
	}
	delete(f.values, key)
	return json.Unmarshal(payload, destination)
}

func (f *captchaCacheFixture) Delete(_ context.Context, key string) error {
	delete(f.values, key)
	f.deleted = append(f.deleted, key)
	return nil
}

func (f *captchaCacheFixture) Increment(_ context.Context, key string, _ time.Duration) (int64, error) {
	f.increments[key]++
	payload, err := json.Marshal(f.increments[key])
	if err != nil {
		return 0, err
	}
	f.values[key] = payload
	return f.increments[key], nil
}

func TestRedisCaptchaProviderIssuesImageAndStoresOnlyHash(t *testing.T) {
	cache := newCaptchaCacheFixture()
	provider := newRedisCaptchaProviderForTest(cache, "auth-captcha", time.Minute, "4821", "challenge-1")

	challenge, err := provider.Issue(context.Background())
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if challenge.ID != "challenge-1" || challenge.Kind != "image" || !strings.HasPrefix(challenge.Payload, "data:image/svg+xml;base64,") {
		t.Fatalf("challenge = %#v", challenge)
	}
	encoded := strings.TrimPrefix(challenge.Payload, "data:image/svg+xml;base64,")
	rendered, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || !strings.Contains(string(rendered), "4821") {
		t.Fatalf("image payload did not render the generated answer: err=%v payload=%s", err, rendered)
	}

	challengeKey, err := provider.challengeKey("challenge-1")
	if err != nil {
		t.Fatalf("challenge key: %v", err)
	}
	var stored map[string]any
	if err := json.Unmarshal(cache.values[challengeKey], &stored); err != nil {
		t.Fatalf("stored challenge JSON: %v", err)
	}
	if _, raw := stored["answer"]; raw {
		t.Fatal("challenge stored a raw answer")
	}
	if stored["answer_hash"] != sha256Hex("4821") {
		t.Fatalf("stored challenge hash = %#v", stored["answer_hash"])
	}
	if strings.Contains(string(cache.values[challengeKey]), "4821") {
		t.Fatal("stored challenge JSON contains the raw answer")
	}

	if err := provider.Verify(context.Background(), challenge.ID, "4821"); err != nil {
		t.Fatalf("Verify(correct) error = %v", err)
	}
	if err := provider.Verify(context.Background(), challenge.ID, "4821"); !errors.Is(err, appauth.ErrCaptchaExpired) {
		t.Fatalf("Verify(replay) error = %v, want ErrCaptchaExpired", err)
	}
}

func TestRedisCaptchaProviderConsumesChallengeAfterWrongAnswer(t *testing.T) {
	cache := newCaptchaCacheFixture()
	provider := newRedisCaptchaProviderForTest(cache, "auth-captcha", time.Minute, "4821", "challenge-2")
	challenge, err := provider.Issue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.Verify(context.Background(), challenge.ID, "0000"); !errors.Is(err, appauth.ErrCaptchaInvalid) {
		t.Fatalf("Verify(wrong) error = %v, want ErrCaptchaInvalid", err)
	}
	if err := provider.Verify(context.Background(), challenge.ID, "4821"); !errors.Is(err, appauth.ErrCaptchaExpired) {
		t.Fatalf("Verify(after wrong) error = %v, want ErrCaptchaExpired", err)
	}
}

func TestRedisCaptchaRiskStoreHashesKeysAndUsesAtomicCounter(t *testing.T) {
	cache := newCaptchaCacheFixture()
	store := newRedisCaptchaRiskStore(cache, "auth-captcha")
	logical := "Alice@Example.test|203.0.113.8"
	ctx := context.Background()

	if required, err := store.Requires(ctx, logical, 2, time.Minute); err != nil || required {
		t.Fatalf("Requires(before) = %v, %v", required, err)
	}
	if err := store.RecordFailure(ctx, logical, time.Minute); err != nil {
		t.Fatalf("RecordFailure(first) error = %v", err)
	}
	if required, err := store.Requires(ctx, logical, 2, time.Minute); err != nil || required {
		t.Fatalf("Requires(after first) = %v, %v", required, err)
	}
	if err := store.RecordFailure(ctx, logical, time.Minute); err != nil {
		t.Fatalf("RecordFailure(second) error = %v", err)
	}
	if required, err := store.Requires(ctx, logical, 2, time.Minute); err != nil || !required {
		t.Fatalf("Requires(after threshold) = %v, %v", required, err)
	}
	for key := range cache.increments {
		if strings.Contains(key, logical) {
			t.Fatalf("risk key leaked logical identifier: %q", key)
		}
	}
	if err := store.Reset(ctx, logical); err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	if required, err := store.Requires(ctx, logical, 2, time.Minute); err != nil || required {
		t.Fatalf("Requires(after reset) = %v, %v", required, err)
	}
}

func TestRedisCaptchaProviderRejectsInvalidConfiguration(t *testing.T) {
	cache := newCaptchaCacheFixture()
	for name, provider := range map[string]*RedisCaptchaProvider{
		"missing cache": newRedisCaptchaProviderForTest(nil, "auth-captcha", time.Minute, "4821", "challenge"),
		"invalid ttl":   newRedisCaptchaProviderForTest(cache, "auth-captcha", 0, "4821", "challenge"),
		"unsafe prefix": newRedisCaptchaProviderForTest(cache, "auth:captcha", time.Minute, "4821", "challenge"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := provider.Issue(context.Background()); err == nil {
				t.Fatal("Issue() error = nil")
			}
		})
	}
}

func TestRedisCaptchaProviderHonorsCanceledContext(t *testing.T) {
	cache := newCaptchaCacheFixture()
	provider := newRedisCaptchaProviderForTest(cache, "auth-captcha", time.Minute, "4821", "challenge")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := provider.Issue(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Issue(canceled) error = %v, want context.Canceled", err)
	}
	if err := provider.Verify(ctx, "challenge", "4821"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Verify(canceled) error = %v, want context.Canceled", err)
	}
}

func newRedisCaptchaProviderForTest(cache captchaCache, prefix string, ttl time.Duration, answer, challengeID string) *RedisCaptchaProvider {
	return &RedisCaptchaProvider{
		cache: cache, prefix: prefix, ttl: ttl,
		answerGenerator: func() (string, error) { return answer, nil },
		idGenerator:     func() (string, error) { return challengeID, nil },
	}
}

func sha256Hex(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmtHex(digest[:])
}

func fmtHex(value []byte) string {
	const alphabet = "0123456789abcdef"
	result := make([]byte, len(value)*2)
	for index, current := range value {
		result[index*2] = alphabet[current>>4]
		result[index*2+1] = alphabet[current&0x0f]
	}
	return string(result)
}

var _ captchaCache = (*captchaCacheFixture)(nil)
var _ appauth.CaptchaProvider = (*RedisCaptchaProvider)(nil)
var _ appauth.CaptchaRiskStore = (*RedisCaptchaRiskStore)(nil)
