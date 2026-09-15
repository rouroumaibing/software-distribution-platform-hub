package middleware

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/service"
)

var (
	errNoUser    = errors.New("no authenticated user on context")
	errForbidden = errors.New("insufficient permission")
)

// RequirePermission must run after Authenticator + UserContext. It checks
// whether the current user's ComponentRoleBinding on :componentIdParam
// grants the given permission (one of permission.Permission* constants),
// and aborts with 403 if not.
//
// This is deliberately separate from Authenticator — authentication
// (who is this) and authorization (what can they do on this specific
// component) are different concerns, and every module's handler.go
// decides its own required permission per route rather than one global
// policy.
func RequirePermission(bindingSvc *service.BindingService, componentIDParam, permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := CurrentUserID(c)
		if !ok {
			common.Fail(c, http.StatusUnauthorized, errNoUser)
			c.Abort()
			return
		}

		componentID, err := uuid.Parse(c.Param(componentIDParam))
		if err != nil {
			common.Fail(c, http.StatusBadRequest, err)
			c.Abort()
			return
		}

		allowed, err := bindingSvc.HasPermission(componentID, userID, CurrentGroups(c), permission)
		if err != nil {
			common.Fail(c, http.StatusForbidden, errForbidden)
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
