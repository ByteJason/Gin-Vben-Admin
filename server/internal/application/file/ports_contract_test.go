package file

import (
	"context"
	"io"
)

type portMediaStub struct{}

func (portMediaStub) Upload(context.Context, UploadInput) (ResourceRef, error) {
	return ResourceRef{}, nil
}
func (portMediaStub) Get(context.Context, ResourceID) (ResourceRef, error) { return ResourceRef{}, nil }
func (portMediaStub) List(context.Context, MediaFilter) (MediaPage, error) { return MediaPage{}, nil }
func (portMediaStub) Open(context.Context, ResourceID, OpenOptions) (io.ReadCloser, error) {
	return nil, nil
}
func (portMediaStub) SignedURL(context.Context, ResourceID, URLRequest) (URLRef, error) {
	return URLRef{}, nil
}
func (portMediaStub) Delete(context.Context, ResourceID, DeleteOptions) error { return nil }
func (portMediaStub) ListCategories(context.Context, CategoryFilter) ([]CategoryRef, error) {
	return nil, nil
}
func (portMediaStub) CreateCategory(context.Context, CategoryInput) (CategoryRef, error) {
	return CategoryRef{}, nil
}
func (portMediaStub) UpdateCategory(context.Context, CategoryID, CategoryPatch) (CategoryRef, error) {
	return CategoryRef{}, nil
}
func (portMediaStub) DeleteCategory(context.Context, CategoryDeleteRequest) error    { return nil }
func (portMediaStub) Attach(context.Context, UsageInput) (UsageRef, error)           { return UsageRef{}, nil }
func (portMediaStub) Detach(context.Context, DetachRequest) error                    { return nil }
func (portMediaStub) ListByResource(context.Context, ResourceID) ([]UsageRef, error) { return nil, nil }

var _ MediaCatalog = portMediaStub{}
var _ MediaUsageService = portMediaStub{}
