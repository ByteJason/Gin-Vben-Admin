package migrations_test

import "testing"

func TestAuthSchemaIncludesIdentifiersAndProfileFields(t *testing.T) {
	requireFields(t, "users", "username", "username_normalized", "email", "email_normalized", "nickname", "avatar", "phone", "last_login_ip", "last_login_at", "password_changed_at")
	requireFields(t, "users", "locked_until")
	requireFields(t, "auth_sessions", "refresh_token_hash", "family_id")
}
