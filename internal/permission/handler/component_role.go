package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/repository"
)

// ComponentRoleHandler exposes the §7 component-scoped roles
// (component_roles) so the console can populate the role picker when
// granting a ComponentRoleBinding. Built-in roles are seeded
// (component-viewer / component-editor / component-approver / component-admin);
// org-defined custom roles may also be present. Read-only — roles are not
// managed through the hub API (they're seeded / org-admin-created elsewhere).
type ComponentRoleHandler struct {
	repo *repository.ComponentRoleRepository
}

func NewComponentRoleHandler(repo *repository.ComponentRoleRepository) *ComponentRoleHandler {
	return &ComponentRoleHandler{repo: repo}
}

func (h *ComponentRoleHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/component-roles", h.List)
}

func (h *ComponentRoleHandler) List(c *gin.Context) {
	items, err := h.repo.List()
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, items)
}
