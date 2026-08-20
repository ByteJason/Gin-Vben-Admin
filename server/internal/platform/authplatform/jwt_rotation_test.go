package authplatform

import (
	"testing"
	"time"
)

func TestRotatingJWTServiceAcceptsPreviousKeyDuringGraceWindow(t *testing.T) {
	service, err := NewRotatingJWTService("key-a", []byte("a-secret"), time.Minute, time.Hour, "ISSUER", "AUDIENCE", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	oldPair, err := service.Issue("7", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Rotate("key-b", []byte("b-secret")); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Parse(oldPair.AccessToken); err != nil {
		t.Fatalf("old token rejected during grace window: %v", err)
	}
	newPair, err := service.Issue("7", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Parse(newPair.AccessToken); err != nil {
		t.Fatalf("new token rejected: %v", err)
	}
}

func TestRotatingJWTServiceRejectsPreviousKeyAfterGraceWindow(t *testing.T) {
	service, err := NewRotatingJWTService("key-a", []byte("a-secret"), time.Minute, time.Hour, "", "", 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := service.Issue("7", "session-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Rotate("key-b", []byte("b-secret")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, err := service.Parse(pair.AccessToken); err == nil {
		t.Fatal("previous key remained valid after grace window")
	}
}
