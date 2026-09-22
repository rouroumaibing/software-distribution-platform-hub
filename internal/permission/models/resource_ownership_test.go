package models

import (
	"testing"

	"github.com/google/uuid"
)

// These pin the ownership decision (ACCOUNT-PERMISSION-MODEL §3): a token's
// org may access a resource iff it is the owner OR appears in the share-with
// set. Getting this wrong silently widens or narrows tenant isolation, so it is
// kept as pure logic and tested without a DB.

func TestResourceOwnership_IsAllowed(t *testing.T) {
	owner := uuid.New()
	sibling := uuid.New()
	other := uuid.New()

	o := &ResourceOwnership{OwnerOrg: owner}
	if err := o.SetAllowedOrgs([]uuid.UUID{sibling}); err != nil {
		t.Fatalf("SetAllowedOrgs: %v", err)
	}

	cases := []struct {
		name string
		org  uuid.UUID
		want bool
	}{
		{"owner is allowed", owner, true},
		{"shared sibling is allowed", sibling, true},
		{"unrelated org is denied", other, false},
		{"nil org is denied (never a wildcard)", uuid.Nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := o.IsAllowed(tc.org); got != tc.want {
				t.Errorf("IsAllowed(%s) = %v, want %v", tc.org, got, tc.want)
			}
		})
	}
}

func TestResourceOwnership_EmptyShareSetIsOwnerOnly(t *testing.T) {
	owner := uuid.New()
	o := &ResourceOwnership{OwnerOrg: owner}
	if err := o.SetAllowedOrgs(nil); err != nil {
		t.Fatalf("SetAllowedOrgs(nil): %v", err)
	}
	if len(o.AllowedOrgs) == 0 || string(o.AllowedOrgs) != "[]" {
		t.Errorf("empty share set should serialize to \"[]\", got %q", string(o.AllowedOrgs))
	}
	if got := o.AllowedOrgsSlice(); len(got) != 0 {
		t.Errorf("AllowedOrgsSlice of \"[]\" should be empty, got %v", got)
	}
	if !o.IsAllowed(owner) {
		t.Error("owner must still be allowed with an empty share set")
	}
	if o.IsAllowed(uuid.New()) {
		t.Error("no org other than the owner may be allowed with an empty share set")
	}
}

func TestResourceOwnership_RoundTripsAllowedOrgs(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	o := &ResourceOwnership{OwnerOrg: uuid.New()}
	if err := o.SetAllowedOrgs([]uuid.UUID{a, b}); err != nil {
		t.Fatalf("SetAllowedOrgs: %v", err)
	}
	got := o.AllowedOrgsSlice()
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Errorf("round-trip mismatch: got %v, want [%s %s]", got, a, b)
	}
}
