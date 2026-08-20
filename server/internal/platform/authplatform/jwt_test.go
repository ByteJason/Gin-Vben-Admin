package authplatform

import (
	"errors"
	"strings"
	"testing"
	"time"

	"example.com/gin-vben-admin/server/internal/domain/authdomain"
)

func TestJWTServiceRejectsTamperingAndWrongAlgorithm(t *testing.T) {
	s := NewJWTService([]byte("secret"), time.Minute, time.Hour)
	pair, err := s.Issue("u1", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Parse(pair.AccessToken); err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	parts := strings.Split(pair.AccessToken, ".")
	parts[1] = "A" + parts[1][1:]
	if _, err := s.Parse(strings.Join(parts, ".")); !errors.Is(err, authdomain.ErrInvalidToken) {
		t.Fatalf("tampered Parse() = %v", err)
	}

	// A valid signature with a header claiming a different algorithm is rejected.
	parts = strings.Split(pair.AccessToken, ".")
	parts[0] = "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9"
	if _, err := s.Parse(strings.Join(parts, ".")); !errors.Is(err, authdomain.ErrInvalidToken) {
		t.Fatalf("wrong-algorithm Parse() = %v", err)
	}
}
