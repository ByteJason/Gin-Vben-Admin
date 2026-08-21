package authdomain

import (
	"errors"
	"strings"
	"unicode"
)

// IdentifierType is the only authentication identifier family supported by
// the management API. Phone numbers remain profile data and never enter this
// classifier.
type IdentifierType string

const (
	IdentifierUsername IdentifierType = "username"
	IdentifierEmail    IdentifierType = "email"
)

var ErrInvalidIdentifier = errors.New("invalid authentication identifier")
var ErrInvalidPhone = errors.New("invalid profile phone")

// NormalizeIdentifier trims and canonicalizes an authentication identifier.
// Usernames and email addresses are compared case-insensitively within a
// tenant; the original display casing is retained separately on the profile.
func NormalizeIdentifier(value string) (string, IdentifierType, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len([]byte(value)) > 254 {
		return "", "", ErrInvalidIdentifier
	}
	if strings.Contains(value, "@") {
		if !validEmailIdentifier(value) {
			return "", "", ErrInvalidIdentifier
		}
		return value, IdentifierEmail, nil
	}
	if !validUsernameIdentifier(value) {
		return "", "", ErrInvalidIdentifier
	}
	return value, IdentifierUsername, nil
}

func validEmailIdentifier(value string) bool {
	if len(value) > 254 || strings.Count(value, "@") != 1 {
		return false
	}
	parts := strings.SplitN(value, "@", 2)
	if parts[0] == "" || parts[1] == "" || strings.HasPrefix(parts[0], ".") || strings.HasSuffix(parts[0], ".") || strings.Contains(parts[0], "..") {
		return false
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return strings.Contains(parts[1], ".") && !strings.HasPrefix(parts[1], ".") && !strings.HasSuffix(parts[1], ".") && !strings.Contains(parts[1], "..")
}

func validUsernameIdentifier(value string) bool {
	if value == "" || len([]byte(value)) > 191 || strings.HasPrefix(value, "+") {
		return false
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}

// NormalizePhone validates the optional profile phone without making it an
// authentication identifier. The returned value is the trimmed E.164 form.
func NormalizePhone(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if len(value) < 9 || len(value) > 16 || value[0] != '+' || value[1] == '0' {
		return "", ErrInvalidPhone
	}
	for _, r := range value[1:] {
		if r < '0' || r > '9' {
			return "", ErrInvalidPhone
		}
	}
	return value, nil
}
