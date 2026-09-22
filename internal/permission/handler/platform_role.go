package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/service"
)

// failPermission writes err using the status its *APIError carries, so a
// validation 400 / conflict 409 / not-found 404 produced by the service layer
// reaches the client unchanged instead of collapsing into a blanket 500.
func failPermission(c *gin.Context, err error) {
	if ae, ok := err.(*common.APIError); ok {
		common.Fail(c, ae.Code, ae)
		return
	}
	common.Fail(c, http.StatusInternalServerError, err)
}

// PlatformRoleHandler exposes §7.2 platform-level roles (C-10). Until this
// existed the tables were reachable only through seed SQL, so no subject could
// hold a platform permission once enforcement is switched on.
type PlatformRoleHandler struct{ svc *service.PlatformRoleService }

func NewPlatformRoleHandler(svc *service.PlatformRoleService) *PlatformRoleHandler {
	return &PlatformRoleHandler{svc: svc}
}

func (h *PlatformRoleHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/platform-roles", h.List)
	rg.POST("/platform-roles", h.Create)
	rg.GET("/platform-roles/:id", h.Get)
	rg.PUT("/platform-roles/:id", h.Update)
	rg.DELETE("/platform-roles/:id", h.Delete)
}

func (h *PlatformRoleHandler) List(c *gin.Context) {
	items, err := h.svc.List()
	if err != nil {
		failPermission(c, err)
		return
	}
	common.OK(c, items)
}

func (h *PlatformRoleHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	item, serr := h.svc.Get(id)
	if serr != nil {
		failPermission(c, serr)
		return
	}
	common.OK(c, item)
}

func (h *PlatformRoleHandler) Create(c *gin.Context) {
	var in models.PlatformRole
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

func (h *PlatformRoleHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	var in models.PlatformRole
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.Update(id, &in); err != nil {
		failPermission(c, err)
		return
	}
	updated, gerr := h.svc.Get(id)
	if gerr != nil {
		failPermission(c, gerr)
		return
	}
	common.OK(c, updated)
}

func (h *PlatformRoleHandler) Delete(c *gin.Context) {
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
