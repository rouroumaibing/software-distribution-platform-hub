package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/service"
)

// PlatformRoleBindingHandler exposes §7.2 platform-level grants (C-10):
// which subjects (users / Keycloak groups) hold which platform role.
//
// There is no Update — changing the granted role is a delete-then-create, so
// a (org, subject, role) triple has exactly one row at a time, mirroring
// BindingHandler's component-level behaviour.
type PlatformRoleBindingHandler struct {
	svc *service.PlatformBindingService
}

func NewPlatformRoleBindingHandler(svc *service.PlatformBindingService) *PlatformRoleBindingHandler {
	return &PlatformRoleBindingHandler{svc: svc}
}

func (h *PlatformRoleBindingHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/platform-role-bindings", h.List)
	rg.POST("/platform-role-bindings", h.Create)
	rg.DELETE("/platform-role-bindings/:id", h.Delete)
}

// List supports ?orgId=, ?subjectType= and ?subjectId=. Expired grants are
// returned on purpose: an admin has to be able to see what lapsed.
func (h *PlatformRoleBindingHandler) List(c *gin.Context) {
	var orgID *uuid.UUID
	if raw := c.Query("orgId"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			common.Fail(c, http.StatusBadRequest, err)
			return
		}
		orgID = &parsed
	}
	items, err := h.svc.List(orgID, c.Query("subjectType"), c.Query("subjectId"))
	if err != nil {
		failPermission(c, err)
		return
	}
	common.OK(c, items)
}

func (h *PlatformRoleBindingHandler) Create(c *gin.Context) {
	var in models.PlatformRoleBinding
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.Create(&in); err != nil {
		failPermission(c, err)
		return
	}
	common.Created(c, in)
}

func (h *PlatformRoleBindingHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.Delete(id); err != nil {
		failPermission(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
