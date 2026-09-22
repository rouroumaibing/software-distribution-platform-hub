package middleware

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// errOutOfOrg is the coarse-half rejection: the resource is registered to
// organizations, and this request proves membership of none of them. It is a
// distinct error from errForbidden on purpose — "you are not in the owning
// org" and "your role cannot call this interface" have completely different
// fixes (an ownership row vs a role binding), and a single 403 message for
// both sends people hunting in the wrong table.
var errOutOfOrg = errors.New("resource is not owned by any of the subject's organizations")

// OwnershipChecker answers the coarse half of authorization for one component.
// *permissionsvc.ResourceOwnershipService satisfies it. Only a boolean is
// asked for: the middleware maps false onto 403, and *which* states count as
// allowed (no ownership row vs. intersecting org) is the service's decision —
// keeping that table in one place rather than split across HTTP handlers.
type OwnershipChecker interface {
	AllowsComponent(componentID uuid.UUID, orgIDs []uuid.UUID) (bool, error)
}

// OrgAliasResolver maps the org aliases carried by the token (`/org:<slug>`)
// onto local orgs.id values. *orgrepository.OrgRepository satisfies it. It is
// a separate dependency from the checker so the alias→id join stays testable
// on its own and the checker never learns the carrier's spelling.
type OrgAliasResolver interface {
	IDsBySlugs(slugs []string) (map[string]uuid.UUID, error)
}

// RequireResourceOwnership is the coarse half of authorization
// (ACCOUNT-PERMISSION-MODEL §4 step 3, 鉴权 a：资源归属). It answers "does
// this request belong to an organization that owns the resource", before
// RequirePermission answers "does this subject hold the role that may call
// this interface" (§4 step 4, 鉴权 b：RBAC 决策).
//
// The two are separate middlewares rather than one check for two reasons:
//   - §4 fixes the order (a then b) and the short-circuit semantics: a failed
//     stage aborts, it never degrades into "log and continue".
//   - collapsing them would erase the distinction above from logs and metrics.
//
// Like RequirePermission it runs against the *component* that owns the route's
// resource, so pipelines and runs inherit their component's ownership instead
// of needing their own rows. In dev mode (auth disabled) main.go does not
// install it at all — the same escape hatch every other authz check uses.
func RequireResourceOwnership(checker OwnershipChecker, orgs OrgAliasResolver, loc Locator, req Requirement) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := resourceID(c, req.Param)
		if err != nil {
			common.Fail(c, http.StatusBadRequest, err)
			c.Abort()
			return
		}

		componentID, err := locateComponent(loc, req.Resource, id)
		if err != nil {
			common.Fail(c, locateStatus(err), err)
			c.Abort()
			return
		}

		orgIDs, err := resolveOrgIDs(orgs, CurrentOrgs(c))
		if err != nil {
			common.Fail(c, http.StatusInternalServerError, err)
			c.Abort()
			return
		}

		allowed, err := checker.AllowsComponent(componentID, orgIDs)
		if err != nil {
			common.Fail(c, http.StatusInternalServerError, err)
			c.Abort()
			return
		}
		if !allowed {
			common.Fail(c, http.StatusForbidden, errOutOfOrg)
			c.Abort()
			return
		}
		c.Next()
	}
}

// resolveOrgIDs turns the subject's org aliases into local org ids, dropping
// aliases this hub does not know. Unknown aliases must not widen access, and
// they cannot narrow it either: with a registered owner an empty result is a
// denial anyway (see ResourceOwnershipService.AllowsComponent).
//
// A nil resolver means the org dimension was never wired; that is reported as
// "no orgs" rather than as an error, so a partial wiring cannot turn every
// ownership-registered request into a 500.
func resolveOrgIDs(orgs OrgAliasResolver, aliases []string) ([]uuid.UUID, error) {
	if orgs == nil || len(aliases) == 0 {
		return nil, nil
	}
	bySlug, err := orgs.IDsBySlugs(aliases)
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(aliases))
	for _, alias := range aliases {
		if id, ok := bySlug[alias]; ok {
			out = append(out, id)
		}
	}
	return out, nil
}
