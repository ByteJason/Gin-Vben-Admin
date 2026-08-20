// Package migrations exposes the versioned schema assets bundled with the server.
package migrations

import "embed"

// FS contains the MySQL and PostgreSQL migration trees.
//
//go:embed mysql/*.sql postgres/*.sql
var FS embed.FS
