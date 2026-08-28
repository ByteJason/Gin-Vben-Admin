package installer

const (
	initialAdminPasswordMinLength = 6
	initialAdminPasswordMaxLength = 128
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
