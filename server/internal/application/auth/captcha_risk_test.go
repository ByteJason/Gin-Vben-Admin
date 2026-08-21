package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryCaptchaRiskStoreRequiresAfterThresholdAndResets(t *testing.T) {
	store := NewMemoryCaptchaRiskStore()
	ctx := context.Background()
	key := "alice|192.0.2.10"
	if required, err := store.Requires(ctx, key, 2, time.Minute); err != nil || required {
		t.Fatalf("initial captcha risk = %v, %v", required, err)
	}
	if err := store.RecordFailure(ctx, key, time.Minute); err != nil {
		t.Fatal(err)
	}
	if required, err := store.Requires(ctx, key, 2, time.Minute); err != nil || required {
		t.Fatalf("first failure captcha risk = %v, %v", required, err)
	}
	if err := store.RecordFailure(ctx, key, time.Minute); err != nil {
		t.Fatal(err)
	}
	if required, err := store.Requires(ctx, key, 2, time.Minute); err != nil || !required {
		t.Fatalf("threshold captcha risk = %v, %v", required, err)
	}
	if err := store.Reset(ctx, key); err != nil {
		t.Fatal(err)
	}
	if required, err := store.Requires(ctx, key, 2, time.Minute); err != nil || required {
		t.Fatalf("reset captcha risk = %v, %v", required, err)
	}
}

func TestMemoryCaptchaRiskStoreValidatesContextAndPolicy(t *testing.T) {
	store := NewMemoryCaptchaRiskStore()
	if _, err := store.Requires(context.Background(), "", 2, time.Minute); !errors.Is(err, ErrInvalidCaptchaRiskKey) {
		t.Fatalf("empty key error = %v", err)
	}
	if _, err := store.Requires(context.Background(), "alice", 0, time.Minute); !errors.Is(err, ErrInvalidCaptchaRiskPolicy) {
		t.Fatalf("invalid threshold error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.RecordFailure(ctx, "alice", time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v", err)
	}
}
