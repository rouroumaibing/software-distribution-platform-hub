package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/middleware"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/repository"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/service"
)

// UserInfoHandler serves GET /api/userinfo — the console's only auth-related
// call (ACCOUNT-PERMISSION-MODEL §8 #3). The frontend uses it purely to
// hide/grey out buttons and menus; it is NOT a security boundary. The response
// is the subject's identity (subject/username), the orgs it belongs to under
// the `/org:<slug>` carrier, and the platform roles it currently holds.
type UserInfoHandler struct {
	platformBindRepo *repository.PlatformRoleBindingRepository
	platformRoleRepo *repository.PlatformRoleRepository
}

func NewUserInfoHandler(
	bindRepo *repository.PlatformRoleBindingRepository,
	roleRepo *repository.PlatformRoleRepository,
) *UserInfoHandler {
	return &UserInfoHandler{platformBindRepo: bindRepo, platformRoleRepo: roleRepo}
}

func (h *UserInfoHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/userinfo", h.Get)
}

type userInfoResponse struct {
	Subject  string   `json:"subject"`
	Username string   `json:"username,omitempty"`
	Groups   []string `json:"groups"`
	Orgs     []string `json:"orgs"`
	Roles    []string `json:"roles"`
}

func (h *UserInfoHandler) Get(c *gin.Context) {
	subject, ok := middleware.CurrentSubject(c)
	if !ok {
		common.Fail(c, http.StatusUnauthorized, common.ErrUnauthorized)
		return
	}
	groups := middleware.CurrentGroups(c)

	// Active, global platform bindings (orgID nil) for this subject/groups.
	bindings, err := h.platformBindRepo.ListMatching(nil, subject, groups)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	var roles []string
	seen := map[string]struct{}{}
	for _, b := range bindings {
		pr, perr := h.platformRoleRepo.GetByID(b.PlatformRoleID)
		if perr != nil {
			continue
		}
		if _, dup := seen[pr.Name]; dup {
			continue
		}
		seen[pr.Name] = struct{}{}
		roles = append(roles, pr.Name)
	}

	common.OK(c, userInfoResponse{
		Subject:  subject,
		Username: middleware.CurrentPreferredUsername(c),
		Groups:   groups,
		Orgs:     service.OrgsFromGroups(groups),
		Roles:    roles,
	})
}
