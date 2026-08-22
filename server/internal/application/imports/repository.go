package imports

import "context"

// Repository is the durable job seam. The in-memory Service remains the
// default local adapter; database-backed composition roots can supply a GORM
// implementation without changing HTTP or worker contracts.
type Repository interface {
	Create(context.Context, Job) (Job, error)
	Get(context.Context, string, string, string) (Job, error)
	List(context.Context, string, string, string) ([]Job, error)
	Update(context.Context, Job) (Job, error)
	AddErrors(context.Context, string, []RowError) error
	ListErrors(context.Context, string, string, string) ([]RowError, error)
}
