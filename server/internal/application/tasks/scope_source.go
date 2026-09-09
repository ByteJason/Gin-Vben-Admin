package tasks

import (
	"context"
	"sort"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

// TaskScopes enumerates enabled definitions stored by the in-memory adapter.
// It is intentionally an adapter method rather than part of Repository so
// request-scoped CRUD implementations do not need a privileged operation.
func (r *MemoryRepository) TaskScopes(ctx context.Context) ([]tenant.Context, error) {
	if err := contextErr(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := map[string]tenant.Context{}
	for _, d := range r.data {
		if d.DeletedAt != nil || !d.Enabled {
			continue
		}
		scope, err := tenant.NewContext(d.TenantID, d.OrgID, false)
		if err != nil {
			return nil, err
		}
		seen[scope.TenantID+"\x00"+scope.Organization] = scope
	}
	out := make([]tenant.Context, 0, len(seen))
	for _, scope := range seen {
		out = append(out, scope)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TenantID == out[j].TenantID {
			return out[i].Organization < out[j].Organization
		}
		return out[i].TenantID < out[j].TenantID
	})
	return out, nil
}
