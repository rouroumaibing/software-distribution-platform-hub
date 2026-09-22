package service

import "strings"

// OrgsFromGroups extracts the org aliases the subject belongs to from the
// token's `groups` claim, under the `/org:<slug>` convention (D1 default
// carrier, ACCOUNT-PERMISSION-MODEL §2.3). Non-org groups are ignored. Pure
// logic so the carrier parsing is unit-testable without a token. Duplicates
// are collapsed, order preserved.
//
// The reserved prefix itself is OrgGroupPrefix (declared in platform_binding.go);
// groups under it identify the org dimension and are never an RBAC subject.
func OrgsFromGroups(groups []string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, g := range groups {
		if !strings.HasPrefix(g, OrgGroupPrefix) {
			continue
		}
		slug := strings.TrimPrefix(g, OrgGroupPrefix)
		if slug == "" {
			continue
		}
		if _, ok := seen[slug]; ok {
			continue
		}
		seen[slug] = struct{}{}
		out = append(out, slug)
	}
	return out
}
