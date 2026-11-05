// Package realmsettings exposes the realm-level configuration the platform
// administers through Keycloak (AUTH-ARCHITECTURE §5): the protocol-layer
// login mode and the identity-provider (SSO) catalog. All endpoints are mounted
// behind the platform-level user:manage guard (main.go), so this handler does
// not re-check authorization.
package realmsettings

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/auth"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/keycloak"
)

// Handler wires Keycloak (identity store) + the login-mode service.
type Handler struct {
	kc        *keycloak.Client
	loginMode auth.LoginModeService
}

func NewHandler(kc *keycloak.Client, loginMode auth.LoginModeService) *Handler {
	return &Handler{kc: kc, loginMode: loginMode}
}

// RegisterRoutes mounts the realm-settings endpoints on a router group that
// already carries the platform user:manage guard.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/identityprovider", h.ListIDP)
	rg.POST("/identityprovider", h.CreateIDP)
	rg.GET("/identityprovider/:alias", h.GetIDP)
	rg.PUT("/identityprovider/:alias", h.UpdateIDP)
	rg.DELETE("/identityprovider/:alias", h.DeleteIDP)
	rg.GET("/identityprovider/types", h.ListIDPTypes)
	rg.PUT("/login", h.SetLoginMode)
}

// ListIDP returns every configured identity provider instance.
func (h *Handler) ListIDP(c *gin.Context) {
	idps, err := h.kc.ListIdentityProviders(c.Request.Context())
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, gin.H{"identityProviders": idps})
}

// ListIDPTypes returns the Keycloak-supported provider factory ids (oidc / saml
// / cas / ...) — the console's checkProviderID whitelist (AUTH-ARCHITECTURE §5).
func (h *Handler) ListIDPTypes(c *gin.Context) {
	types, err := h.kc.ListIdentityProviderTypes(c.Request.Context())
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, gin.H{"types": types})
}

// GetIDP returns one identity provider by alias.
func (h *Handler) GetIDP(c *gin.Context) {
	alias := c.Param("alias")
	idp, err := h.kc.GetIdentityProvider(c.Request.Context(), alias)
	if err != nil {
		if errors.Is(err, keycloak.ErrNotFound) {
			common.Fail(c, http.StatusNotFound, err)
			return
		}
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, idp)
}

// CreateIDP adds a new identity provider.
func (h *Handler) CreateIDP(c *gin.Context) {
	var in keycloak.IdentityProvider
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	id, err := h.kc.CreateIdentityProvider(c.Request.Context(), in)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, gin.H{"alias": id})
}

// UpdateIDP replaces an existing identity provider.
func (h *Handler) UpdateIDP(c *gin.Context) {
	alias := c.Param("alias")
	var in keycloak.IdentityProvider
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if in.Alias == "" {
		in.Alias = alias
	}
	if err := h.kc.UpdateIdentityProvider(c.Request.Context(), alias, in); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, gin.H{"alias": alias})
}

// DeleteIDP removes an identity provider.
func (h *Handler) DeleteIDP(c *gin.Context) {
	alias := c.Param("alias")
	if err := h.kc.DeleteIdentityProvider(c.Request.Context(), alias); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// SetLoginMode switches the protocol-layer login mode. Switching back to
// default is forbidden while any IdP is enabled — SSO users would be stranded
// (AUTH-ARCHITECTURE §2 / D9).
func (h *Handler) SetLoginMode(c *gin.Context) {
	var req struct {
		Login string `json:"login"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if req.Login == "default" {
		idps, err := h.kc.ListIdentityProviders(c.Request.Context())
		if err == nil {
			for _, idp := range idps {
				if idp.Enabled {
					common.Fail(c, http.StatusConflict,
						common.DomainError(common.KindAuth, http.StatusConflict, 45,
							"cannot switch to default login while an identity provider is enabled"))
					return
				}
			}
		}
	}
	if err := h.loginMode.Set(c.Request.Context(), req.Login); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	common.OK(c, gin.H{"login": req.Login})
}
