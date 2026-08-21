package authdomain

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeIdentifierClassifiesAndCanonicalizesUsernameOrEmail(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      string
		wantType  IdentifierType
		wantError bool
	}{
		{name: "username trims and lowercases", input: "  Alice  ", want: "alice", wantType: IdentifierUsername},
		{name: "email trims and lowercases", input: "  Alice@Example.TEST  ", want: "alice@example.test", wantType: IdentifierEmail},
		{name: "phone is not an authentication identifier", input: "+8613800138000", wantError: true},
		{name: "empty is invalid", input: "   ", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotType, err := NormalizeIdentifier(tt.input)
			if tt.wantError {
				if err == nil {
					t.Fatalf("NormalizeIdentifier(%q) error = nil, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeIdentifier(%q) error = %v", tt.input, err)
			}
			if got != tt.want || gotType != tt.wantType {
				t.Fatalf("NormalizeIdentifier(%q) = (%q, %q), want (%q, %q)", tt.input, got, gotType, tt.want, tt.wantType)
			}
		})
	}
}

func TestNormalizeIdentifierRejectsUsernameLongerThanStorageLimit(t *testing.T) {
	if _, kind, err := NormalizeIdentifier(strings.Repeat("u", 192)); !errors.Is(err, ErrInvalidIdentifier) || kind != "" {
		t.Fatalf("long username result = kind:%q err:%v", kind, err)
	}
}

func TestNormalizePhoneAcceptsE164AndRejectsMalformedValues(t *testing.T) {
	if got, err := NormalizePhone(" +8613800138000 "); err != nil || got != "+8613800138000" {
		t.Fatalf("NormalizePhone(valid) = %q, %v", got, err)
	}
	for _, value := range []string{"13800138000", "+012345678", "+123", "+1234567890123456", "+86138-00138000"} {
		if _, err := NormalizePhone(value); !errors.Is(err, ErrInvalidPhone) {
			t.Fatalf("NormalizePhone(%q) error = %v, want ErrInvalidPhone", value, err)
		}
	}
}
