package authorization

import (
	"errors"
	"testing"
)

func TestPolicyMapsOnlyExactOwnerAndAuthorizesOwnerActions(t *testing.T) {
	t.Parallel()

	const ownerID = "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"
	policy, err := NewPolicy(ExternalIdentity{Issuer: "https://issuer.example/tenant", Subject: "coach-123"}, ownerID)
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	principal, err := policy.PrincipalFor(ExternalIdentity{Issuer: "https://issuer.example/tenant", Subject: "coach-123"})
	if err != nil {
		t.Fatalf("PrincipalFor(owner) error = %v", err)
	}
	if principal.OwnerID != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" {
		t.Fatalf("OwnerID = %q", principal.OwnerID)
	}
	for _, action := range []Action{Read, Mutate, Archive, Recovery} {
		if err := policy.Authorize(principal, action, principal.OwnerID); err != nil {
			t.Errorf("Authorize(%q) error = %v", action, err)
		}
	}
}

func TestPolicyDeniesUnknownIdentityTargetAndAction(t *testing.T) {
	t.Parallel()

	const ownerID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	policy, err := NewPolicy(ExternalIdentity{Issuer: "https://issuer.example/tenant", Subject: "coach-123"}, ownerID)
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	for _, identity := range []ExternalIdentity{
		{Issuer: "https://issuer.example/TENANT", Subject: "coach-123"},
		{Issuer: "https://issuer.example/tenant", Subject: "Coach-123"},
		{Issuer: "https://issuer.example/tenant", Subject: "admin-group"},
	} {
		if _, err := policy.PrincipalFor(identity); !errors.Is(err, ErrDenied) {
			t.Errorf("PrincipalFor(%+v) error = %v", identity, err)
		}
	}
	principal, _ := policy.PrincipalFor(ExternalIdentity{Issuer: "https://issuer.example/tenant", Subject: "coach-123"})
	if err := policy.Authorize(principal, Read, "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"); !errors.Is(err, ErrDenied) {
		t.Errorf("Authorize(other owner) error = %v", err)
	}
	if err := policy.Authorize(principal, Action("admin"), ownerID); !errors.Is(err, ErrDenied) {
		t.Errorf("Authorize(unknown action) error = %v", err)
	}
}

func TestNewPolicyRejectsIncompleteBinding(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		identity ExternalIdentity
		ownerID  string
	}{
		{identity: ExternalIdentity{Subject: "coach"}, ownerID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
		{identity: ExternalIdentity{Issuer: "https://issuer.example"}, ownerID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"},
		{identity: ExternalIdentity{Issuer: "https://issuer.example", Subject: "coach"}, ownerID: "not-a-uuid"},
	} {
		if _, err := NewPolicy(test.identity, test.ownerID); err == nil {
			t.Errorf("NewPolicy(%+v, %q) error = nil", test.identity, test.ownerID)
		}
	}
}
