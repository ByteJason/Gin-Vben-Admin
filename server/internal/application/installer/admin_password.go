package installer

const (
	initialAdminPasswordMinLength = 6
	// bcrypt rejects inputs longer than 72 bytes. The installer accepts ASCII
	// only, so the byte and character limits are identical.
	initialAdminPasswordMaxLength = 72
)

// IsValidInitialAdminPassword reports whether password satisfies the policy for
// the administrator account created during installation.
func IsValidInitialAdminPassword(password string) bool {
	if len(password) < initialAdminPasswordMinLength || len(password) > initialAdminPasswordMaxLength {
		return false
	}

	var hasLetter, hasDigit bool
	for index := 0; index < len(password); index++ {
		character := password[index]
		switch {
		case character >= 'a' && character <= 'z', character >= 'A' && character <= 'Z':
			hasLetter = true
		case character >= '0' && character <= '9':
			hasDigit = true
		default:
			return false
		}
	}
	return hasLetter && hasDigit
}
