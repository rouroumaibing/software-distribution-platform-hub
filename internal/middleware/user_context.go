package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/service"
)

// contextKeyUserID is where UserContext stashes the resolved local user's
// ID; handlers read it via CurrentUserID(c).
const contextKeyUserID = "auth.userID"

// UserContext must run after Authenticator.Middleware(). It resolves the
// verified Keycloak identity to a local users row, auto-provisioning one
// on first login (see UserService.GetOrProvisionByKeycloakID) so nobody
// needs a manual "create my account" step after signing in via SSO.
func UserContext(userSvc *service.UserService) gin.HandlerFunc {
	return func(c *gin.Context) {
	raw, exists := c.Get(contextKeyClaims)
	if !exists {
		// Authenticator didn't run — dev mode (auth disabled) or a public
		// route. Provision a fixed dev user so downstream handlers (and
		// RequirePermission, when auth is on but claims are missing) have a
		// resolved identity to work with.
		user, err := userSvc.GetOrProvisionByKeycloakID("dev", "dev@local", "Dev User")
		if err != nil {
			common.Fail(c, http.StatusInternalServerError, err)
			c.Abort()
			return
		}
		c.Set(contextKeyUserID, user.ID)
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
