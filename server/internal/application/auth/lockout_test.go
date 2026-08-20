package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryLoginAttemptStoreLocksAndExpires(t *testing.T) {
	store := NewMemoryLoginAttemptStore(2, time.Minute)
	ctx := context.Background()
	locked, err := store.IsLocked(ctx, "alice")
	if err != nil || locked {
		t.Fatalf("initial lock = %v, err=%v", locked, err)
	}
	if locked, err = store.RecordFailure(ctx, "alice"); err != nil || locked {
		t.Fatalf("first failure lock = %v, err=%v", locked, err)
	}
	if locked, err = store.RecordFailure(ctx, "alice"); err != nil || !locked {
		t.Fatalf("threshold failure lock = %v, err=%v", locked, err)
	}
	if locked, err = store.IsLocked(ctx, "alice"); err != nil || !locked {
		t.Fatalf("locked state = %v, err=%v", locked, err)
	}
	if err := store.Reset(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if locked, err = store.IsLocked(ctx, "alice"); err != nil || locked {
		t.Fatalf("reset lock = %v, err=%v", locked, err)
	}
}

func TestMemoryLoginAttemptStoreValidatesPolicyAndContext(t *testing.T) {
	if _, err := NewMemoryLoginAttemptStore(0, time.Minute).RecordFailure(context.Background(), "alice"); !errors.Is(err, ErrInvalidLoginAttemptPolicy) {
		t.Fatalf("invalid threshold error = %v", err)
	}
	store := NewMemoryLoginAttemptStore(2, time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.IsLocked(ctx, "alice"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context error = %v", err)
	}
}
