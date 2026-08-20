package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryCaptchaIsOneTimeAndExpires(t *testing.T) {
	provider := NewMemoryCaptchaProvider(50 * time.Millisecond)
	challenge, err := provider.Issue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.PutAnswer(challenge.ID, "4821"); err != nil {
		t.Fatal(err)
	}
	if err := provider.Verify(context.Background(), challenge.ID, "4821"); err != nil {
		t.Fatalf("valid captcha error = %v", err)
	}
	if err := provider.Verify(context.Background(), challenge.ID, "4821"); !errors.Is(err, ErrCaptchaExpired) {
		t.Fatalf("replay captcha error = %v, want expired", err)
	}

	expired, err := provider.Issue(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := provider.PutAnswer(expired.ID, "1234"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(70 * time.Millisecond)
	if err := provider.Verify(context.Background(), expired.ID, "1234"); !errors.Is(err, ErrCaptchaExpired) {
		t.Fatalf("expired captcha error = %v, want expired", err)
	}
}
