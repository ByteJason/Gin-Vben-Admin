package gormquery

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CreateValues inserts one typed record while preserving the caller's zero
// values. GORM's struct Create callback applies a model's `default` tag to a
// zero-valued field before it builds the INSERT (for example false -> true),
// which is useful for database defaults but wrong when a repository has
// already normalized an explicit value. Set keeps the operation on the
// gorm.G[T] API while making every supplied column explicit.
func CreateValues[T any](ctx context.Context, db *gorm.DB, values map[string]any) error {
	if db == nil {
		return gorm.ErrInvalidDB
	}
	if len(values) == 0 {
		return gorm.ErrInvalidData
	}
	return gorm.G[T](db).Set(clause.Assignments(values)).Create(ctx)
}
