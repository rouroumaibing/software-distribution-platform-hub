package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/service"
)

// contextKeyUserID is where UserContext stashes the resolved local user's
// ID; handlers read it via CurrentUserID(c). It is used for the component
// owner-override (components.owner_user is a uuid) and for the legacy V1
// binding column; it is NOT the RBAC subject any more (see contextKeySubject).
const contextKeyUserID = "auth.userID"

// contextKeySubject is the RBAC subject: the Keycloak `sub` claim, or the
// dev pseudo-subject when auth is disabled. Per ACCOUNT-PERMISSION-MODEL §5.3
// bindings key on `sub` (stable, immutable), never on the local users.id.
const contextKeySubject = "auth.subject"

// contextKeyPreferredUsername is the display name, for audit trails.
const contextKeyPreferredUsername = "auth.preferredUsername"

// contextKeyGroups is where UserContext stashes the resolved Keycloak group
// memberships (empty in dev mode / when the token carries no groups); the RBAC
// middleware reads them via CurrentGroups(c) to apply §7 group inheritance.
const contextKeyGroups = "auth.groups"

// contextKeyOrgs is where UserContext stashes the org aliases parsed out of
// the `groups` claim via the `/org:<slug>` convention (D1 default carrier,
// §2.3). The ownership middleware reads them via CurrentOrgs(c) as the input
// to the §3 decision formula — this is the *only* consumer of the org
// dimension, which is why orgs are kept out of subject_id (§5.3).
const contextKeyOrgs = "auth.orgs"

// DevSubject is the pseudo-subject used when auth is disabled (dev mode). It
// matches the KeycloakID the dev user is provisioned under, so dev bindings
// created against it resolve.
const DevSubject = "dev"

// UserContext must run after Authenticator.Middleware(). It resolves the
// verified Keycloak identity to a local users row, auto-provisioning one
// on first login (see UserService.GetOrProvisionByKeycloakID) so nobody
// needs a manual "create my account" step after signing in via SSO. It also
// records the RBAC subject (`sub`) separately from the local user id so the
// authorization path keys on the stable Keycloak identity (§5.3 / §12 D3),
// and derives the org dimension from the same `groups` claim (§2.3).
func UserContext(userSvc *service.UserService) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, exists := c.Get(contextKeyClaims)
		if !exists {
			// Authenticator didn't run — dev mode (auth disabled) or a public
			// route. Provision a fixed dev user so downstream handlers (and
			// RequirePermission, when auth is on but claims are missing) have a
			// resolved identity to work with.
			user, err := userSvc.GetOrProvisionByKeycloakID(DevSubject, "dev@local", "Dev User")
			if err != nil {
				common.Fail(c, http.StatusInternalServerError, err)
				c.Abort()
				return
			}
			c.Set(contextKeyUserID, user.ID)
			c.Set(contextKeySubject, DevSubject)
			c.Set(contextKeyPreferredUsername, "Dev User")
			// No claims ⇒ no org dimension. Setting it explicitly keeps
			// CurrentOrgs honest about "looked, found nothing" rather than
			// "middleware never ran".
			c.Set(contextKeyOrgs, []string(nil))
			c.Next()
			return
		}
		claims := raw.(keycloakClaims)

		user, err := userSvc.GetOrProvisionByKeycloakID(claims.Subject, claims.Email, claims.PreferredUsername)
		if err != nil {
			common.Fail(c, http.StatusInternalServerError, err)
			c.Abort()
			return
		}

		c.Set(contextKeyUserID, user.ID)
		c.Set(contextKeySubject, claims.Subject)
		c.Set(contextKeyPreferredUsername, claims.PreferredUsername)
		c.Set(contextKeyGroups, claims.Groups)
		c.Set(contextKeyOrgs, service.OrgsFromGroups(claims.Groups))
		c.Next()
	}
}

// CurrentUserID reads the resolved local user ID set by UserContext.
// Returns uuid.Nil, false if UserContext hasn't run on this route.
func CurrentUserID(c *gin.Context) (uuid.UUID, bool) {
	raw, exists := c.Get(contextKeyUserID)
	if !exists {
		return uuid.Nil, false
	}
	id, ok := raw.(uuid.UUID)
	return id, ok
}

// CurrentSubject reads the RBAC subject (the Keycloak `sub`, or the dev
// pseudo-subject). Returns "", false if UserContext hasn't run. This — not
// CurrentUserID — is what role bindings key on (§5.3).
func CurrentSubject(c *gin.Context) (string, bool) {
	raw, exists := c.Get(contextKeySubject)
	if !exists {
		return "", false
	}
	s, ok := raw.(string)
	return s, ok && s != ""
}

// CurrentPreferredUsername reads the display name set by UserContext ("" when
// absent). Used for audit entries only — never as a key.
func CurrentPreferredUsername(c *gin.Context) string {
	raw, exists := c.Get(contextKeyPreferredUsername)
	if !exists {
		return ""
	}
	s, _ := raw.(string)
	return s
}

// CurrentGroups reads the Keycloak group memberships set by UserContext.
// Returns nil in dev mode or when the token carries no groups.
func CurrentGroups(c *gin.Context) []string {
	raw, exists := c.Get(contextKeyGroups)
	if !exists {
		return nil
	}
	groups, ok := raw.([]string)
	if !ok {
		return nil
	}
	return groups
}

// CurrentOrgs reads the org aliases (no `/org:` prefix) derived from the
// token's groups claim. Returns nil when the subject belongs to no
// organization — which includes dev mode and the current realm, where no
// groups exist yet. Note this is NOT the same as an error: the ownership
// decision treats "no orgs" as a normal state (§3).
func CurrentOrgs(c *gin.Context) []string {
	raw, exists := c.Get(contextKeyOrgs)
	if !exists {
		return nil
	}
	orgs, ok := raw.([]string)
	if !ok {
		return nil
	}
	return orgs
}
