package file

import (
	"context"
	"time"
)

// FileRepository is the durable metadata authority for file operations. The
// service falls back to its legacy in-memory index only when no repository is
// configured (development fixtures); production wiring always supplies one.
type FileRepository interface {
	Create(context.Context, File) error
	Get(context.Context, string) (File, error)
	List(context.Context, ListFilter) (Page, error)
	Update(context.Context, File) error
	MarkStatus(context.Context, string, MediaStatus, string, *time.Time) error
}

type StatusRepository interface {
	FileRepository
	ListByStatus(context.Context, MediaStatus, int) ([]File, error)
}

// UsageRepository is an optional durable reference checker used by delete
// workflows. Implementations query media_usages without exposing provider
// object keys to callers.
type UsageRepository interface {
	CountByResource(context.Context, string) (int64, error)
}

// DeletionRepository atomically checks media_usages and moves a file row into
// deleting state. Durable adapters use a row lock so a usage attachment and a
// deletion request cannot pass one another between separate queries.
type DeletionRepository interface {
	RequestDeletion(context.Context, string, bool, time.Time) error
}
