package migrations_test

import "testing"

func TestAuthSchemaIncludesIdentifiersAndProfileFields(t *testing.T) {
	requireFields(t, "gvba_iam_users", "username", "username_normalized", "email", "email_normalized", "nickname", "avatar", "phone", "last_login_ip", "last_login_at", "password_changed_at")
	requireFields(t, "gvba_iam_users", "locked_until")
	requireFields(t, "gvba_auth_sessions", "refresh_token_hash", "family_id")
}
