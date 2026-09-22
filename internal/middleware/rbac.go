package middleware

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

var (
	errNoUser    = errors.New("no authenticated user on context")
	errNoSubject = errors.New("no RBAC subject on context")
	errForbidden = errors.New("insufficient permission")
	errNoParam   = errors.New("route has no resource id path parameter")
	errNoLocator = errors.New("route carries a non-component id but no Locator is wired")
)

// Resource names what kind of id a guarded route carries in its path. Most
// routes are addressed by a component id, but the run / pipeline routes are
// addressed by their *own* id — those must be resolved to the owning
// component *before* the binding lookup, otherwise the lookup receives an id
// that matches no component binding and the route denies every request as
// soon as auth is switched on (ACCOUNT-PERMISSION-MODEL §10 #14).
type Resource string

const (
	// ResourceComponent — the path parameter is already the component id.
	ResourceComponent Resource = "component"
	// ResourcePipeline — the path parameter is a pipeline id; the check runs
	// against the pipeline's owning component.
	ResourcePipeline Resource = "pipeline"
	// ResourceRun — the path parameter is a run id; the check runs against
	// the owning component of the pipeline the run belongs to.
	ResourceRun Resource = "run"
)

// Locator resolves the ids of non-component resources to the component that
// owns them. RequirePermission only needs it for ResourcePipeline /
// ResourceRun; ResourceComponent never calls it.
type Locator interface {
	ComponentOfPipeline(pipelineID uuid.UUID) (uuid.UUID, error)
	ComponentOfRun(runID uuid.UUID) (uuid.UUID, error)
}

// PermissionChecker is the slice of the permission service the middleware
// actually uses, narrowed to an interface so guarded routes can be exercised
// without a database (same motive as the service layer's narrow stores).
// *permissionsvc.BindingService satisfies it.
//
// Both the local user id and the RBAC subject are passed: bindings key on the
// stable Keycloak `sub` (§5.3), while the component owner-override still keys
// on the local user UUID (components.owner_user). See ResolveComponentActions.
type PermissionChecker interface {
	HasPermission(componentID, userID uuid.UUID, subject string, groups []string, permission string) (bool, error)
}

// PlatformPermissionChecker is the platform-scoped sibling of
// PermissionChecker: it answers "does this subject hold a platform-level
// permission" (page/console visibility, org/user management). orgID == nil
// means "global bindings only". *permissionsvc.BindingService satisfies it.
type PlatformPermissionChecker interface {
	HasPlatformPermission(orgID *uuid.UUID, subject string, groups []string, permission string) (bool, error)
}

// Requirement declares one guarded route: which path parameter carries the
// resource id, what kind of resource that id identifies, and which permission
// the resource's owning component must grant.
type Requirement struct {
	Resource   Resource
	Param      string
	Permission string
}

// RequirePermission must run after Authenticator + UserContext. It resolves
// the route's resource id to its owning component and checks whether the
// current subject's ComponentRoleBinding on that component grants
// req.Permission (one of the permmodels.Action* constants).
//
// This is deliberately separate from Authenticator — authentication (who is
// this) and authorization (what can they do on this specific component) are
// different concerns, and every module's handler decides its own required
// permission per route rather than one global policy.
func RequirePermission(checker PermissionChecker, loc Locator, req Requirement) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := CurrentUserID(c)
		if !ok {
			common.Fail(c, http.StatusUnauthorized, errNoUser)
			c.Abort()
			return
		}
		subject, ok := CurrentSubject(c)
		if !ok {
			common.Fail(c, http.StatusUnauthorized, errNoSubject)
			c.Abort()
			return
		}

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

		allowed, err := checker.HasPermission(componentID, userID, subject, CurrentGroups(c), req.Permission)
		if err != nil {
			// A failing lookup is an infrastructure problem, not a denial:
			// answering 403 here would disguise an outage as "no permission"
			// and send people hunting for a binding that was never the issue.
			common.Fail(c, http.StatusInternalServerError, err)
			c.Abort()
			return
		}
		if !allowed {
			common.Fail(c, http.StatusForbidden, errForbidden)
			c.Abort()
			return
		}
		c.Next()
	}
}

// RequirePlatformPermission gates the platform-level management routes
// (roles, bindings, ownership, permission requests). It checks the subject's
// *global* platform bindings (orgID == nil), which is where the seeded
// `/sdp-admin` → sdp-admin grant lives; org-scoped platform grants are a
// follow-up once the org carrier is wired into the token.
func RequirePlatformPermission(checker PlatformPermissionChecker, permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		subject, ok := CurrentSubject(c)
		if !ok {
			common.Fail(c, http.StatusUnauthorized, errNoSubject)
			c.Abort()
			return
		}
		allowed, err := checker.HasPlatformPermission(nil, subject, CurrentGroups(c), permission)
		if err != nil {
			common.Fail(c, http.StatusInternalServerError, err)
			c.Abort()
			return
		}
		if !allowed {
			common.Fail(c, http.StatusForbidden, errForbidden)
			c.Abort()
			return
		}
		c.Next()
	}
}

// resourceID parses the guarded route's path parameter into a UUID.
func resourceID(c *gin.Context, param string) (uuid.UUID, error) {
	raw := c.Param(param)
	if raw == "" {
		return uuid.Nil, errNoParam
	}
	return uuid.Parse(raw)
}

// locateComponent maps a parsed resource id onto the component whose binding
// authorizes the request. ResourceComponent needs no lookup — the id already
// IS the component id.
func locateComponent(loc Locator, res Resource, id uuid.UUID) (uuid.UUID, error) {
	switch res {
	case ResourcePipeline:
		if loc == nil {
			return uuid.Nil, errNoLocator
		}
		return loc.ComponentOfPipeline(id)
	case ResourceRun:
		if loc == nil {
			return uuid.Nil, errNoLocator
		}
		return loc.ComponentOfRun(id)
	default:
		return id, nil
	}
}

// locateStatus picks the HTTP status for a failed locate: a missing resource
// is a bad path (404), a wiring gap or a store failure is ours (500).
func locateStatus(err error) int {
	if errors.Is(err, common.ErrResourceNotFound) {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}
