package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/service"
)

// RoleHandler only exposes List — roles are seeded (Viewer/Editor/Admin),
// no create/update/delete API for now.
type RoleHandler struct{ svc *service.RoleService }

func NewRoleHandler(svc *service.RoleService) *RoleHandler { return &RoleHandler{svc: svc} }

func (h *RoleHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/roles", h.List)
}

func (h *RoleHandler) List(c *gin.Context) {
	items, err := h.svc.List()
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, items)
}
