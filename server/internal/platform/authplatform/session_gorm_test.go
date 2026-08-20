package authplatform

import (
	"context"
	"testing"
	"time"

	"example.com/gin-vben-admin/server/internal/domain/authdomain"
)

func TestGORMSessionStoreImplementsSessionStore(t *testing.T) {
	var _ authdomain.SessionStore = NewGORMSessionStore(nil)
	store := NewGORMSessionStore(nil)
	if _, err := store.Get(context.Background(), "missing"); err == nil {
		t.Fatal("Get() with an uninitialized store returned nil error")
	}
	_ = time.Second // keep the seam test's time dependency explicit
}
