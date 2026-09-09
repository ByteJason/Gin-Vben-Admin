package tasksplatform

import (
	"context"
	"fmt"
	"sort"

	"github.com/ByteJason/Gin-Vben-Admin/server/internal/domain/tenant"
)

// TaskScopes returns distinct scopes that have at least one enabled, live
// definition. It is used only by the privileged background scheduler.
func (r *GORMRepository) TaskScopes(ctx context.Context) ([]tenant.Context, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("task repository unavailable")
	}
	rows, err := r.db.Read(ctx).Table("gvba_task_definitions").Select("tenant_id, org_id").Where("deleted_at IS NULL AND enabled = ?", true).Distinct().Order("tenant_id ASC, org_id ASC").Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []tenant.Context
	for rows.Next() {
		var tenantID, orgID string
		if err := rows.Scan(&tenantID, &orgID); err != nil {
			return nil, err
		}
		scope, err := tenant.NewContext(tenantID, orgID, false)
		if err != nil {
			return nil, err
		}
		out = append(out, scope)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TenantID == out[j].TenantID {
			return out[i].Organization < out[j].Organization
		}
		return out[i].TenantID < out[j].TenantID
	})
	return out, nil
}
