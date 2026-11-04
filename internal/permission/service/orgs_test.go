package service

import (
	"reflect"
	"testing"
)

// OrgsFromGroups derives the orgs a subject belongs to from the token's
// `groups` claim. A bug here would silently misattribute a subject to an org,
// so the parsing is pinned without needing a token.
//
// Two carrier forms are supported (ACCESS-CONTROL §11 / D1):
//   - legacy `/org:<slug>` (migrated away, kept for conformance)
//   - hierarchical `/{slug}` + `/{slug}/{group}` — under the new model the
//     top-level group IS the org, so every top-level group segment resolves to
//     an org slug.
func TestOrgsFromGroups(t *testing.T) {
	cases := []struct {
		name   string
		groups []string
		want   []string
	}{
		{"empty", nil, nil},
		{"legacy org prefix", []string{"/org:acme", "/org:globex"}, []string{"acme", "globex"}},
		{"new model: top-level groups are orgs", []string{"/sdp-admin", "/org:acme", "/Engineering"}, []string{"sdp-admin", "acme", "Engineering"}},
		{"new model: subgroup resolves to parent org", []string{"/acme/team-a", "/globex/team-b"}, []string{"acme", "globex"}},
		{"dedupes", []string{"/org:acme", "/org:acme"}, []string{"acme"}},
		{"skips bare prefix", []string{"/org:"}, nil},
		{"skips root path", []string{"/"}, nil},
		{"top-level arbitrary group is an org", []string{"/organization:acme"}, []string{"organization:acme"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := OrgsFromGroups(tc.groups)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("OrgsFromGroups(%v) = %v, want %v", tc.groups, got, tc.want)
			}
		})
	}
}

// The reserved prefix is the org carrier and must never be an RBAC subject.
// This guards the D1 hard convention (§2.3): if it were dropped, an org group
// could be bound as a role subject and swapping the carrier would need a data
// migration.
func TestOrgGroupPrefixIsReserved(t *testing.T) {
	if OrgGroupPrefix != "/org:" {
		t.Fatalf("OrgGroupPrefix = %q, want \"/org:\"", OrgGroupPrefix)
	}
}
