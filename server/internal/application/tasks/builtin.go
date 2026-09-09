package tasks

import (
	"context"
	"encoding/json"
	"fmt"
)

// Inventory is a read-only built-in operational check. Its output comes from
// the current tenant's repository, never from seeded demonstration values.
func (s *Service) Inventory(ctx context.Context, _ json.RawMessage) (ExecutionResult, error) {
	items, err := s.List(ctx)
	if err != nil {
		return ExecutionResult{}, err
	}
	enabled := 0
	for _, d := range items {
		if d.Enabled {
			enabled++
		}
	}
	output, err := json.Marshal(map[string]int{"total": len(items), "enabled": enabled, "disabled": len(items) - enabled})
	return ExecutionResult{ResultSummary: fmt.Sprintf("Checked %d task definitions", len(items)), RedactedOutput: string(output)}, err
}
