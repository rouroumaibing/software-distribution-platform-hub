package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/service"
)

// BindingHandler has no Update — re-granting a different role is a
// delete-then-create, so the resource's identity (component+user) always
// maps to exactly one role at a time.
type BindingHandler struct{ svc *service.BindingService }

func NewBindingHandler(svc *service.BindingService) *BindingHandler { return &BindingHandler{svc: svc} }

func (h *BindingHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/components/:id/role-bindings", h.Create)
	rg.GET("/components/:id/role-bindings", h.List)
	rg.DELETE("/role-bindings/:id", h.Delete)
}

func (h *BindingHandler) Create(c *gin.Context) {
	componentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	var in models.ComponentRoleBinding
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	in.ComponentID = componentID
	if err := h.svc.Create(&in); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.Created(c, in)
}

func (h *BindingHandler) List(c *gin.Context) {
	componentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	items, err := h.svc.ListByComponent(componentID)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, items)
}

func (h *BindingHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.Delete(id); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	c.Status(http.StatusNoContent)
}
