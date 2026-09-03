package iam

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryAuthorizerDefaultsDenyAndDenyWins(t *testing.T) {
	p := NewMemoryPolicyStore()
	p.Add(Policy{Subject: "u1", Method: "GET", Path: "/items", Effect: EffectAllow})
	p.Add(Policy{Subject: "u1", Method: "GET", Path: "/items", Effect: EffectDeny})
	a := NewAuthorizer(p)
	ok, e := a.Authorize(context.Background(), Subject{UserID: "u1"}, Request{Method: "GET", Path: "/items"})
	if !errors.Is(e, ErrAccessDenied) || ok {
		t.Fatalf("deny wins=%v,%v", ok, e)
	}
	ok, e = a.Authorize(context.Background(), Subject{UserID: "u1"}, Request{Method: "POST", Path: "/items"})
	if !errors.Is(e, ErrAccessDenied) || ok {
		t.Fatalf("default deny=%v,%v", ok, e)
	}
}
func TestMemoryDataScopeResolver(t *testing.T) {
	p := NewMemoryPolicyStore()
	p.Add(DataScope{Subject: "u1", Resource: "orders", Scope: ScopeOwn, IDs: []string{"a"}})
	r := NewMemoryDataScopeResolver(p)
	got, e := r.Resolve(context.Background(), Subject{UserID: "u1"}, "orders")
	if e != nil || got.Scope != ScopeOwn || len(got.IDs) != 1 {
		t.Fatalf("scope=%+v,%v", got, e)
	}
}

func TestAuthorizerMatchesRoleAndDomain(t *testing.T) {
	p := NewMemoryPolicyStore()
	p.Add(Policy{RoleID: "role-reader", Domain: "tenant-a", Method: "GET", Path: "/orders", Effect: EffectAllow})
	a := NewAuthorizer(p)
	ok, err := a.Authorize(context.Background(), Subject{UserID: "u1", RoleIDs: []string{"role-reader"}, Domain: "tenant-a"}, Request{Domain: "tenant-a", Method: "GET", Path: "/orders"})
	if err != nil || !ok {
		t.Fatalf("role policy should allow: ok=%v err=%v", ok, err)
	}
	ok, err = a.Authorize(context.Background(), Subject{UserID: "u1", RoleIDs: []string{"role-reader"}, Domain: "tenant-b"}, Request{Domain: "tenant-b", Method: "GET", Path: "/orders"})
	if !errors.Is(err, ErrAccessDenied) || ok {
		t.Fatalf("cross-domain policy should deny: ok=%v err=%v", ok, err)
	}
}

func TestAuthorizerDenyRoleWinsOverAllowUser(t *testing.T) {
	p := NewMemoryPolicyStore()
	p.Add(Policy{Subject: "u1", Method: "GET", Path: "/orders", Effect: EffectAllow})
	p.Add(Policy{RoleID: "role-blocked", Method: "GET", Path: "/orders", Effect: EffectDeny})
	a := NewAuthorizer(p)
	ok, err := a.Authorize(context.Background(), Subject{UserID: "u1", RoleIDs: []string{"role-blocked"}}, Request{Method: "GET", Path: "/orders"})
	if !errors.Is(err, ErrAccessDenied) || ok {
		t.Fatalf("deny role must win: ok=%v err=%v", ok, err)
	}
}

func TestAuthorizerSupportsActionObjectAliasesAndRejectsDomainMismatch(t *testing.T) {
	p := NewMemoryPolicyStore()
	if err := p.AddPolicy(Policy{Subject: "u1", Domain: "tenant-a", Action: "read", Object: "/orders/:id", Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	a := NewAuthorizer(p)
	ok, err := a.Authorize(context.Background(), Subject{UserID: "u1", Domain: "tenant-a"}, Request{Domain: "tenant-a", Action: "READ", Object: "/orders/42"})
	if err != nil || !ok {
		t.Fatalf("action/object aliases should allow: ok=%v err=%v", ok, err)
	}
	ok, err = a.Authorize(context.Background(), Subject{UserID: "u1", Domain: "tenant-a"}, Request{Domain: "tenant-b", Action: "READ", Object: "/orders/42"})
	if !errors.Is(err, ErrAccessDenied) || ok {
		t.Fatalf("domain mismatch should deny: ok=%v err=%v", ok, err)
	}
}

func TestAuthorizerPathPrefixIncludesCollectionRootAndDescendants(t *testing.T) {
	policies := NewMemoryPolicyStore()
	if err := policies.AddPolicy(Policy{Subject: "u1", Method: "GET", Path: "/api/admin/v1/dictionaries/*", Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	authorizer := NewAuthorizer(policies)
	for _, path := range []string{
		"/api/admin/v1/dictionaries",
		"/api/admin/v1/dictionaries/regions/items",
	} {
		allowed, err := authorizer.Authorize(context.Background(), Subject{UserID: "u1"}, Request{Method: "GET", Path: path})
		if err != nil || !allowed {
			t.Fatalf("prefix policy should allow %q: allowed=%v err=%v", path, allowed, err)
		}
	}
	allowed, err := authorizer.Authorize(context.Background(), Subject{UserID: "u1"}, Request{Method: "GET", Path: "/api/admin/v1/dictionaries-archive"})
	if !errors.Is(err, ErrAccessDenied) || allowed {
		t.Fatalf("prefix policy escaped collection: allowed=%v err=%v", allowed, err)
	}
}

func TestAuthorizerKeepsMediaFilesCompatibilityBridgeScoped(t *testing.T) {
	policies := NewMemoryPolicyStore()
	if err := policies.AddPolicy(Policy{Subject: "u1", Method: "GET", Path: "/api/admin/v1/files/*", Effect: EffectAllow}); err != nil {
		t.Fatal(err)
	}
	authorizer := NewAuthorizer(policies)
	for _, path := range []string{
		"/api/admin/v1/files",
		"/api/admin/v1/files/upload",
		"/api/admin/v1/media/library",
		"/api/admin/v1/media/resources/logo/open",
	} {
		allowed, err := authorizer.Authorize(context.Background(), Subject{UserID: "u1"}, Request{Method: "GET", Path: path})
		if err != nil || !allowed {
			t.Fatalf("media compatibility policy should allow %q: allowed=%v err=%v", path, allowed, err)
		}
	}
	for _, path := range []string{
		"/api/admin/v1/files-archive",
		"/api/admin/v1/metadata",
	} {
		allowed, err := authorizer.Authorize(context.Background(), Subject{UserID: "u1"}, Request{Method: "GET", Path: path})
		if !errors.Is(err, ErrAccessDenied) || allowed {
			t.Fatalf("compatibility bridge escaped catalog roots for %q: allowed=%v err=%v", path, allowed, err)
		}
	}
}

func TestDataScopeResolverMatchesRoleAndCopiesIDs(t *testing.T) {
	p := NewMemoryPolicyStore()
	p.Add(DataScope{RoleID: "role-org", Resource: "orders", Scope: ScopeOrg, IDs: []string{"org-a"}})
	r := NewMemoryDataScopeResolver(p)
	got, err := r.Resolve(context.Background(), Subject{UserID: "u1", RoleIDs: []string{"role-org"}}, "orders")
	if err != nil || got.Scope != ScopeOrg || len(got.IDs) != 1 {
		t.Fatalf("role scope=%+v err=%v", got, err)
	}
	got.IDs[0] = "mutated"
	again, err := r.Resolve(context.Background(), Subject{UserID: "u1", RoleIDs: []string{"role-org"}}, "orders")
	if err != nil || again.IDs[0] != "org-a" {
		t.Fatalf("scope IDs leaked through result: %+v err=%v", again, err)
	}
}
