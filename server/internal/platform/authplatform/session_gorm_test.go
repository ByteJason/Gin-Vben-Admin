package authplatform

import (
	"context"
	"testing"
	"time"

	appauth "example.com/gin-vben-admin/server/internal/application/auth"
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

func TestGORMSessionStoreExposesUserScopedDeviceOperations(t *testing.T) {
	var _ appauth.SessionQuery = NewGORMSessionStore(nil)
	store := NewGORMSessionStore(nil)
	if _, err := store.ListByUser(context.Background(), "1"); err == nil {
		t.Fatal("ListByUser() with an uninitialized store returned nil error")
	}
	if err := store.RevokeOwned(context.Background(), "1", "session-1"); err == nil {
		t.Fatal("RevokeOwned() with an uninitialized store returned nil error")
	}
	_ = authdomain.Session{DeviceID: "device-1", DeviceName: "Browser", IPAddress: "127.0.0.1", UserAgent: "UA"}
}
