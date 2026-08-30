package iam

import (
	"context"
	"errors"
	"testing"
)

func TestStaticComponentRegistryIsBoundedAndCopiesResults(t *testing.T) {
	registry := NewStaticComponentRegistry()
	entries, err := registry.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 6 {
		t.Fatalf("registry entries=%d, want the shared admin pages", len(entries))
	}
	if _, ok := registry.Resolve("/iam/users/index.vue"); !ok {
		t.Fatal("users page is not registered")
	}
	entries[0].Component = "mutated"
	again, err := registry.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if again[0].Component == "mutated" {
		t.Fatal("registry leaked mutable entry")
	}
	if err := registry.Validate("../arbitrary.vue"); !errors.Is(err, ErrComponentNotRegistered) {
		t.Fatalf("Validate() error=%v", err)
	}
}

func TestServiceUsesComponentRegistryByDefault(t *testing.T) {
	service := NewService(NewMemoryStore())
	entries, err := service.ListComponents(context.Background())
	if err != nil || len(entries) == 0 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	if err := service.ValidateComponent("BasicLayout"); err != nil {
		t.Fatalf("ValidateComponent() error=%v", err)
	}
}

func TestStaticComponentRegistryCoversSeededProductionPages(t *testing.T) {
	registry := NewStaticComponentRegistry()
	for _, component := range []string{
		"/dashboard/analytics/index.vue",
		"/iam/users/index.vue",
		"/iam/roles/index.vue",
		"/iam/menus/index.vue",
		"/iam/permissions/index.vue",
		"/system/settings/index.vue",
		"/system/dictionary/index.vue",
		"/system/mail/index.vue",
		"/system/files/index.vue",
		"/system/observability/index.vue",
		"/system/monitor/index.vue",
		"/system/audit/index.vue",
		"/system/tasks/index.vue",
		"/system/import-export/index.vue",
		"/ops/operation-history/index.vue",
		"/ops/login-logs/index.vue",
	} {
		if err := registry.Validate(component); err != nil {
			t.Errorf("seeded component %q is not registered: %v", component, err)
		}
	}
}
