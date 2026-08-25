package iam

import (
	"context"
	"errors"
	"sort"
	"strings"
)

// Component describes one server-approved frontend component. Menu writes
// must reference one of these values; arbitrary module paths are never
// accepted from a request.
type Component struct {
	ID        string `json:"id"`
	Component string `json:"component"`
	Label     string `json:"label"`
	Kind      string `json:"kind"`
}

var ErrComponentNotRegistered = errors.New("iam component is not registered")

// ComponentRegistry is intentionally read-only. Deployments can replace the
// implementation at composition time, while the HTTP layer only exposes the
// bounded list and validation seam.
type ComponentRegistry interface {
	List(context.Context) ([]Component, error)
	Resolve(string) (Component, bool)
}

type StaticComponentRegistry struct {
	entries []Component
	byValue map[string]Component
}

// NewStaticComponentRegistry returns the allowlist shared by the three admin
// templates. Paths are normalized to the backend route converter's format.
func NewStaticComponentRegistry() *StaticComponentRegistry {
	entries := []Component{
		{ID: "layout.basic", Component: "BasicLayout", Label: "Basic layout", Kind: "layout"},
		{ID: "layout.iframe", Component: "IFrameView", Label: "Iframe layout", Kind: "layout"},
		{ID: "dashboard.analytics", Component: "/dashboard/analytics/index.vue", Label: "Analytics", Kind: "page"},
		{ID: "dashboard.workspace", Component: "/dashboard/workspace/index.vue", Label: "Workspace", Kind: "page"},
		{ID: "iam.users", Component: "/iam/users/index.vue", Label: "Users", Kind: "page"},
		{ID: "iam.roles", Component: "/iam/roles/index.vue", Label: "Roles", Kind: "page"},
		{ID: "iam.menus", Component: "/iam/menus/index.vue", Label: "Menus", Kind: "page"},
		{ID: "iam.permissions", Component: "/iam/permissions/index.vue", Label: "Permissions", Kind: "page"},
		{ID: "iam.policies", Component: "/iam/policies/index.vue", Label: "Policies", Kind: "page"},
		{ID: "iam.data-scopes", Component: "/iam/data-scopes/index.vue", Label: "Data scopes", Kind: "page"},
		{ID: "system.settings", Component: "/system/settings/index.vue", Label: "Settings", Kind: "page"},
		{ID: "system.dictionary", Component: "/system/dictionary/index.vue", Label: "Dictionary", Kind: "page"},
		{ID: "system.mail", Component: "/system/mail/index.vue", Label: "Mail", Kind: "page"},
		{ID: "system.files", Component: "/system/files/index.vue", Label: "Files", Kind: "page"},
		{ID: "system.observability", Component: "/system/observability/index.vue", Label: "Observability", Kind: "page"},
		{ID: "system.monitor", Component: "/system/monitor/index.vue", Label: "Monitor", Kind: "page"},
		{ID: "system.audit", Component: "/system/audit/index.vue", Label: "Audit", Kind: "page"},
		{ID: "system.tasks", Component: "/system/tasks/index.vue", Label: "Tasks", Kind: "page"},
		{ID: "system.import-export", Component: "/system/import-export/index.vue", Label: "Import/export", Kind: "page"},
	}
	byValue := make(map[string]Component, len(entries))
	for _, entry := range entries {
		byValue[entry.Component] = entry
	}
	return &StaticComponentRegistry{entries: entries, byValue: byValue}
}

func (r *StaticComponentRegistry) List(ctx context.Context) ([]Component, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	if r == nil {
		return nil, ErrRepositoryMissing
	}
	out := append([]Component(nil), r.entries...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r *StaticComponentRegistry) Resolve(value string) (Component, bool) {
	if r == nil {
		return Component{}, false
	}
	entry, ok := r.byValue[strings.TrimSpace(value)]
	return entry, ok
}

func (r *StaticComponentRegistry) Validate(value string) error {
	if _, ok := r.Resolve(value); !ok {
		return ErrComponentNotRegistered
	}
	return nil
}
