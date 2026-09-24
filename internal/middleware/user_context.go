package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/service"
)

// contextKeySubject is the RBAC subject: the Keycloak `sub` claim, or the dev
// pseudo-subject when auth is disabled. Per ACCOUNT-PERMISSION-MODEL §5.3
// bindings key on `sub` (stable, immutable), never on a local row id — and
// since D3 hub keeps no user table at all (§2.2), it is the *only* identity
// key the system has.
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

// DevSubject is the pseudo-subject used when auth is disabled (dev mode).
const DevSubject = "dev"

// UserContext must run after Authenticator.Middleware(). It turns the verified
// Keycloak claims into the three things the §4 authorization chain needs: the
// RBAC subject (`sub`, §5.3), the display name (audit only) and the group /
// org dimensions (§7 / §2.3).
//
// It deliberately touches no database. Before D3 this middleware ran a
// get-or-provision write against a local `users` table on **every** request;
// hub is now stateless with respect to identity (§2.2) and reads the subject
// straight out of the token, so an authenticated request costs zero identity
// writes and dev mode is a pure constant — not a row that has to exist first.
func UserContext() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, exists := c.Get(contextKeyClaims)
		if !exists {
			// Authenticator didn't run — dev mode (auth disabled) or a public
			// route. Stamp the fixed dev subject so downstream handlers (and
			// RequirePermission, when auth is on but claims are missing) have a
			// stable identity to work with.
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

		c.Set(contextKeySubject, claims.Subject)
		c.Set(contextKeyPreferredUsername, claims.PreferredUsername)
		c.Set(contextKeyGroups, claims.Groups)
		c.Set(contextKeyOrgs, service.OrgsFromGroups(claims.Groups))
		c.Next()
	}
}

// CurrentSubject reads the RBAC subject (the Keycloak `sub`, or the dev
// pseudo-subject). Returns "", false if UserContext hasn't run or the token
// carried an empty `sub`. This is the single identity key in the system:
// bindings, ownership rows and every audit stamp resolve from it (§5.3).
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
// `/org:<slug>` groups exist yet. Note this is NOT the same as an error: the
// ownership decision treats "no orgs" as a normal state (§3).
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
