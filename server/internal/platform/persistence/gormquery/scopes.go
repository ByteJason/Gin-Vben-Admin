// Package gormquery contains small, composable query scopes shared by the
// persistence repositories.  The callbacks use GORM's generics signature so
// callers can build a typed chain without falling back to *gorm.DB helpers.
package gormquery

import (
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Tenant adds the mandatory tenant predicate. Values are passed as clause
// variables, never interpolated into SQL.
func Tenant(tenantID string) func(*gorm.Statement) {
	return Where("tenant_id", tenantID)
}

// Organization adds an organization predicate when a non-empty organization
// is supplied. Empty organization means tenant-wide scope.
func Organization(orgID string) func(*gorm.Statement) {
	orgID = strings.TrimSpace(orgID)
	return func(stmt *gorm.Statement) {
		if orgID == "" {
			return
		}
		Where("org_id", orgID)(stmt)
	}
}

// Active restricts rows to the conventional active status.
func Active() func(*gorm.Statement) { return Where("status", "active") }

// NotDeleted restricts soft-deletable rows to live records.
func NotDeleted() func(*gorm.Statement) {
	return func(stmt *gorm.Statement) {
		stmt.AddClause(clause.Where{Exprs: []clause.Expression{
			clause.Eq{Column: clause.Column{Name: "deleted_at"}, Value: nil},
		}})
	}
}

// Where returns a parameterized equality predicate for a trusted column name.
// Callers should pass a compile-time column or an identifier selected from a
// local allowlist; the value is always bound by GORM.
func Where(column string, value any) func(*gorm.Statement) {
	return func(stmt *gorm.Statement) {
		stmt.AddClause(clause.Where{Exprs: []clause.Expression{
			clause.Eq{Column: clause.Column{Name: column}, Value: value},
		}})
	}
}

// WhereNotEqual adds a parameterized inequality predicate.
func WhereNotEqual(column string, value any) func(*gorm.Statement) {
	return func(stmt *gorm.Statement) {
		stmt.AddClause(clause.Where{Exprs: []clause.Expression{
			clause.Neq{Column: clause.Column{Name: column}, Value: value},
		}})
	}
}

// WhereIn adds a parameterized IN predicate. An empty list intentionally adds
// a false condition rather than emitting invalid SQL or broadening the query.
func WhereIn(column string, values []any) func(*gorm.Statement) {
	copyValues := append([]any(nil), values...)
	return func(stmt *gorm.Statement) {
		if len(copyValues) == 0 {
			stmt.AddClause(clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "1 = 0"}}})
			return
		}
		stmt.AddClause(clause.Where{Exprs: []clause.Expression{
			clause.IN{Column: clause.Column{Name: column}, Values: copyValues},
		}})
	}
}
