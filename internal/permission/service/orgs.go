package service

import "strings"

// OrgsFromGroups extracts the org aliases the subject belongs to from the
// token's `groups` claim. It supports both the legacy `/org:<slug>` carrier
// (ACCOUNT-PERMISSION-MODEL §2.3) and the hierarchical `/{slug}` + `/{slug}/{group}`
// model (ACCESS-CONTROL §11 / D1 revision): the org is the first path segment
// of any group whose path is `/{org}` or `/{org}/{group}`. Non-org groups that
// happen to be top-level (e.g. `/team-x`) are treated as org "team-x" under the
// new model, where org groups ARE the top-level groups. Pure logic so the
// carrier parsing is unit-testable without a token. Duplicates are collapsed,
// order preserved.
func OrgsFromGroups(groups []string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, g := range groups {
		slug := orgSlugFromGroup(g)
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

// orgSlugFromGroup resolves the org slug from a single group path.
func orgSlugFromGroup(g string) string {
	if strings.HasPrefix(g, OrgGroupPrefix) {
		return strings.TrimPrefix(g, OrgGroupPrefix)
	}
	t := strings.TrimPrefix(g, "/")
	if t == "" {
		return ""
	}
	return strings.SplitN(t, "/", 2)[0]
}
