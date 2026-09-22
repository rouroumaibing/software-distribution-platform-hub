package service

import (
	"reflect"
	"testing"
)

// OrgsFromGroups parses the `/org:<slug>` carrier (D1 default, §2.3). A bug
// here would silently misattribute a subject to an org, so the parsing is
// pinned without needing a token.
func TestOrgsFromGroups(t *testing.T) {
	cases := []struct {
		name   string
		groups []string
		want   []string
	}{
		{"empty", nil, nil},
		{"only org groups", []string{"/org:acme", "/org:globex"}, []string{"acme", "globex"}},
		{"ignores non-org groups", []string{"/sdp-admin", "/org:acme", "/Engineering"}, []string{"acme"}},
		{"dedupes", []string{"/org:acme", "/org:acme"}, []string{"acme"}},
		{"skips bare prefix", []string{"/org:"}, nil},
		{"prefix must be exact", []string{"/organization:acme"}, nil},
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
