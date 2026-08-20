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
