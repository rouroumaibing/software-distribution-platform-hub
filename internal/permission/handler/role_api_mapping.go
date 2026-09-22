package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/repository"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/service"
)

// RoleAPIMappingHandler exposes the reviewable role→action registry
// (ACCOUNT-PERMISSION-MODEL §5.1③). Reads are by role; writes replace a
// role's whole action set (the reconcile in SetActions), so an admin edits
// the mapping as a unit rather than row-by-row.
type RoleAPIMappingHandler struct {
	svc  *service.RoleAPIMappingService
	repo *repository.RoleAPIMappingRepository
}

func NewRoleAPIMappingHandler(svc *service.RoleAPIMappingService, repo *repository.RoleAPIMappingRepository) *RoleAPIMappingHandler {
	return &RoleAPIMappingHandler{svc: svc, repo: repo}
}

func (h *RoleAPIMappingHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/role-api-mappings", h.List)
	rg.PUT("/role-api-mappings", h.Set)
	rg.DELETE("/role-api-mappings/:id", h.Delete)
}

func (h *RoleAPIMappingHandler) List(c *gin.Context) {
	kind := c.Query("roleKind")
	roleID, err := uuid.Parse(c.Query("roleId"))
	if err != nil || kind == "" {
		common.Fail(c, http.StatusBadRequest, common.ErrBadRequest.WithMessage("roleKind and roleId are required"))
		return
	}
	rows, lerr := h.repo.ListByRole(kind, roleID)
	if lerr != nil {
		failPermission(c, lerr)
		return
	}
	common.OK(c, rows)
}

type roleActionsInput struct {
	RoleKind string    `json:"roleKind"`
	RoleID   uuid.UUID `json:"roleId"`
	Actions  []string  `json:"actions"`
}

func (h *RoleAPIMappingHandler) Set(c *gin.Context) {
	var in roleActionsInput
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if (in.RoleKind != "platform" && in.RoleKind != "component") || in.RoleID == uuid.Nil {
		common.Fail(c, http.StatusBadRequest, common.ErrBadRequest.WithMessage("roleKind must be platform|component and roleId is required"))
		return
	}
	if err := h.svc.SetActions(in.RoleKind, in.RoleID, in.Actions); err != nil {
		failPermission(c, err)
		return
	}
	rows, lerr := h.repo.ListByRole(in.RoleKind, in.RoleID)
	if lerr != nil {
		failPermission(c, lerr)
		return
	}
	common.OK(c, rows)
}

func (h *RoleAPIMappingHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if derr := h.repo.Delete(id); derr != nil {
		failPermission(c, derr)
		return
	}
	c.Status(http.StatusNoContent)
}
